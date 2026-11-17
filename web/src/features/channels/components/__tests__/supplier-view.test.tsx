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
  flexRender,
  getCoreRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { cleanup, render, renderHook, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import type { ReactNode } from 'react'
import { I18nextProvider, initReactI18next } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import type { Channel } from '../../types'
import { useChannelsColumns } from '../channels-columns'
import { ChannelsPrimaryButtons } from '../channels-primary-buttons'
import { ChannelsProvider } from '../channels-provider'

const i18n = createInstance().use(initReactI18next)
await i18n.init({
  lng: 'en',
  resources: { en: { translation: {} } },
  initAsync: false,
  interpolation: { escapeValue: false },
})

const originalAuth = useAuthStore.getState().auth
const clients: QueryClient[] = []

function signInAs(role: number) {
  useAuthStore.setState({
    auth: { ...originalAuth, user: { id: 1, username: 'u', role } },
  })
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  cleanup()
  localStorage.clear()
  clients.splice(0).forEach((client) => client.clear())
  useAuthStore.setState({ auth: originalAuth })
})

function newClient() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  return client
}

function Providers(props: { children: ReactNode }) {
  return (
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={clients[0] ?? newClient()}>
        <ChannelsProvider>{props.children}</ChannelsProvider>
      </QueryClientProvider>
    </I18nextProvider>
  )
}

const allSiteActions = [
  'Test All Channels',
  'Update All Balances',
  'Detect All Upstream Updates',
  'Apply All Upstream Updates',
  'Repair Channel Consistency',
  'Delete All Disabled',
]

async function openMoreActions() {
  const root = createRootRoute()
  const auth = createRoute({ getParentRoute: () => root, id: '_authenticated' })
  const channelsRoute = createRoute({
    getParentRoute: () => auth,
    path: 'channels/',
    component: () => (
      <Providers>
        <ChannelsPrimaryButtons />
      </Providers>
    ),
  })
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([channelsRoute])]),
    history: createMemoryHistory({ initialEntries: ['/channels/'] }),
  })
  await router.load()
  render(<RouterProvider router={router} />)
  await userEvent
    .setup()
    .click(await screen.findByRole('button', { name: 'More actions' }))
}

describe('More actions menu', () => {
  it('hides every site-wide action when a channel supplier opens it', async () => {
    signInAs(ROLE.CHANNEL_SUPPLIER)
    await openMoreActions()

    expect(
      await screen.findByRole('menuitemcheckbox', { name: 'Tag Mode' })
    ).toBeInTheDocument()
    for (const label of allSiteActions) {
      expect(screen.queryByText(label)).not.toBeInTheDocument()
    }
  })

  it('lists every site-wide action when an administrator opens it', async () => {
    signInAs(ROLE.ADMIN)
    await openMoreActions()

    for (const label of allSiteActions) {
      expect(await screen.findByText(label)).toBeInTheDocument()
    }
  })
})

function channelWith(creator: Partial<Channel>): Channel {
  return { id: 3, name: 'prod', ...creator } as Channel
}

function renderColumns() {
  return renderHook(() => useChannelsColumns(), {
    wrapper: Providers,
  }).result.current
}

function CreatorCell(props: { channel: Channel }) {
  const columns = useChannelsColumns({ enableSelection: false })
  const table = useReactTable({
    data: [props.channel],
    columns,
    getCoreRowModel: getCoreRowModel(),
  })
  const cell = table
    .getRowModel()
    .rows[0].getVisibleCells()
    .find((c) => c.column.id === 'creator')
  return cell ? flexRender(cell.column.columnDef.cell, cell.getContext()) : null
}

function renderCreatorCell(channel: Channel) {
  signInAs(ROLE.ADMIN)
  render(
    <Providers>
      <CreatorCell channel={channel} />
    </Providers>
  )
}

describe('creator column', () => {
  it('is absent from the columns when a channel supplier views the table', () => {
    signInAs(ROLE.CHANNEL_SUPPLIER)
    const ids = renderColumns().map((column) => column.id ?? '')
    expect(ids).not.toContain('creator')
  })

  it('follows the name column when an administrator views the table', () => {
    signInAs(ROLE.ADMIN)
    const ids = renderColumns().map(
      (column) =>
        column.id ?? (column as { accessorKey?: string }).accessorKey ?? ''
    )
    expect(ids).toContain('creator')
    expect(ids.indexOf('creator')).toBe(ids.indexOf('name') + 1)
  })

  it('shows the username when the creator still exists', () => {
    renderCreatorCell(
      channelWith({ creator_user_id: 7, creator_username: 'alice' })
    )
    expect(screen.getByText('alice')).toBeInTheDocument()
  })

  it('shows a deleted-user label when the creator id is set but the username is empty', () => {
    renderCreatorCell(channelWith({ creator_user_id: 7, creator_username: '' }))
    expect(screen.getByText('Deleted user #7')).toBeInTheDocument()
  })

  it('shows a dash when the channel has no creator', () => {
    renderCreatorCell(channelWith({ creator_user_id: 0, creator_username: '' }))
    expect(screen.getByText('—')).toBeInTheDocument()
  })
})
