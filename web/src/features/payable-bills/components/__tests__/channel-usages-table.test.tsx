/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  defaultStringifySearch,
  RouterProvider,
} from '@tanstack/react-router'
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { payableBillsSearchSchema } from '@/routes/_authenticated/payable-bills/index'

import * as billApi from '../../api'
import type { PayableUsage } from '../../types'
import { ChannelUsagesTable } from '../channel-usages-table'

vi.mock('../../api')

const baseUsage: PayableUsage = {
  id: 101,
  task_id: 12,
  pull_site_id: 31,
  bill_date: 20261123,
  site_id: 3,
  site_name: 'Site A',
  site_base_url: 'https://a.example.com',
  remote_channel_id: 17,
  remote_channel_name: 'gpt-dcmark-3-17',
  remote_channel_status: 1,
  remote_channel_group: 'default',
  local_channel_id: 17,
  local_channel_name: 'gpt-platform',
  used_quota: 228394500,
  delta_quota: 6172839,
  delta_flag: 'normal',
  quota_per_unit: 500000,
  provider_id: 5,
  provider_name: 'alice',
  provider_source: 'parsed',
  payable_discount: 0.8,
  synced_at: 1763914205,
  created_time: 1763914205,
  updated_time: 1763914205,
  counted: true,
  manual: false,
  delta_amount: '12.345678',
  used_amount: '456.789000',
}

const clients: QueryClient[] = []

async function renderUsages(
  usages: PayableUsage[] = [baseUsage],
  initialUrl = '/payable-bills/?tab=usages'
) {
  vi.mocked(billApi.listPayableUsages).mockResolvedValue({
    success: true,
    data: { items: usages, total: usages.length, page: 1, page_size: 10 },
  })
  vi.mocked(billApi.getPayableBillOptions).mockResolvedValue({
    success: true,
    data: {
      sites: [
        { id: 3, name: 'Site A', deleted: false },
        { id: 4, name: 'Site B', deleted: true },
      ],
      providers: [
        { id: 5, username: 'alice', disabled: false, payable_discount: 0.8 },
      ],
    },
  })
  const i18n = createInstance()
  await i18n.init({ lng: 'en', resources: {}, initAsync: false })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  const root = createRootRoute()
  const auth = createRoute({ getParentRoute: () => root, id: '_authenticated' })
  const pageRoute = createRoute({
    getParentRoute: () => auth,
    path: 'payable-bills/',
    validateSearch: payableBillsSearchSchema,
    component: ChannelUsagesTable,
  })
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([pageRoute])]),
    history: createMemoryHistory({ initialEntries: [initialUrl] }),
  })
  await router.load()
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  )
}

function usageRow(channelName: string) {
  return screen.getByRole('row', { name: new RegExp(channelName) })
}

/** The cell of a row under the column with this header. */
function cellUnder(row: HTMLElement, header: string) {
  const headers = screen
    .getAllByRole('columnheader')
    .map((columnHeader) => columnHeader.textContent)
  return within(row).getAllByRole('cell')[headers.indexOf(header)]
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  // 00:30 on 2026-11-24 in Beijing, so the latest bill date is 2026-11-23.
  vi.setSystemTime(new Date('2026-11-23T16:30:00Z'))
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  localStorage.clear()
})

afterEach(() => {
  cleanup()
  localStorage.clear()
  clients.splice(0).forEach((client) => client.clear())
  vi.useRealTimers()
  vi.restoreAllMocks()
  vi.mocked(billApi.listPayableUsages).mockReset()
  vi.mocked(billApi.getPayableBillOptions).mockReset()
  vi.mocked(billApi.exportPayableUsages).mockReset()
})

describe('ChannelUsagesTable', () => {
  it('requests this Beijing month up to yesterday by default', async () => {
    await renderUsages()

    await waitFor(() =>
      expect(billApi.listPayableUsages).toHaveBeenCalledWith({
        p: 1,
        page_size: 10,
        start_date: 20261101,
        end_date: 20261123,
        site_ids: [],
        provider_ids: [],
      })
    )
  })

  it('lists the platform and site channel columns without a site URL column', async () => {
    await renderUsages()
    await screen.findByText('gpt-dcmark-3-17')

    expect(
      screen.getAllByRole('columnheader').map((header) => header.textContent)
    ).toEqual([
      'Task ID',
      'Bill Date',
      'Site ID',
      'Site Name',
      'Platform Channel ID',
      'Platform Channel Name',
      'Third-Party Site Channel ID',
      'Third-Party Site Channel Name',
      'Third-Party Site Channel Status',
      'Third-Party Site Channel Status Type',
      'Third-Party Site Channel Group',
      'Daily Amount Before Discount ($)',
      'Cumulative Amount ($)',
      'Provider Name',
      'Sync Time',
      'Actions',
    ])
    expect(screen.queryByText('https://a.example.com')).not.toBeInTheDocument()
  })

  it('shows the platform channel, site channel ID and groups of a pushed channel', async () => {
    await renderUsages([
      {
        ...baseUsage,
        remote_channel_id: 42,
        remote_channel_group: 'vip,default',
      },
    ])
    await screen.findByText('gpt-dcmark-3-17')

    const row = usageRow('gpt-dcmark-3-17')
    expect(cellUnder(row, 'Platform Channel ID')).toHaveTextContent(/^17$/)
    expect(cellUnder(row, 'Platform Channel Name')).toHaveTextContent(
      /^gpt-platform$/
    )
    expect(cellUnder(row, 'Third-Party Site Channel ID')).toHaveTextContent(
      /^42$/
    )
    const groups = cellUnder(row, 'Third-Party Site Channel Group')
    expect(within(groups).getByText('default')).toBeInTheDocument()
    expect(within(groups).getByText('vip')).toBeInTheDocument()
  })

  it.each([
    { status: 1, label: 'Enabled', type: '—' },
    { status: 2, label: 'Disabled', type: 'Manually disabled' },
    { status: 3, label: 'Disabled', type: 'Auto disabled' },
    { status: 0, label: 'Unknown', type: '—' },
  ])(
    'shows site channel status $status as $label with status type $type',
    async ({ status, label, type }) => {
      await renderUsages([{ ...baseUsage, remote_channel_status: status }])
      await screen.findByText('gpt-dcmark-3-17')

      const row = usageRow('gpt-dcmark-3-17')
      expect(
        cellUnder(row, 'Third-Party Site Channel Status')
      ).toHaveTextContent(new RegExp(`^${label}$`))
      expect(
        cellUnder(row, 'Third-Party Site Channel Status Type')
      ).toHaveTextContent(new RegExp(`^${type}$`))
    }
  )

  it('shows dashes when the row has no platform channel and no recorded site channel state', async () => {
    await renderUsages([
      {
        ...baseUsage,
        remote_channel_name: 'manual-channel',
        remote_channel_status: null,
        remote_channel_group: '',
        local_channel_id: 0,
        local_channel_name: null,
      },
    ])
    await screen.findByText('manual-channel')

    const row = usageRow('manual-channel')
    for (const header of [
      'Platform Channel ID',
      'Platform Channel Name',
      'Third-Party Site Channel Status',
      'Third-Party Site Channel Status Type',
      'Third-Party Site Channel Group',
    ]) {
      expect(cellUnder(row, header)).toHaveTextContent(/^—$/)
    }
  })

  it('says the platform channel does not exist when no channel has the marked ID', async () => {
    await renderUsages([
      { ...baseUsage, local_channel_id: 9, local_channel_name: null },
    ])
    await screen.findByText('gpt-dcmark-3-17')

    const row = usageRow('gpt-dcmark-3-17')
    expect(cellUnder(row, 'Platform Channel ID')).toHaveTextContent(/^9$/)
    expect(cellUnder(row, 'Platform Channel Name')).toHaveTextContent(
      /^Channel does not exist$/
    )
  })

  it('renders an em dash with the first-sync label when the delta is null', async () => {
    await renderUsages([
      {
        ...baseUsage,
        remote_channel_name: 'manual-channel',
        delta_quota: null,
        delta_flag: 'first_sync',
        delta_amount: null,
      },
    ])
    await screen.findByText('manual-channel')

    const delta = cellUnder(
      usageRow('manual-channel'),
      'Daily Amount Before Discount ($)'
    )
    expect(within(delta).getByText('—')).toBeInTheDocument()
    expect(
      within(delta).getByText('First sync, no baseline')
    ).toBeInTheDocument()
  })

  it('marks a rollback row and an uncounted row', async () => {
    await renderUsages([
      {
        ...baseUsage,
        id: 102,
        remote_channel_name: 'rollback-channel',
        delta_quota: 0,
        delta_flag: 'rollback',
        delta_amount: '0.000000',
      },
      {
        ...baseUsage,
        id: 103,
        remote_channel_name: 'uncounted-channel',
        counted: false,
      },
    ])
    await screen.findByText('rollback-channel')

    const rollback = usageRow('rollback-channel')
    expect(within(rollback).getByText('0.000000')).toBeInTheDocument()
    expect(
      within(rollback).getByText('Cumulative value rolled back')
    ).toBeInTheDocument()
    expect(
      within(rollback).queryByText(
        'Site pull incomplete, not counted in the bill'
      )
    ).not.toBeInTheDocument()
    const uncounted = usageRow('uncounted-channel')
    expect(within(uncounted).getByText('12.345678')).toBeInTheDocument()
    expect(
      within(uncounted).getByText(
        'Site pull incomplete, not counted in the bill'
      )
    ).toBeInTheDocument()
  })

  it('labels the amount of a new channel', async () => {
    await renderUsages([
      {
        ...baseUsage,
        remote_channel_name: 'new-one',
        delta_flag: 'new_channel',
      },
    ])
    await screen.findByText('new-one')

    const row = usageRow('new-one')
    expect(within(row).getByText('12.345678')).toBeInTheDocument()
    expect(within(row).getByText('New channel')).toBeInTheDocument()
  })

  it('marks only a manually set provider as manual and shows provider 0 as unknown', async () => {
    await renderUsages([
      {
        ...baseUsage,
        id: 104,
        remote_channel_name: 'manual-source',
        provider_source: 'manual',
        manual: true,
      },
      {
        // The override exists but no longer applies, so the name was parsed.
        ...baseUsage,
        id: 105,
        remote_channel_name: 'parsed-source',
        provider_source: 'parsed',
        manual: true,
      },
      {
        ...baseUsage,
        id: 106,
        remote_channel_name: 'no-provider',
        provider_id: 0,
        provider_name: '',
        provider_source: 'unknown',
      },
    ])
    await screen.findByText('manual-source')

    expect(
      within(usageRow('manual-source')).getByText('Manual')
    ).toBeInTheDocument()
    expect(
      within(usageRow('parsed-source')).queryByText('Manual')
    ).not.toBeInTheDocument()
    expect(within(usageRow('parsed-source')).getByText('alice')).toBeVisible()
    expect(
      within(usageRow('no-provider')).getByText('Unknown')
    ).toBeInTheDocument()
  })

  it('marks rows decided by site-level rules as Rule and keeps manual and unknown marks', async () => {
    await renderUsages([
      {
        ...baseUsage,
        id: 107,
        remote_channel_name: 'rule-all',
        provider_source: 'rule_all',
      },
      {
        ...baseUsage,
        id: 108,
        remote_channel_name: 'rule-unresolved',
        provider_source: 'rule_unresolved',
      },
      {
        ...baseUsage,
        id: 104,
        remote_channel_name: 'manual-source',
        provider_source: 'manual',
        manual: true,
      },
      {
        ...baseUsage,
        id: 106,
        remote_channel_name: 'no-provider',
        provider_id: 0,
        provider_name: '',
        provider_source: 'unknown',
      },
    ])
    await screen.findByText('rule-all')

    expect(screen.getAllByText('Rule')).toHaveLength(2)
    expect(within(usageRow('rule-all')).getByText('Rule')).toBeInTheDocument()
    expect(
      within(usageRow('rule-unresolved')).getByText('Rule')
    ).toBeInTheDocument()
    expect(screen.getAllByText('Manual')).toHaveLength(1)
    expect(screen.getAllByText('Unknown')).toHaveLength(1)
  })

  it('sends provider_ids=0 when the unknown provider filter is chosen', async () => {
    await renderUsages()
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
    await screen.findByText('gpt-dcmark-3-17')

    await user.click(screen.getByRole('button', { name: /^Provider/ }))
    await user.click(
      await screen.findByRole('option', { name: /Unknown provider/ })
    )

    await waitFor(() =>
      expect(billApi.listPayableUsages).toHaveBeenLastCalledWith(
        expect.objectContaining({ p: 1, provider_ids: [0] })
      )
    )
  })

  it('labels deleted sites in the site filter', async () => {
    await renderUsages()
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
    await screen.findByText('gpt-dcmark-3-17')

    await user.click(screen.getByRole('button', { name: /Site Name/ }))

    expect(await screen.findByText('Site B (deleted)')).toBeInTheDocument()
  })

  it('exports the usages of the current filters', async () => {
    vi.mocked(billApi.exportPayableUsages).mockResolvedValue(new Blob(['xlsx']))
    Object.assign(URL, {
      createObjectURL: vi.fn(() => 'blob:usages'),
      revokeObjectURL: vi.fn(),
    })
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    await renderUsages(
      [baseUsage],
      `/payable-bills/${defaultStringifySearch({
        tab: 'usages',
        start: 20261110,
        end: 20261120,
        sites: ['3'],
        providers: ['0', '5'],
      })}`
    )
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })

    await user.click(
      await screen.findByRole('button', { name: 'Export Excel' })
    )

    await waitFor(() =>
      expect(billApi.exportPayableUsages).toHaveBeenCalledWith({
        start_date: 20261110,
        end_date: 20261120,
        site_ids: [3],
        provider_ids: [0, 5],
      })
    )
  })

  it('opens the provider dialog from the row action', async () => {
    await renderUsages()
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })

    await user.click(
      await screen.findByRole('button', { name: 'Set Provider' })
    )

    expect(
      await screen.findByRole('dialog', { name: 'Set Provider' })
    ).toBeInTheDocument()
  })
})
