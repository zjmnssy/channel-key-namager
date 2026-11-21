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
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance, type i18n as I18n } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { toast } from 'sonner'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { formatTimestampToDate } from '@/lib/format'

import { api } from '../../../../lib/api'
import { CHANNEL_PUSH_QUERY_KEYS } from '../../constants'
import type { ChannelPushTask } from '../../types'
import { ChannelPushDialogs } from '../channel-push-dialogs'
import { ChannelPushProvider } from '../channel-push-provider'
import { TasksTable } from '../tasks-table'

const periodicTask: ChannelPushTask = {
  id: 1,
  site_id: 9,
  site_name: 'Alpha',
  site_deleted: false,
  suppliers: [
    { id: 5, username: 'alice' },
    { id: 6, username: 'bob' },
  ],
  task_type: 'periodic',
  interval_minutes: 30,
  target_group: 'default',
  enabled: true,
  next_run_at: 0,
  last_finished_at: 1712345678,
  created_time: 1700000000,
}

const clients: QueryClient[] = []
let i18n: I18n

async function renderPage(items: ChannelPushTask[] = [periodicTask]) {
  const get = vi.spyOn(api, 'get').mockImplementation(async (url: string) => {
    if (url === '/api/channel_push/options') {
      return {
        data: {
          success: true,
          data: {
            sites: [{ id: 9, name: 'Alpha' }],
            suppliers: [{ id: 5, username: 'alice', display_name: 'Alice' }],
          },
        },
      }
    }
    if (url === '/api/channel_push/sites/9/groups') {
      return { data: { success: true, data: ['default'] } }
    }
    return {
      data: {
        success: true,
        data: { items, total: items.length, page: 1, page_size: 10 },
      },
    }
  })
  i18n = createInstance()
  await i18n.init({ lng: 'en', resources: {}, initAsync: false })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  const root = createRootRoute()
  const auth = createRoute({ getParentRoute: () => root, id: '_authenticated' })
  const pageRoute = createRoute({
    getParentRoute: () => auth,
    path: 'channel-push/',
    component: () => (
      <ChannelPushProvider>
        <TasksTable />
        <ChannelPushDialogs />
      </ChannelPushProvider>
    ),
  })
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([pageRoute])]),
    history: createMemoryHistory({ initialEntries: ['/channel-push/'] }),
  })
  await router.load()
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  )
  return get
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
})

function listRequests(get: { mock: { calls: unknown[][] } }) {
  return get.mock.calls.filter((call) => call[0] === '/api/channel_push/tasks')
}

describe('TasksTable', () => {
  it('renders every column for a periodic task with two suppliers', async () => {
    await renderPage()

    expect(await screen.findByText('Alpha')).toBeInTheDocument()
    for (const name of [
      'Task ID',
      'Supplier User ID',
      'Supplier Username',
      'Third-Party Site ID',
      'Third-Party Site Name',
      'Is Enabled',
      'Task Type',
      'Last Push Time',
      'Created Time',
      'Actions',
    ]) {
      expect(screen.getByRole('columnheader', { name })).toBeInTheDocument()
    }
    expect(screen.getByText('5, 6')).toBeInTheDocument()
    expect(screen.getByText('alice, bob')).toBeInTheDocument()
    expect(screen.getByText('9')).toBeInTheDocument()
    expect(screen.getByText('Periodic · every 30 minutes')).toBeInTheDocument()
    expect(
      screen.getByText(formatTimestampToDate(periodicTask.last_finished_at))
    ).toBeInTheDocument()
    expect(
      screen.getByText(formatTimestampToDate(periodicTask.created_time))
    ).toBeInTheDocument()
  })

  it('shows once for a one-off task', async () => {
    await renderPage([
      { ...periodicTask, task_type: 'once', interval_minutes: 0 },
    ])

    expect(await screen.findByText('Once')).toBeInTheDocument()
  })

  it('shows a dash when the task has never run', async () => {
    await renderPage([{ ...periodicTask, last_finished_at: 0 }])

    expect(await screen.findByText('Alpha')).toBeInTheDocument()
    expect(screen.getByText('—')).toBeInTheDocument()
  })

  it('marks a deleted site', async () => {
    await renderPage([{ ...periodicTask, site_deleted: true }])

    expect(await screen.findByText('Alpha')).toBeInTheDocument()
    expect(screen.getByText('Site deleted')).toBeInTheDocument()
  })

  it('disables a task from the row menu and refreshes the list', async () => {
    const get = await renderPage()
    const user = userEvent.setup()
    const success = vi.spyOn(toast, 'success').mockImplementation(() => 1)
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValue({ data: { success: true } })

    await screen.findByText('Alpha')
    expect(listRequests(get)).toHaveLength(1)
    await user.click(screen.getByRole('button', { name: 'Open menu' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Disable' }))

    await waitFor(() =>
      expect(post).toHaveBeenCalledWith('/api/channel_push/tasks/1/enabled', {
        enabled: false,
      })
    )
    await waitFor(() => expect(listRequests(get)).toHaveLength(2))
    expect(success).toHaveBeenCalledWith('Task disabled')
  })

  it('enables a task from the row menu and says it will start soon', async () => {
    const get = await renderPage([{ ...periodicTask, enabled: false }])
    const user = userEvent.setup()
    const success = vi.spyOn(toast, 'success').mockImplementation(() => 1)
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValue({ data: { success: true } })

    await screen.findByText('Alpha')
    await user.click(screen.getByRole('button', { name: 'Open menu' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Enable' }))

    await waitFor(() =>
      expect(post).toHaveBeenCalledWith('/api/channel_push/tasks/1/enabled', {
        enabled: true,
      })
    )
    await waitFor(() => expect(listRequests(get)).toHaveLength(2))
    expect(success).toHaveBeenCalledWith(
      'Saved. The task will start within half a minute'
    )
  })

  it('keeps the task unchanged and reports the error when enabling fails', async () => {
    const get = await renderPage([{ ...periodicTask, enabled: false }])
    const user = userEvent.setup()
    const success = vi.spyOn(toast, 'success').mockImplementation(() => 1)
    const error = vi.spyOn(toast, 'error').mockImplementation(() => 1)
    vi.spyOn(api, 'post').mockResolvedValue({
      data: { success: false, message: 'Task does not exist' },
    })

    await screen.findByText('Alpha')
    await user.click(screen.getByRole('button', { name: 'Open menu' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Enable' }))

    await waitFor(() =>
      expect(error).toHaveBeenCalledWith('Task does not exist')
    )
    expect(success).not.toHaveBeenCalled()
    expect(listRequests(get)).toHaveLength(1)
  })

  it('opens the edit dialog from the row menu', async () => {
    await renderPage()
    const user = userEvent.setup()

    await screen.findByText('Alpha')
    await user.click(screen.getByRole('button', { name: 'Open menu' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Modify' }))

    const dialog = await screen.findByRole('dialog', { name: 'Edit push task' })
    expect(
      await within(dialog).findByRole('combobox', { name: 'Third-party site' })
    ).toHaveTextContent('Alpha')
  })

  it('keeps the edits in an open edit dialog when the list refetches', async () => {
    const get = await renderPage()
    const user = userEvent.setup()

    await screen.findByText('Alpha')
    await user.click(screen.getByRole('button', { name: 'Open menu' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Modify' }))
    const dialog = await screen.findByRole('dialog', { name: 'Edit push task' })
    const interval = await within(dialog).findByLabelText('Interval (minutes)')
    await user.clear(interval)
    await user.type(interval, '45')

    const callsBefore = listRequests(get).length
    await clients.at(-1)?.invalidateQueries({
      queryKey: CHANNEL_PUSH_QUERY_KEYS.tasks,
    })

    await waitFor(() =>
      expect(listRequests(get).length).toBeGreaterThan(callsBefore)
    )
    expect(within(dialog).getByLabelText('Interval (minutes)')).toHaveValue(
      '45'
    )
  })

  it('shows the empty state when there are no tasks', async () => {
    await renderPage([])

    expect(await screen.findByText('No Push Tasks Found')).toBeInTheDocument()
  })
})
