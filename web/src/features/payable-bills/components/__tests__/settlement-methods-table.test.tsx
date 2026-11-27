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

import { payableBillsSearchSchema } from '@/routes/_authenticated/payable-bills/index'

import * as billApi from '../../api'
import type { PayableSettlement } from '../../types'
import { SettlementMethodsTable } from '../settlement-methods-table'

vi.mock('../../api')

const baseSettlement: PayableSettlement = {
  provider_id: 5,
  username: 'alice',
  disabled: false,
  payable_discount: 0.8,
  method: 'usage',
  updated_time: 1764145211,
  whole_sites: [{ site_id: 3, site_name: 'Site A', conflict: false }],
}

const CONFLICT_HINT =
  'Some channels of this site are assigned to other providers, so it is still settled by channel details'

const clients: QueryClient[] = []

async function renderSettlements(
  settlements: PayableSettlement[] = [baseSettlement],
  options: { initialUrl?: string; total?: number } = {}
) {
  vi.mocked(billApi.listPayableSettlementMethods).mockResolvedValue({
    success: true,
    data: {
      items: settlements,
      total: options.total ?? settlements.length,
      page: 1,
      page_size: 10,
    },
  })
  vi.mocked(billApi.getPayableBillOptions).mockResolvedValue({
    success: true,
    data: {
      sites: [],
      providers: [
        { id: 5, username: 'alice', disabled: false, payable_discount: 0.8 },
        { id: 6, username: 'bob', disabled: true, payable_discount: null },
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
    component: SettlementMethodsTable,
  })
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([pageRoute])]),
    history: createMemoryHistory({
      initialEntries: [options.initialUrl ?? '/payable-bills/?tab=settlements'],
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

function settlementRow(providerName: string) {
  return screen.getByRole('row', { name: new RegExp(providerName) })
}

/** The cell of a row under the column with this header. */
function cellUnder(row: HTMLElement, header: string) {
  const headers = screen
    .getAllByRole('columnheader')
    .map((columnHeader) => columnHeader.textContent)
  return within(row).getAllByRole('cell')[headers.indexOf(header)]
}

beforeEach(() => {
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  localStorage.clear()
})

afterEach(() => {
  cleanup()
  localStorage.clear()
  clients.splice(0).forEach((client) => client.clear())
  vi.restoreAllMocks()
  vi.mocked(billApi.listPayableSettlementMethods).mockReset()
  vi.mocked(billApi.getPayableBillOptions).mockReset()
})

describe('SettlementMethodsTable', () => {
  it('requests the first page without filters by default', async () => {
    await renderSettlements()

    await waitFor(() =>
      expect(billApi.listPayableSettlementMethods).toHaveBeenCalledWith({
        p: 1,
        page_size: 10,
        provider_ids: [],
        methods: [],
      })
    )
  })

  it('shows a provider that was never set as channel details settlement with no update time', async () => {
    await renderSettlements([
      {
        ...baseSettlement,
        provider_id: 6,
        username: 'bob',
        payable_discount: null,
        method: 'channel',
        updated_time: 0,
        whole_sites: [],
      },
    ])
    await screen.findByText('bob')

    const row = settlementRow('bob')
    expect(
      within(row).getByText('Channel details settlement')
    ).toBeInTheDocument()
    expect(within(row).getByText('Not set')).toBeInTheDocument()
    expect(cellUnder(row, 'Updated At')).toHaveTextContent(/^—$/)
    expect(cellUnder(row, 'Fully Assigned Sites')).toHaveTextContent(/^—$/)
  })

  it('marks a disabled provider and its fully assigned sites', async () => {
    await renderSettlements([
      {
        ...baseSettlement,
        provider_id: 6,
        username: 'cpsupplier3',
        disabled: true,
        whole_sites: [
          { site_id: 3, site_name: 'Site A', conflict: false },
          { site_id: 4, site_name: '本地测试', conflict: true },
        ],
      },
    ])
    await screen.findByText('cpsupplier3 (disabled)')

    const row = settlementRow('cpsupplier3')
    expect(within(row).getByText('cpsupplier3 (disabled)')).toBeInTheDocument()
    expect(within(row).getByText('Site A')).toBeInTheDocument()
    const conflictSite = within(row).getByText('本地测试')
    expect(screen.queryByText(CONFLICT_HINT)).not.toBeInTheDocument()
    // Keyboard users reach the hint through the conflicting site's label.
    const conflictLabel = conflictSite.closest<HTMLElement>(
      '[data-slot="tooltip-trigger"]'
    )

    act(() => conflictLabel?.focus())

    expect(conflictLabel).toHaveFocus()
    expect(await screen.findByText(CONFLICT_HINT)).toBeInTheDocument()
  })

  it('filters by settlement method', async () => {
    await renderSettlements([baseSettlement], {
      initialUrl: `/payable-bills/${defaultStringifySearch({
        tab: 'settlements',
        page: 2,
      })}`,
      total: 30,
    })
    const user = userEvent.setup()
    await screen.findByText('alice')

    await user.click(screen.getByRole('button', { name: /Settlement Method/ }))
    await user.click(
      await screen.findByRole('option', { name: /Usage details settlement/ })
    )

    await waitFor(() =>
      expect(billApi.listPayableSettlementMethods).toHaveBeenLastCalledWith(
        expect.objectContaining({ p: 1, methods: ['usage'] })
      )
    )
  })

  it('filters by provider without offering the unknown provider', async () => {
    await renderSettlements()
    const user = userEvent.setup()
    await screen.findByText('alice')

    await user.click(screen.getByRole('button', { name: /^Provider/ }))
    expect(
      await screen.findByRole('option', { name: /bob \(disabled\)/ })
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('option', { name: /Unknown provider/ })
    ).not.toBeInTheDocument()
    await user.click(screen.getByRole('option', { name: /alice/ }))

    await waitFor(() =>
      expect(billApi.listPayableSettlementMethods).toHaveBeenLastCalledWith(
        expect.objectContaining({ p: 1, provider_ids: [5] })
      )
    )
  })

  it('ignores settlement methods it does not know in the URL', async () => {
    await renderSettlements([baseSettlement], {
      initialUrl: `/payable-bills/${defaultStringifySearch({
        tab: 'settlements',
        methods: ['unknown', 'usage'],
      })}`,
    })

    await waitFor(() =>
      expect(billApi.listPayableSettlementMethods).toHaveBeenCalledWith({
        p: 1,
        page_size: 10,
        provider_ids: [],
        methods: ['usage'],
      })
    )
  })

  it('clears only its own filters on reset', async () => {
    const router = await renderSettlements([baseSettlement], {
      initialUrl: `/payable-bills/${defaultStringifySearch({
        tab: 'settlements',
        page: 2,
        start: 20261110,
        providers: ['5'],
        settlementProviders: ['5'],
        methods: ['usage'],
      })}`,
      total: 30,
    })
    const user = userEvent.setup()
    // The chosen provider is also named on its filter button.
    await screen.findByRole('row', { name: /alice/ })

    await user.click(screen.getByRole('button', { name: 'Reset' }))

    // The date and provider filters belong to the details and provider bills tabs.
    await waitFor(() =>
      expect(router.state.location.search).toEqual({
        tab: 'settlements',
        start: 20261110,
        providers: ['5'],
      })
    )
  })

  it('opens the change dialog of the row', async () => {
    await renderSettlements()
    const user = userEvent.setup()

    await user.click(await screen.findByRole('button', { name: 'Modify' }))

    expect(
      await screen.findByRole('dialog', {
        name: 'Change settlement method: alice',
      })
    ).toBeInTheDocument()
  })
})
