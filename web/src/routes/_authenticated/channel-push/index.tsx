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

import { ChannelPush } from '@/features/channel-push'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

export const channelPushSearchSchema = z.object({
  tab: z.enum(['tasks', 'runs']).optional().catch('tasks'),
  taskPage: z.number().optional().catch(1),
  taskPageSize: z.number().optional().catch(10),
  runPage: z.number().optional().catch(1),
  runPageSize: z.number().optional().catch(10),
  startFrom: z.number().optional().catch(undefined), // Unix seconds
  startTo: z.number().optional().catch(undefined),
  supplierId: z.number().optional().catch(undefined),
  siteId: z.number().optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/channel-push/')({
  beforeLoad: () => {
    const { auth } = useAuthStore.getState()

    if (!auth.user || auth.user.role < ROLE.ADMIN) {
      throw redirect({
        to: '/403',
      })
    }
  },
  validateSearch: channelPushSearchSchema,
  component: ChannelPush,
})
