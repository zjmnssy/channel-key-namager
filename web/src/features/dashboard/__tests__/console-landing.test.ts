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
import { afterEach, describe, expect, it } from 'vitest'

import { ROLE } from '@/lib/roles'
import { Route as ConsoleEntryRoute } from '@/routes/_authenticated/dashboard/index'
import { type AuthUser, useAuthStore } from '@/stores/auth-store'

// Every sign-in and every "Console" link opens /dashboard; Overview and
// Dashboard are hidden from the menus, so the route picks the landing page.
describe('console entry route', () => {
  afterEach(() => {
    useAuthStore.setState(useAuthStore.getInitialState(), true)
  })

  // The session's admin_permissions is the effective matrix, so an
  // administrator carries the channel read grant from the role baseline.
  const channelUsers: Array<[string, AuthUser]> = [
    [
      'an administrator',
      {
        id: 1,
        username: 'admin',
        role: ROLE.ADMIN,
        permissions: { admin_permissions: { channel: { read: true } } },
      },
    ],
    [
      'the super administrator',
      { id: 2, username: 'root', role: ROLE.SUPER_ADMIN },
    ],
    [
      'a channel supplier with channel read access',
      {
        id: 3,
        username: 'supplier',
        role: ROLE.CHANNEL_SUPPLIER,
        permissions: { admin_permissions: { channel: { read: true } } },
      },
    ],
  ]

  it.each(channelUsers)('sends %s to Channels', (_, user) => {
    useAuthStore.getState().auth.setUser(user)
    const beforeLoad = ConsoleEntryRoute.options.beforeLoad as () => void

    expect(() => beforeLoad()).toThrow(
      expect.objectContaining({
        options: expect.objectContaining({ to: '/channels' }),
      })
    )
  })

  const otherUsers: Array<[string, AuthUser]> = [
    ['a common user', { id: 4, username: 'user', role: ROLE.USER }],
    [
      'a channel supplier without channel read access',
      { id: 5, username: 'supplier', role: ROLE.CHANNEL_SUPPLIER },
    ],
    [
      'an administrator whose channel read access was revoked',
      {
        id: 6,
        username: 'admin2',
        role: ROLE.ADMIN,
        permissions: { admin_permissions: { channel: { read: false } } },
      },
    ],
  ]

  it.each(otherUsers)('sends %s to Usage Logs', (_, user) => {
    useAuthStore.getState().auth.setUser(user)
    const beforeLoad = ConsoleEntryRoute.options.beforeLoad as () => void

    expect(() => beforeLoad()).toThrow(
      expect.objectContaining({
        options: expect.objectContaining({
          to: '/usage-logs/$section',
          params: { section: 'common' },
        }),
      })
    )
  })
})
