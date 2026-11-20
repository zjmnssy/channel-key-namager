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
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ChannelPush } from '..'
import * as channelPushApi from '../api'

vi.mock('../api')

const clients: QueryClient[] = []

async function renderPage() {
  vi.mocked(channelPushApi.getChannelPushTasks).mockResolvedValue({
    success: true,
    data: { items: [], total: 0, page: 1, page_size: 10 },
  })
  vi.mocked(channelPushApi.getChannelPushRuns).mockResolvedValue({
    success: true,
    data: { items: [], total: 0, page: 1, page_size: 10 },
  })
  vi.mocked(channelPushApi.getChannelPushOptions).mockResolvedValue({
    success: true,
    data: { sites: [], suppliers: [] },
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
    path: 'channel-push/',
    component: ChannelPush,
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

describe('ChannelPush page', () => {
  it('shows the task list and the new task button on the tasks tab', async () => {
    await renderPage()

    expect(await screen.findByText('No Push Tasks Found')).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'New push task' })
    ).toBeInTheDocument()
  })

  it('switching to the runs tab shows the runs table and hides the new task button', async () => {
    await renderPage()
    const user = userEvent.setup()
    await screen.findByText('No Push Tasks Found')

    await user.click(screen.getByRole('tab', { name: 'Task Results' }))

    expect(await screen.findByText('No Push Results Found')).toBeInTheDocument()
    expect(screen.queryByText('No Push Tasks Found')).toBeNull()
    expect(screen.queryByRole('button', { name: 'New push task' })).toBeNull()
  })
})
