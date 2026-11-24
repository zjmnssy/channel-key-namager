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
import type { StatusVariant } from '@/components/status-badge'

import type {
  PayableBillQuery,
  PayablePullSiteStatus,
  PayablePullTaskStatus,
} from './types'

export const PAYABLE_BILL_QUERY_KEYS = {
  all: ['payable-bills'],
  tasks: (p: number, size: number) => ['payable-bills', 'tasks', p, size],
  taskSites: (id: number) => ['payable-bills', 'task-sites', id],
  usages: (query: PayableBillQuery) => ['payable-bills', 'usages', query],
  providers: (query: PayableBillQuery) => ['payable-bills', 'providers', query],
  options: ['payable-bills', 'options'],
} as const

/** The provider filter value of rows whose provider is unknown. */
export const UNKNOWN_PROVIDER_VALUE = '0'

export const PULL_TASK_STATUS_VARIANTS: Record<
  PayablePullTaskStatus,
  StatusVariant
> = {
  running: 'info',
  success: 'success',
  partial_failed: 'warning',
}

export const PULL_SITE_STATUS_DISPLAY: Record<
  PayablePullSiteStatus,
  { labelKey: string; variant: StatusVariant }
> = {
  pending: { labelKey: 'Waiting', variant: 'neutral' },
  retrying: { labelKey: 'Retrying', variant: 'warning' },
  success: { labelKey: 'Success', variant: 'success' },
  failed: { labelKey: 'Failed', variant: 'danger' },
  superseded: {
    labelKey: 'Failed (replaced by a new task)',
    variant: 'danger',
  },
}
