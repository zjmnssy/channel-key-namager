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
  RouterProvider,
} from '@tanstack/react-router'
import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { formatTimestampToDate } from '@/lib/format'
import { Route as PayableBillsRoute } from '@/routes/_authenticated/payable-bills/index'
import { useAuthStore } from '@/stores/auth-store'

import * as billApi from '../../api'
import type { PayablePullSite, PayablePullTask } from '../../types'
import { PullTasksTable } from '../pull-tasks-table'

vi.mock('../../api')

const runningTask: PayablePullTask = {
  id: 12,
  bill_date: 20261123,
  site_total: 5,
  status: 'running',
  created_time: 1763914202,
  started_time: 1763914203,
  finished_time: 0,
  updated_time: 1763914300,
  finished_sites: 2,
  retrying_sites: 1,
  failed_sites: 1,
  mismatch_sites: 0,
  channel_total: 312,
  delta_amount: null,
  used_amount: null,
}

const finishedTask: PayablePullTask = {
  ...runningTask,
  id: 11,
  bill_date: 20261122,
  status: 'partial_failed',
  finished_time: 1763829280,
  finished_sites: 4,
  retrying_sites: 0,
  failed_sites: 1,
  delta_amount: '1234.567890',
  used_amount: '98765.432100',
}

const retryingSite: PayablePullSite = {
  id: 31,
  task_id: 12,
  site_id: 3,
  bill_date: 20261123,
  site_name: 'Site A',
  site_base_url: 'https://a.example.com',
  status: 'retrying',
  next_page: 3,
  pass: 1,
  reported_total: 250,
  channel_count: 200,
  attempts: 2,
  next_retry_at: 1763914500,
  finished_time: 0,
  created_time: 1763914202,
  updated_time: 1763914300,
  error: {
    key: 'Site request failed: {{detail}}',
    params: { detail: 'HTTP 502' },
  },
  note: null,
}

const clients: QueryClient[] = []

async function renderTasks(
  tasks: PayablePullTask[],
  sites: PayablePullSite[] = []
) {
  vi.mocked(billApi.listPayablePullTasks).mockResolvedValue({
    success: true,
    data: { items: tasks, total: tasks.length, page: 1, page_size: 10 },
  })
  vi.mocked(billApi.listPayablePullSites).mockResolvedValue({
    success: true,
    data: sites,
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
    component: PullTasksTable,
  })
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([pageRoute])]),
    history: createMemoryHistory({ initialEntries: ['/payable-bills/'] }),
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
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  localStorage.clear()
})

afterEach(() => {
  cleanup()
  localStorage.clear()
  clients.splice(0).forEach((client) => client.clear())
  vi.restoreAllMocks()
  vi.mocked(billApi.listPayablePullTasks).mockReset()
  vi.mocked(billApi.listPayablePullSites).mockReset()
})

describe('PullTasksTable', () => {
  it('shows progress and retrying count for a running task', async () => {
    await renderTasks([runningTask])

    // Sites that ended (2 succeeded + 1 failed) over the site total.
    expect(
      await screen.findByText('Pulling 3/5 sites, 1 retrying')
    ).toBeInTheDocument()
    // Finish time and both totals are unknown while the task runs.
    expect(screen.getAllByText('—')).toHaveLength(3)
  })

  it('shows totals with six decimals for a finished task', async () => {
    await renderTasks([finishedTask])

    expect(await screen.findByText('1234.567890')).toBeInTheDocument()
    expect(screen.getByText('98765.432100')).toBeInTheDocument()
  })

  it('shows succeeded sites over the total for a partially failed task', async () => {
    await renderTasks([finishedTask])

    expect(await screen.findByText('Partially failed 4/5')).toBeInTheDocument()
  })

  it('labels a task whose sites all succeeded as done', async () => {
    await renderTasks([
      {
        ...finishedTask,
        status: 'success',
        finished_sites: 5,
        failed_sites: 0,
      },
    ])

    expect(await screen.findByText('Done')).toBeInTheDocument()
  })

  it('notes the sites whose totals did not match on a done task', async () => {
    await renderTasks([
      {
        ...finishedTask,
        status: 'success',
        finished_sites: 5,
        failed_sites: 0,
        mismatch_sites: 2,
      },
    ])

    expect(
      await screen.findByText('Done (2 site(s) with mismatched totals)')
    ).toBeInTheDocument()
    expect(screen.queryByText('Done')).not.toBeInTheDocument()
  })

  it('opens the site drawer from the row action with the keyboard', async () => {
    await renderTasks([runningTask], [retryingSite])
    const user = userEvent.setup()
    const action = await screen.findByRole('button', { name: 'View Sites' })

    action.focus()
    await user.keyboard('{Enter}')

    const drawer = await screen.findByRole('dialog')
    expect(billApi.listPayablePullSites).toHaveBeenCalledWith(12)
    expect(
      await within(drawer).findByText('Site request failed: HTTP 502')
    ).toBeInTheDocument()
    expect(
      within(drawer).getByText(
        formatTimestampToDate(retryingSite.next_retry_at)
      )
    ).toBeInTheDocument()
  })

  it('labels a superseded site as failed and replaced by a new task', async () => {
    await renderTasks(
      [finishedTask],
      [{ ...retryingSite, status: 'superseded', next_retry_at: 0 }]
    )
    const user = userEvent.setup()

    await user.click(await screen.findByRole('button', { name: 'View Sites' }))

    const drawer = await screen.findByRole('dialog')
    expect(
      await within(drawer).findByText('Failed (replaced by a new task)')
    ).toBeInTheDocument()
  })
})

describe('payable bills route', () => {
  afterEach(() => {
    useAuthStore.setState(useAuthStore.getInitialState(), true)
  })

  it('redirects administrators who are not super administrators', () => {
    useAuthStore.getState().auth.setUser({ id: 1, username: 'a', role: 10 })
    const beforeLoad = PayableBillsRoute.options.beforeLoad as () => void

    expect(() => beforeLoad()).toThrow(
      expect.objectContaining({
        options: expect.objectContaining({ to: '/403' }),
      })
    )
  })

  it('lets super administrators in', () => {
    useAuthStore.getState().auth.setUser({ id: 1, username: 'a', role: 100 })
    const beforeLoad = PayableBillsRoute.options.beforeLoad as () => void

    expect(() => beforeLoad()).not.toThrow()
  })

  it('lets channel suppliers in to their own bills', () => {
    useAuthStore.getState().auth.setUser({ id: 5, username: 'a', role: 5 })
    const beforeLoad = PayableBillsRoute.options.beforeLoad as () => void

    expect(() => beforeLoad()).not.toThrow()
  })

  it('redirects common users', () => {
    useAuthStore.getState().auth.setUser({ id: 1, username: 'a', role: 1 })
    const beforeLoad = PayableBillsRoute.options.beforeLoad as () => void

    expect(() => beforeLoad()).toThrow(
      expect.objectContaining({
        options: expect.objectContaining({ to: '/403' }),
      })
    )
  })
})
