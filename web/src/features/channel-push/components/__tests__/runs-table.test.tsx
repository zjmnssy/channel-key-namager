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
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import * as channelPushApi from '../../api'
import type { ChannelPushRun } from '../../types'
import { RunsTable } from '../runs-table'

vi.mock('../../api')

const baseRun: ChannelPushRun = {
  id: 11,
  task_id: 3,
  site_id: 2,
  site_name: 'Alpha',
  target_group: 'default',
  suppliers: [{ id: 5, username: 'alice' }],
  status: 'success',
  error: null,
  started_at: 1712345678,
  finished_at: 1712345690,
  site_channel_count: 4,
  target_channel_count: 3,
  unchanged_count: 0,
  enable_count: 1,
  disable_count: 0,
  update_models_count: 0,
  create_count: 2,
  orphan_count: 0,
  auto_disabled_count: 0,
  unsupported_count: 0,
  failed_action_count: 0,
}

const clients: QueryClient[] = []

async function renderRuns(
  runs: ChannelPushRun[] = [baseRun],
  initialUrl = '/channel-push/?tab=runs'
) {
  vi.mocked(channelPushApi.getChannelPushRuns).mockResolvedValue({
    success: true,
    data: { items: runs, total: runs.length, page: 1, page_size: 10 },
  })
  vi.mocked(channelPushApi.getChannelPushOptions).mockResolvedValue({
    success: true,
    data: {
      sites: [{ id: 2, name: 'Alpha' }],
      suppliers: [{ id: 5, username: 'alice', display_name: 'Alice' }],
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
    path: 'channel-push/',
    component: RunsTable,
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
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  localStorage.clear()
})

afterEach(() => {
  cleanup()
  localStorage.clear()
  clients.splice(0).forEach((client) => client.clear())
  vi.restoreAllMocks()
  vi.mocked(channelPushApi.getChannelPushRuns).mockReset()
})

describe('RunsTable', () => {
  it('sends start range, supplier and site filters as request params', async () => {
    await renderRuns(
      [baseRun],
      '/channel-push/?tab=runs&startFrom=100&startTo=200&supplierId=5&siteId=2'
    )

    await waitFor(() =>
      expect(channelPushApi.getChannelPushRuns).toHaveBeenCalledWith({
        p: 1,
        page_size: 10,
        start_from: 100,
        start_to: 200,
        supplier_user_id: 5,
        site_id: 2,
      })
    )
  })

  it('reset clears every filter', async () => {
    await renderRuns(
      [baseRun],
      '/channel-push/?tab=runs&startFrom=100&startTo=200&supplierId=5&siteId=2'
    )
    const user = userEvent.setup()
    await screen.findByText('Alpha')

    await user.click(screen.getByRole('button', { name: 'Reset' }))

    await waitFor(() =>
      expect(
        vi.mocked(channelPushApi.getChannelPushRuns).mock.lastCall?.[0]
      ).toEqual({ p: 1, page_size: 10 })
    )
  })

  it('shows only non-zero categories in the action summary', async () => {
    await renderRuns()

    expect(
      await screen.findByRole('button', { name: 'Enable 1 · Create 2' })
    ).toBeInTheDocument()
  })

  it('disables detail links while a run is running', async () => {
    await renderRuns([{ ...baseRun, status: 'running' }])

    expect(
      await screen.findByRole('button', { name: '4 items' })
    ).toBeDisabled()
    expect(screen.getByRole('button', { name: '3 items' })).toBeDisabled()
    expect(
      screen.getByRole('button', { name: 'Enable 1 · Create 2' })
    ).toBeDisabled()
  })

  it('shows the failure reason on hover for a failed run', async () => {
    await renderRuns([
      {
        ...baseRun,
        status: 'failed',
        error: { key: 'Site has been deleted', params: {} },
      },
    ])
    const user = userEvent.setup()

    await user.hover(await screen.findByText('Failed'))

    const tooltip = await waitFor(() => {
      const content = document.querySelector('[data-slot="tooltip-content"]')
      expect(content).not.toBeNull()
      return content
    })
    expect(tooltip).toHaveTextContent('Site has been deleted')
  })
})
