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
import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { formatTimestampToDate } from '@/lib/format'

import * as channelPushApi from '../../api'
import type {
  ChannelPushAction,
  ChannelPushRun,
  RunDetailKind,
  SiteSnapshot,
} from '../../types'
import { RunDetailDialog } from '../run-detail-dialogs'

vi.mock('../../api')

const run: ChannelPushRun = {
  id: 11,
  task_id: 3,
  site_id: 2,
  site_name: 'Alpha',
  target_group: 'default',
  suppliers: [{ id: 5, username: 'alice' }],
  status: 'partial',
  error: null,
  started_at: 1712345678,
  finished_at: 1712345690,
  site_channel_count: 25,
  target_channel_count: 0,
  unchanged_count: 0,
  enable_count: 0,
  disable_count: 0,
  update_models_count: 1,
  create_count: 0,
  orphan_count: 1,
  auto_disabled_count: 0,
  unsupported_count: 0,
  failed_action_count: 1,
}

const baseAction: ChannelPushAction = {
  category: 'update_models',
  local_channel_id: 7,
  local_channel_name: 'local-seven',
  remote_channel_id: 70,
  remote_channel_name: 'remote-seventy',
  status_from: 0,
  status_to: 0,
  models_added: ['a', 'b'],
  models_removed: ['c'],
  models: 'a,b',
  remote_status: 1,
  remote_models: 'c',
  duplicate: false,
  result: 'failed',
  error: { key: 'Site rejected the request', params: {} },
}

const orphanAction: ChannelPushAction = {
  ...baseAction,
  category: 'orphan',
  local_channel_id: 8,
  local_channel_name: '',
  remote_channel_id: 80,
  remote_channel_name: 'remote-eighty',
  models_added: null,
  models_removed: null,
  remote_status: 2,
  remote_models: 'old-model',
  reason: { key: 'Local channel was deleted', params: {} },
  result: 'skipped',
  error: undefined,
}

const clients: QueryClient[] = []

async function renderDialog(kind: RunDetailKind, data: unknown) {
  vi.mocked(channelPushApi.getChannelPushRunDetail).mockResolvedValue({
    success: true,
    data,
  })
  const i18n = createInstance()
  await i18n.init({ lng: 'en', resources: {}, initAsync: false })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <RunDetailDialog run={run} kind={kind} open onOpenChange={() => {}} />
      </QueryClientProvider>
    </I18nextProvider>
  )
  return screen.findByRole('dialog')
}

function siteSnapshot(count: number, usedQuota = 0): SiteSnapshot {
  return {
    fetched_at: 1712340000,
    channels: Array.from({ length: count }, (_, index) => ({
      id: index + 1,
      name: `site-channel-${index + 1}`,
      used_quota: usedQuota,
      status: 1,
      models: 'gpt-4o',
    })),
  }
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  cleanup()
  localStorage.clear()
  clients.splice(0).forEach((client) => client.clear())
  vi.restoreAllMocks()
  vi.mocked(channelPushApi.getChannelPushRunDetail).mockReset()
})

describe('RunDetailDialog', () => {
  it('site snapshot shows fetched time and paginates 25 channels', async () => {
    const dialog = await renderDialog('site', siteSnapshot(25))
    const user = userEvent.setup()

    expect(
      await within(dialog).findByText(formatTimestampToDate(1712340000), {
        exact: false,
      })
    ).toBeInTheDocument()
    expect(within(dialog).getByText('site-channel-10')).toBeInTheDocument()
    expect(within(dialog).queryByText('site-channel-11')).toBeNull()

    await user.click(
      within(dialog).getByRole('button', { name: 'Go to next page' })
    )

    expect(within(dialog).getByText('site-channel-11')).toBeInTheDocument()
    expect(within(dialog).queryByText('site-channel-10')).toBeNull()
  })

  it('site snapshot formats used quota with thousands separators', async () => {
    const dialog = await renderDialog('site', siteSnapshot(1, 1234567))

    expect(await within(dialog).findByText('1,234,567')).toBeInTheDocument()
  })

  it('actions dialog shows eight category tabs with counts', async () => {
    const dialog = await renderDialog('actions', [baseAction, orphanAction])

    const tabs = await within(dialog).findAllByRole('tab')
    expect(tabs.map((tab) => tab.textContent)).toEqual([
      'No change needed0',
      'Enable channel0',
      'Disable channel0',
      'Update model list1',
      'Create new0',
      'Orphan channel1',
      'Auto-disabled on site0',
      'Unsupported for push0',
    ])
  })

  it('actions dialog shows model changes, orphan reason and failure reason', async () => {
    const dialog = await renderDialog('actions', [baseAction, orphanAction])
    const user = userEvent.setup()

    expect(await within(dialog).findByText('+a, b / −c')).toBeInTheDocument()
    expect(
      within(dialog).getByText('Site rejected the request')
    ).toBeInTheDocument()

    await user.click(
      within(dialog).getByRole('tab', { name: /Orphan channel/ })
    )

    expect(
      within(dialog).getByText('Local channel was deleted')
    ).toBeInTheDocument()
    expect(
      within(dialog).getByText('Manually disabled · old-model')
    ).toBeInTheDocument()
    expect(within(dialog).getByText('Not called')).toBeInTheDocument()
  })

  it('actions dialog marks duplicates', async () => {
    const dialog = await renderDialog('actions', [
      { ...baseAction, duplicate: true },
    ])

    expect(await within(dialog).findByText('Duplicate')).toBeInTheDocument()
  })

  it('shows an empty state when details were not saved', async () => {
    const dialog = await renderDialog('site', null)

    expect(
      await within(dialog).findByText('Details were not saved')
    ).toBeInTheDocument()
  })
})
