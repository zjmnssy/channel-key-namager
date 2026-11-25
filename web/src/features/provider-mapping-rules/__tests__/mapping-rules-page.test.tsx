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
import { type QueryClient, QueryClientProvider } from '@tanstack/react-query'
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
import { AxiosError, AxiosHeaders } from 'axios'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { toast } from 'sonner'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { createAppQueryClient } from '@/lib/query-client'
import {
  providerMappingRulesSearchSchema,
  Route as MappingRulesRoute,
} from '@/routes/_authenticated/provider-mapping-rules/index'
import { useAuthStore } from '@/stores/auth-store'

import { ProviderMappingRules } from '..'
import type { ProviderMappingRule } from '../types'

vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }))

const baseRule: ProviderMappingRule = {
  id: 1,
  site_id: 3,
  site_name: 'Alpha',
  site_deleted: false,
  rule_type: 'all',
  remote_channel_id: 0,
  remote_channel_name: '',
  provider_id: 5,
  provider_name: 'alice',
  provider_valid: true,
  created_time: 1,
  updated_time: 1,
}

const rules: ProviderMappingRule[] = [
  baseRule,
  {
    ...baseRule,
    id: 2,
    site_id: 4,
    site_name: 'Beta',
    site_deleted: true,
    rule_type: 'unresolved',
  },
  {
    ...baseRule,
    id: 3,
    rule_type: 'channel',
    remote_channel_id: 18704,
    remote_channel_name: 'az-gpt-eastus2-22536',
    provider_valid: false,
  },
]

const options = {
  sites: [
    { id: 3, name: 'Alpha', deleted: false },
    { id: 4, name: 'Beta', deleted: true },
  ],
  providers: [
    { id: 5, username: 'alice', disabled: false, payable_discount: null },
  ],
}

const OPTIONS_URL = '/api/payable_bill/options'
const RULES_URL = '/api/provider_mapping_rule/'

const clients: QueryClient[] = []

async function mount(initialUrl = '/provider-mapping-rules/') {
  const i18n = createInstance()
  await i18n.init({ lng: 'en', resources: {}, initAsync: false })
  const client = createAppQueryClient()
  clients.push(client)
  const root = createRootRoute()
  const auth = createRoute({ getParentRoute: () => root, id: '_authenticated' })
  const pageRoute = createRoute({
    getParentRoute: () => auth,
    path: 'provider-mapping-rules/',
    validateSearch: providerMappingRulesSearchSchema,
    component: ProviderMappingRules,
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

async function renderPage(
  items: ProviderMappingRule[] = rules,
  initialUrl = '/provider-mapping-rules/'
) {
  const get = vi.spyOn(api, 'get').mockImplementation(async (url: string) => {
    if (url === OPTIONS_URL) {
      return { data: { success: true, data: options } }
    }
    return {
      data: {
        success: true,
        data: { items, total: items.length, page: 1, page_size: 10 },
      },
    }
  })
  return { get, router: await mount(initialUrl) }
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
  vi.mocked(toast.error).mockClear()
})

describe('ProviderMappingRules page', () => {
  it('renders the rule columns and the marks of deleted sites and invalid providers', async () => {
    await renderPage()

    await screen.findByText('18704 az-gpt-eastus2-22536')
    for (const name of [
      'ID',
      'Third-party site',
      'Rule Type',
      'Site Channel',
      'Provider',
      'Updated At',
      'Actions',
    ]) {
      expect(screen.getByRole('columnheader', { name })).toBeInTheDocument()
    }
    const rowOf = (id: string) =>
      within(screen.getByRole('table'))
        .getByText(id, { selector: 'span' })
        .closest('tr') as HTMLElement
    expect(within(rowOf('1')).getByText('All site channels')).toBeVisible()
    expect(within(rowOf('1')).getByText('—')).toBeVisible()
    expect(within(rowOf('2')).getByText('Site deleted')).toBeVisible()
    expect(within(rowOf('2')).getByText('Unresolved channels')).toBeVisible()
    expect(within(rowOf('3')).getByText('Specified channel')).toBeVisible()
    expect(within(rowOf('3')).getByText('No longer valid')).toBeVisible()
    expect(within(rowOf('1')).queryByText('No longer valid')).toBeNull()
  })

  it('requests the selected filters and returns to the first page', async () => {
    const { get, router } = await renderPage(
      rules,
      '/provider-mapping-rules/?page=3'
    )
    const user = userEvent.setup()
    await screen.findByText('18704 az-gpt-eastus2-22536')

    await user.click(screen.getByRole('button', { name: /Third-party site/ }))
    await user.click(await screen.findByRole('option', { name: /^Alpha/ }))
    await user.keyboard('{Escape}')
    await user.click(screen.getByRole('button', { name: /Rule Type/ }))
    await user.click(
      await screen.findByRole('option', { name: /Specified channel/ })
    )
    await user.keyboard('{Escape}')
    await user.click(screen.getByRole('button', { name: /^Provider/ }))
    await user.click(await screen.findByRole('option', { name: /alice/ }))

    await waitFor(() => {
      const last = [...get.mock.calls]
        .reverse()
        .find((call) => call[0] === RULES_URL)
      expect(last?.[1]).toEqual({
        params: expect.objectContaining({
          p: 1,
          site_ids: '3',
          types: 'channel',
          provider_ids: '5',
        }),
      })
    })
    expect(router.state.location.search).toEqual({
      sites: ['3'],
      types: ['channel'],
      providers: ['5'],
    })
  })

  it('does not offer the unknown provider in the provider filter', async () => {
    await renderPage()
    const user = userEvent.setup()
    await screen.findByText('18704 az-gpt-eastus2-22536')

    await user.click(screen.getByRole('button', { name: /^Provider/ }))

    expect(await screen.findByRole('option', { name: /alice/ })).toBeVisible()
    expect(
      screen.queryByRole('option', { name: /Unknown provider/ })
    ).toBeNull()
  })

  it('shows the empty state with a hint to create a rule', async () => {
    await renderPage([])

    expect(await screen.findByText('No mapping rules')).toBeInTheDocument()
    expect(
      screen.getByText('Add a rule to assign site channels to a provider.')
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Create' })).toBeInTheDocument()
  })

  it('opens the create dialog from the Create button', async () => {
    await renderPage()
    const user = userEvent.setup()
    await screen.findByText('18704 az-gpt-eastus2-22536')

    await user.click(screen.getByRole('button', { name: 'Create' }))

    expect(
      await screen.findByRole('dialog', { name: 'Create Mapping Rule' })
    ).toBeInTheDocument()
  })

  it('opens the edit dialog of the row chosen in its menu', async () => {
    await renderPage()
    const user = userEvent.setup()
    const row = (await screen.findByText('18704 az-gpt-eastus2-22536')).closest(
      'tr'
    ) as HTMLElement

    await user.click(within(row).getByRole('button', { name: 'Open menu' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Edit' }))

    const dialog = await screen.findByRole('dialog', {
      name: 'Edit Mapping Rule',
    })
    expect(dialog).toHaveTextContent('18704 az-gpt-eastus2-22536')
  })

  it('opens the delete confirmation of the row chosen in its menu', async () => {
    await renderPage()
    const user = userEvent.setup()
    const row = (await screen.findByText('18704 az-gpt-eastus2-22536')).closest(
      'tr'
    ) as HTMLElement

    await user.click(within(row).getByRole('button', { name: 'Open menu' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Delete' }))

    expect(
      await screen.findByRole('alertdialog', { name: 'Delete Mapping Rule' })
    ).toHaveTextContent('Delete the mapping rule of Alpha?')
  })

  it('reports a load failure with the server reason', async () => {
    vi.spyOn(api, 'get').mockImplementation(async (url: string) => {
      if (url === OPTIONS_URL) {
        return { data: { success: true, data: options } }
      }
      throw new AxiosError(
        'Request failed',
        'ERR_BAD_REQUEST',
        undefined,
        undefined,
        {
          status: 400,
          statusText: 'Bad Request',
          headers: {},
          config: { headers: new AxiosHeaders() },
          data: { success: false, message: 'Site does not exist' },
        }
      )
    })
    await mount()

    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith('Site does not exist')
    )
  })

  it('reports an unsuccessful response with the fallback message', async () => {
    vi.spyOn(api, 'get').mockImplementation(async (url: string) => ({
      data:
        url === OPTIONS_URL
          ? { success: true, data: options }
          : { success: false },
    }))
    await mount()

    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith('Failed to load mapping rules')
    )
  })
})

describe('provider mapping rules route', () => {
  afterEach(() => {
    useAuthStore.setState(useAuthStore.getInitialState(), true)
  })

  it('redirects non super administrators', () => {
    useAuthStore.getState().auth.setUser({ id: 1, username: 'a', role: 10 })
    const beforeLoad = MappingRulesRoute.options.beforeLoad as () => void

    expect(() => beforeLoad()).toThrow(
      expect.objectContaining({
        options: expect.objectContaining({ to: '/403' }),
      })
    )
  })

  it('lets super administrators in', () => {
    useAuthStore.getState().auth.setUser({ id: 1, username: 'a', role: 100 })
    const beforeLoad = MappingRulesRoute.options.beforeLoad as () => void

    expect(() => beforeLoad()).not.toThrow()
  })
})
