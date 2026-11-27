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
  act,
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

import { ROLE } from '@/lib/roles'
import { payableBillsSearchSchema } from '@/routes/_authenticated/payable-bills/index'
import { useAuthStore } from '@/stores/auth-store'

import * as billApi from '../../api'
import type {
  ApiResponse,
  PayableProviderBill,
  PayableProviderBillPage,
} from '../../types'
import { ProviderBillsTable } from '../provider-bills-table'

vi.mock('../../api')

const baseBill: PayableProviderBill = {
  bill_date: 20261123,
  provider_id: 5,
  provider_name: 'alice',
  settlement_method: 'channel',
  amount: '123.456789',
  discount: 0.8,
  discount_mixed: false,
  discount_missing: false,
  payable_amount: '98.765431',
}

// One provider-day settled both ways has a row for each settlement method.
const usageBill: PayableProviderBill = {
  ...baseBill,
  settlement_method: 'usage',
  amount: '207.779237',
  payable_amount: '166.223390',
}
const channelBill: PayableProviderBill = {
  ...baseBill,
  amount: '12.345678',
  payable_amount: '9.876542',
}

const INCOMPLETE_NOTICE =
  'In the selected dates, {{count}} site syncs did not succeed, so the bill may be incomplete'
const MISMATCH_NOTICE =
  '{{count}} site(s) reported more channels than were pulled in the selected dates; the bills may be incomplete'
const UNSYNCED_NOTICE =
  'In the selected dates, {{count}} sites have daily bills that have not synced successfully; they are settled by channel details for now'

const clients: QueryClient[] = []

function providerBillPage(
  bills: PayableProviderBill[],
  counts: {
    incompleteSiteCount?: number
    mismatchSiteCount?: number
    unsyncedUsageSiteCount?: number
  } = {}
): ApiResponse<PayableProviderBillPage> {
  return {
    success: true,
    data: {
      items: bills,
      total: bills.length,
      page: 1,
      page_size: 10,
      incomplete_site_count: counts.incompleteSiteCount ?? 0,
      mismatch_site_count: counts.mismatchSiteCount ?? 0,
      unsynced_usage_site_count: counts.unsyncedUsageSiteCount ?? 0,
    },
  }
}

async function renderBills(
  bills: PayableProviderBill[] = [baseBill],
  options: {
    initialUrl?: string
    incompleteSiteCount?: number
    mismatchSiteCount?: number
    unsyncedUsageSiteCount?: number
    /** The whole response, when it differs from the default page. */
    page?: ApiResponse<PayableProviderBillPage>
  } = {}
) {
  vi.mocked(billApi.listPayableProviderBills).mockResolvedValue(
    options.page ?? providerBillPage(bills, options)
  )
  vi.mocked(billApi.getPayableBillOptions).mockResolvedValue({
    success: true,
    data: {
      sites: [{ id: 3, name: 'Site A', deleted: false }],
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
    component: ProviderBillsTable,
  })
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([pageRoute])]),
    history: createMemoryHistory({
      initialEntries: [options.initialUrl ?? '/payable-bills/?tab=providers'],
    }),
  })
  await router.load()
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  )
  return router
}

function billRow(providerName: string) {
  return screen.getByRole('row', { name: new RegExp(providerName) })
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
  useAuthStore.setState(useAuthStore.getInitialState(), true)
  clients.splice(0).forEach((client) => client.clear())
  vi.useRealTimers()
  vi.restoreAllMocks()
  vi.mocked(billApi.listPayableProviderBills).mockReset()
  vi.mocked(billApi.getPayableBillOptions).mockReset()
  vi.mocked(billApi.exportPayableProviderBills).mockReset()
})

describe('ProviderBillsTable', () => {
  it('requests this Beijing month up to yesterday by default', async () => {
    await renderBills()

    await waitFor(() =>
      expect(billApi.listPayableProviderBills).toHaveBeenCalledWith({
        p: 1,
        page_size: 10,
        start_date: 20261101,
        end_date: 20261123,
        site_ids: [],
        provider_ids: [],
      })
    )
  })

  it('shows the discount and payable amount of a complete bill', async () => {
    await renderBills()
    await screen.findByText('alice')

    const row = billRow('alice')
    expect(within(row).getByText('2026-11-23')).toBeInTheDocument()
    expect(within(row).getByText('123.456789')).toBeInTheDocument()
    expect(within(row).getByText('0.8')).toBeInTheDocument()
    expect(within(row).getByText('98.765431')).toBeInTheDocument()
    expect(within(row).queryByText('Incomplete amount')).not.toBeInTheDocument()
  })

  it('shows Multiple for mixed discounts and an em dash for a missing payable amount', async () => {
    await renderBills([
      {
        ...baseBill,
        provider_id: 6,
        provider_name: 'mixed-provider',
        discount: null,
        discount_mixed: true,
      },
      {
        ...baseBill,
        provider_id: 7,
        provider_name: 'unset-provider',
        discount: null,
        discount_missing: true,
        payable_amount: null,
      },
    ])
    await screen.findByText('mixed-provider')

    const mixed = billRow('mixed-provider')
    expect(within(mixed).getByText('Multiple')).toBeInTheDocument()
    expect(within(mixed).queryByText('Not set')).not.toBeInTheDocument()
    const unset = billRow('unset-provider')
    expect(within(unset).getByText('Not set')).toBeInTheDocument()
    expect(within(unset).getByText('—')).toBeInTheDocument()
    expect(
      within(unset).queryByText('Incomplete amount')
    ).not.toBeInTheDocument()
  })

  it('explains a mixed discount when its label gets keyboard focus', async () => {
    await renderBills([{ ...baseBill, discount: null, discount_mixed: true }])
    const label = await screen.findByText('Multiple')

    act(() => label.focus())

    expect(label).toHaveFocus()
    expect(
      await screen.findByText('The rows of this day have different discounts')
    ).toBeInTheDocument()
  })

  it('marks the payable amount incomplete whenever some rows have no discount', async () => {
    await renderBills([
      {
        ...baseBill,
        provider_id: 6,
        provider_name: 'partly-unset',
        discount: null,
        discount_missing: true,
        payable_amount: '40.000000',
      },
      {
        ...baseBill,
        provider_id: 7,
        provider_name: 'mixed-and-unset',
        discount: null,
        discount_mixed: true,
        discount_missing: true,
        payable_amount: '12.500000',
      },
    ])
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
    await screen.findByText('partly-unset')

    const partly = billRow('partly-unset')
    expect(within(partly).getByText('Not set')).toBeInTheDocument()
    expect(within(partly).getByText('40.000000')).toBeInTheDocument()
    expect(within(partly).getByText('Incomplete amount')).toBeInTheDocument()
    const mixed = billRow('mixed-and-unset')
    expect(within(mixed).getByText('Multiple')).toBeInTheDocument()
    expect(within(mixed).getByText('12.500000')).toBeInTheDocument()
    expect(within(mixed).getByText('Incomplete amount')).toBeInTheDocument()

    await user.hover(within(partly).getByText('Incomplete amount'))

    expect(
      await screen.findByText(
        'Some rows have no discount and are not included in the payable amount'
      )
    ).toBeInTheDocument()
  })

  it('shows provider 0 as the unknown provider', async () => {
    await renderBills([
      {
        ...baseBill,
        provider_id: 0,
        provider_name: '',
        discount: null,
        discount_missing: true,
        payable_amount: null,
      },
    ])

    expect(await screen.findByText('Unknown provider')).toBeInTheDocument()
  })

  it('shows the settlement method of each bill', async () => {
    await renderBills([usageBill, channelBill])
    await screen.findByText('207.779237')

    expect(
      screen.getAllByRole('columnheader').map((header) => header.textContent)
    ).toEqual([
      'Bill Date',
      'Provider',
      'Settlement Method',
      'Amount Before Discount ($)',
      'Discount (Payable)',
      'Payable Amount ($)',
    ])
    expect(
      within(screen.getByRole('row', { name: /207\.779237/ })).getByText(
        'Usage details settlement'
      )
    ).toBeInTheDocument()
    expect(
      within(screen.getByRole('row', { name: /12\.345678/ })).getByText(
        'Channel details settlement'
      )
    ).toBeInTheDocument()
  })

  it('keeps exactly the two bills of a provider-day settled both ways when the list changes', async () => {
    await renderBills([
      { ...baseBill, provider_id: 4, provider_name: 'carol' },
      usageBill,
      channelBill,
    ])
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
    await screen.findByText('carol')
    vi.mocked(billApi.listPayableProviderBills).mockResolvedValue(
      providerBillPage([usageBill, channelBill])
    )

    await user.click(screen.getByRole('button', { name: /^Provider/ }))
    await user.click(await screen.findByRole('option', { name: /alice/ }))
    await waitFor(() =>
      expect(screen.queryByText('carol')).not.toBeInTheDocument()
    )

    // The header row and the two bills, with no row left over from before.
    expect(screen.getAllByRole('row')).toHaveLength(3)
    expect(screen.getAllByText('Usage details settlement')).toHaveLength(1)
    expect(screen.getAllByText('Channel details settlement')).toHaveLength(1)
  })

  it('shows the incomplete-sites notice when some pulls did not finish', async () => {
    await renderBills([baseBill], { incompleteSiteCount: 2 })

    const notice = await screen.findByRole('alert')
    expect(notice).toHaveTextContent(
      INCOMPLETE_NOTICE.replace('{{count}}', '2')
    )
  })

  it('shows no notice when every pull in the dates finished', async () => {
    await renderBills()
    await screen.findByText('alice')

    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('shows the mismatched-totals notice when sites reported more channels than were pulled', async () => {
    await renderBills([baseBill], { mismatchSiteCount: 3 })

    const notice = await screen.findByRole('alert')
    expect(notice).toHaveTextContent(MISMATCH_NOTICE.replace('{{count}}', '3'))
  })

  it('shows both notices when some pulls failed and others had mismatched totals', async () => {
    await renderBills([baseBill], {
      incompleteSiteCount: 2,
      mismatchSiteCount: 1,
      unsyncedUsageSiteCount: 0,
    })
    await screen.findByText('alice')

    const notices = await screen.findAllByRole('alert')
    expect(notices).toHaveLength(2)
    expect(notices[0]).toHaveTextContent(
      INCOMPLETE_NOTICE.replace('{{count}}', '2')
    )
    expect(notices[1]).toHaveTextContent(
      MISMATCH_NOTICE.replace('{{count}}', '1')
    )
  })

  it('shows the unsynced daily bills notice when whole sites are settled by channel details for now', async () => {
    await renderBills([baseBill], { unsyncedUsageSiteCount: 2 })

    const notice = await screen.findByRole('alert')
    expect(notice).toHaveTextContent(UNSYNCED_NOTICE.replace('{{count}}', '2'))
  })

  it('shows an em dash for the discount of a bill without consumption', async () => {
    await renderBills([
      {
        ...baseBill,
        amount: '0.000000',
        discount: null,
        payable_amount: '0.000000',
      },
    ])
    await screen.findByText('alice')

    const row = billRow('alice')
    expect(within(row).getByText('—')).toBeInTheDocument()
    expect(within(row).queryByText('Not set')).not.toBeInTheDocument()
    expect(within(row).getAllByText('0.000000')).toHaveLength(2)
  })

  it('has no sync result filter', async () => {
    await renderBills()
    await screen.findByText('alice')

    expect(
      screen.getByRole('button', { name: /Site Name/ })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: /^Provider/ })
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: /Sync Result/ })
    ).not.toBeInTheDocument()
  })

  it('sends provider_ids=0 when the unknown provider filter is chosen', async () => {
    await renderBills()
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
    await screen.findByText('alice')

    await user.click(screen.getByRole('button', { name: /^Provider/ }))
    await user.click(
      await screen.findByRole('option', { name: /Unknown provider/ })
    )

    await waitFor(() =>
      expect(billApi.listPayableProviderBills).toHaveBeenLastCalledWith(
        expect.objectContaining({ p: 1, provider_ids: [0] })
      )
    )
  })

  it('clears the shared date, site and provider filters on reset', async () => {
    const router = await renderBills([baseBill], {
      initialUrl: `/payable-bills/${defaultStringifySearch({
        tab: 'providers',
        page: 2,
        start: 20261110,
        end: 20261120,
        sites: ['3'],
        providers: ['5'],
      })}`,
    })
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
    await screen.findByText('alice')

    await user.click(screen.getByRole('button', { name: 'Reset' }))

    await waitFor(() =>
      expect(router.state.location.search).toEqual({ tab: 'providers' })
    )
  })

  it('exports the provider bills of the current filters', async () => {
    vi.mocked(billApi.exportPayableProviderBills).mockResolvedValue(
      new Blob(['xlsx'])
    )
    Object.assign(URL, {
      createObjectURL: vi.fn(() => 'blob:providers'),
      revokeObjectURL: vi.fn(),
    })
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    await renderBills([baseBill], {
      initialUrl: `/payable-bills/${defaultStringifySearch({
        tab: 'providers',
        start: 20261110,
        end: 20261120,
        sites: ['3'],
        providers: ['0', '5'],
      })}`,
    })
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })

    await user.click(
      await screen.findByRole('button', { name: 'Export Excel' })
    )

    await waitFor(() =>
      expect(billApi.exportPayableProviderBills).toHaveBeenCalledWith({
        start_date: 20261110,
        end_date: 20261120,
        site_ids: [3],
        provider_ids: [0, 5],
      })
    )
  })
})

describe('ProviderBillsTable for a channel supplier', () => {
  // A channel supplier gets one bill a day, not split by settlement method,
  // and no site sync counts.
  const supplierBills: PayableProviderBill[] = [
    {
      bill_date: 20261123,
      provider_id: 5,
      provider_name: 'alice',
      amount: '220.124915',
      discount: 0.8,
      discount_mixed: false,
      discount_missing: false,
      payable_amount: '176.099932',
    },
    {
      bill_date: 20261122,
      provider_id: 5,
      provider_name: 'alice',
      amount: '2.000000',
      discount: 0.8,
      discount_mixed: false,
      discount_missing: false,
      payable_amount: '1.600000',
    },
  ]
  const supplierPage: ApiResponse<PayableProviderBillPage> = {
    success: true,
    data: { items: supplierBills, total: 2, page: 1, page_size: 10 },
  }

  beforeEach(() => {
    useAuthStore
      .getState()
      .auth.setUser({ id: 5, username: 'alice', role: ROLE.CHANNEL_SUPPLIER })
  })

  it('lists one bill a day without a settlement method column', async () => {
    await renderBills(supplierBills, { page: supplierPage })
    await screen.findByText('220.124915')

    expect(
      screen.getAllByRole('columnheader').map((header) => header.textContent)
    ).toEqual([
      'Bill Date',
      'Provider',
      'Amount Before Discount ($)',
      'Discount (Payable)',
      'Payable Amount ($)',
    ])
    expect(screen.getByText('2.000000')).toBeInTheDocument()
  })

  it('offers only the date filter and never loads the site and provider options', async () => {
    await renderBills(supplierBills, { page: supplierPage })
    await screen.findByText('220.124915')

    expect(
      screen.queryByRole('button', { name: /Site Name/ })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: /^Provider/ })
    ).not.toBeInTheDocument()
    expect(billApi.getPayableBillOptions).not.toHaveBeenCalled()
  })
})
