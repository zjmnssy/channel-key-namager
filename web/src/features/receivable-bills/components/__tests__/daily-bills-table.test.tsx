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

import { Route as ReceivableBillsRoute } from '@/routes/_authenticated/receivable-bills/index'
import { useAuthStore } from '@/stores/auth-store'

import * as billApi from '../../api'
import type { ReceivableBill } from '../../types'
import { DailyBillsTable } from '../daily-bills-table'

vi.mock('../../api')
// The page route pulls in the trend chart, which VChart cannot load under node.
vi.mock('@visactor/react-vchart', () => ({ VChart: () => null }))

const baseBill: ReceivableBill = {
  id: 1,
  site_id: 2,
  bill_date: 20261122,
  site_name: 'Site A',
  site_base_url: 'https://a.example.com',
  quota: 61728394,
  quota_per_unit: 500000,
  receivable_discount: 0.8,
  status: 'success',
  attempts: 0,
  synced_at: 1700000000,
  next_retry_at: 0,
  created_time: 1700000000,
  updated_time: 1700000000,
  amount: '123.456788',
  error: null,
  site_deleted: false,
}

const clients: QueryClient[] = []

async function renderBills(
  bills: ReceivableBill[] = [baseBill],
  initialUrl = '/receivable-bills/?tab=daily'
) {
  vi.mocked(billApi.listReceivableBills).mockResolvedValue({
    success: true,
    data: { items: bills, total: bills.length, page: 1, page_size: 10 },
  })
  vi.mocked(billApi.listReceivableBillSites).mockResolvedValue({
    success: true,
    data: [
      { id: 1, name: 'Site A', deleted: false },
      { id: 2, name: 'Site B', deleted: true },
    ],
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
    path: 'receivable-bills/',
    component: DailyBillsTable,
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

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-11-22T16:30:00Z'))
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  localStorage.clear()
})

afterEach(() => {
  cleanup()
  localStorage.clear()
  clients.splice(0).forEach((client) => client.clear())
  vi.useRealTimers()
  vi.restoreAllMocks()
  vi.mocked(billApi.listReceivableBills).mockReset()
})

describe('DailyBillsTable', () => {
  it('requests the current Beijing month by default', async () => {
    await renderBills()

    await waitFor(() =>
      expect(billApi.listReceivableBills).toHaveBeenCalledWith(
        expect.objectContaining({
          start_date: 20261101,
          end_date: 20261123,
          p: 1,
          page_size: 10,
        })
      )
    )
  })

  it('shows amounts only for successful bills', async () => {
    await renderBills([
      baseBill,
      {
        ...baseBill,
        id: 2,
        site_name: 'Site R',
        status: 'retrying',
        attempts: 3,
        amount: null,
      },
      {
        ...baseBill,
        id: 3,
        site_name: 'Site F',
        status: 'failed',
        amount: null,
      },
    ])

    expect(await screen.findByText('123.456788')).toBeInTheDocument()
    expect(screen.getByText('Retrying (attempt 3)')).toBeInTheDocument()
    expect(screen.getByText('Failed')).toBeInTheDocument()
    expect(screen.getAllByText('—')).toHaveLength(4)
  })

  it('sends selected sites and statuses as filters', async () => {
    await renderBills(
      [baseBill],
      `/receivable-bills/${defaultStringifySearch({
        tab: 'daily',
        sites: ['2'],
        statuses: ['failed'],
      })}`
    )

    await waitFor(() =>
      expect(billApi.listReceivableBills).toHaveBeenCalledWith(
        expect.objectContaining({ site_ids: [2], statuses: ['failed'] })
      )
    )
  })

  it('exports the current filters', async () => {
    vi.mocked(billApi.exportReceivableBills).mockResolvedValue(
      new Blob(['xlsx'])
    )
    const createObjectURL = vi.fn(() => 'blob:bills')
    Object.assign(URL, { createObjectURL, revokeObjectURL: vi.fn() })
    const downloads: string[] = []
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
      this: HTMLAnchorElement
    ) {
      downloads.push(this.download)
    })
    await renderBills()
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })

    await user.click(
      await screen.findByRole('button', { name: 'Export Excel' })
    )

    await waitFor(() =>
      expect(billApi.exportReceivableBills).toHaveBeenCalledWith({
        start_date: 20261101,
        end_date: 20261123,
      })
    )
    await waitFor(() =>
      expect(downloads).toEqual([
        'All Sites-Receivable Bills-20261101-20261123.xlsx',
      ])
    )
  })

  it('labels deleted sites in the site filter', async () => {
    await renderBills()
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })

    await user.click(await screen.findByRole('button', { name: /Site/ }))

    expect(await screen.findByText('Site B (deleted)')).toBeInTheDocument()
  })
})

describe('receivable bills route', () => {
  afterEach(() => {
    useAuthStore.setState(useAuthStore.getInitialState(), true)
  })

  it('redirects administrators who are not super administrators', () => {
    useAuthStore.getState().auth.setUser({ id: 1, username: 'a', role: 10 })
    const beforeLoad = ReceivableBillsRoute.options.beforeLoad as () => void

    expect(() => beforeLoad()).toThrow(
      expect.objectContaining({
        options: expect.objectContaining({ to: '/403' }),
      })
    )
  })

  it('lets super administrators in', () => {
    useAuthStore.getState().auth.setUser({ id: 1, username: 'a', role: 100 })
    const beforeLoad = ReceivableBillsRoute.options.beforeLoad as () => void

    expect(() => beforeLoad()).not.toThrow()
  })
})
