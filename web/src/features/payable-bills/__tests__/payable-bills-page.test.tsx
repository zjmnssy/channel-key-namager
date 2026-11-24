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
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { payableBillsSearchSchema } from '@/routes/_authenticated/payable-bills/index'

import { PayableBills } from '..'
import * as billApi from '../api'

vi.mock('../api')

const clients: QueryClient[] = []

async function renderPage(initialUrl = '/payable-bills/') {
  vi.mocked(billApi.listPayablePullTasks).mockResolvedValue({
    success: true,
    data: { items: [], total: 0, page: 1, page_size: 10 },
  })
  vi.mocked(billApi.listPayableUsages).mockResolvedValue({
    success: true,
    data: { items: [], total: 0, page: 1, page_size: 10 },
  })
  vi.mocked(billApi.listPayableProviderBills).mockResolvedValue({
    success: true,
    data: {
      items: [],
      total: 0,
      page: 1,
      page_size: 10,
      incomplete_site_count: 0,
    },
  })
  vi.mocked(billApi.getPayableBillOptions).mockResolvedValue({
    success: true,
    data: { sites: [], providers: [] },
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
    component: PayableBills,
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
  return router
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
  vi.mocked(billApi.listPayablePullTasks).mockReset()
  vi.mocked(billApi.listPayableUsages).mockReset()
  vi.mocked(billApi.listPayableProviderBills).mockReset()
  vi.mocked(billApi.getPayableBillOptions).mockReset()
})

describe('PayableBills page', () => {
  it('reloads the pull tasks when Refresh is clicked', async () => {
    await renderPage()
    const user = userEvent.setup()
    await waitFor(() =>
      expect(billApi.listPayablePullTasks).toHaveBeenCalledTimes(1)
    )
    const refresh = screen.getByRole('button', { name: 'Refresh' })
    await waitFor(() => expect(refresh).toBeEnabled())

    await user.click(refresh)

    await waitFor(() =>
      expect(billApi.listPayablePullTasks).toHaveBeenCalledTimes(2)
    )
  })

  it('loads the channel usage details on the usages tab', async () => {
    await renderPage('/payable-bills/?tab=usages')

    await waitFor(() =>
      expect(billApi.listPayableUsages).toHaveBeenCalledTimes(1)
    )
    expect(billApi.listPayablePullTasks).not.toHaveBeenCalled()
  })

  it('loads the provider bills on the providers tab', async () => {
    await renderPage('/payable-bills/?tab=providers')

    await waitFor(() =>
      expect(billApi.listPayableProviderBills).toHaveBeenCalledTimes(1)
    )
    expect(billApi.listPayableUsages).not.toHaveBeenCalled()
  })

  it('starts the next tab from its first page and keeps the shared filters', async () => {
    const router = await renderPage(
      `/payable-bills/${defaultStringifySearch({
        tab: 'pulls',
        page: 3,
        start: 20261101,
        sites: ['2'],
      })}`
    )
    const user = userEvent.setup()

    await user.click(
      await screen.findByRole('tab', { name: 'Channel Usage Details' })
    )

    await waitFor(() =>
      expect(router.state.location.search).toEqual({
        tab: 'usages',
        start: 20261101,
        sites: ['2'],
      })
    )
  })
})
