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
import { createFileRoute, redirect } from '@tanstack/react-router'
import z from 'zod'

import { PayableBills } from '@/features/payable-bills'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

export const payableBillsSearchSchema = z.object({
  tab: z
    .enum(['pulls', 'usages', 'providers', 'settlements'])
    .optional()
    .catch('pulls'),
  page: z.number().optional().catch(1),
  pageSize: z.number().optional().catch(10),
  // Shared by the usage details and provider bills tabs.
  start: z.number().optional().catch(undefined), // YYYYMMDD
  end: z.number().optional().catch(undefined),
  sites: z.array(z.string()).optional().catch([]),
  providers: z.array(z.string()).optional().catch([]), // '0' = unknown
  // The settlement methods tab filters on its own.
  settlementProviders: z.array(z.string()).optional().catch([]),
  methods: z.array(z.string()).optional().catch([]),
})

export const Route = createFileRoute('/_authenticated/payable-bills/')({
  beforeLoad: () => {
    const { auth } = useAuthStore.getState()

    if (!auth.user || auth.user.role < ROLE.SUPER_ADMIN) {
      throw redirect({
        to: '/403',
      })
    }
  },
  validateSearch: payableBillsSearchSchema,
  component: PayableBills,
})
