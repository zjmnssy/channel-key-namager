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

import type { DailyBillQuery, ReceivableBillStatus, TrendQuery } from './types'

export const RECEIVABLE_BILL_QUERY_KEYS = {
  all: ['receivable-bills'],
  daily: (query: DailyBillQuery) => ['receivable-bills', 'daily', query],
  trend: (query: TrendQuery) => ['receivable-bills', 'trend', query],
  sites: ['receivable-bills', 'sites'],
} as const

export const BILL_STATUS_DISPLAY: Record<
  ReceivableBillStatus,
  { labelKey: string; variant: StatusVariant }
> = {
  success: { labelKey: 'Success', variant: 'success' },
  retrying: { labelKey: 'Retrying', variant: 'warning' },
  failed: { labelKey: 'Failed', variant: 'danger' },
}

export const BILL_STATUS_OPTIONS: {
  value: ReceivableBillStatus
  labelKey: string
}[] = (Object.keys(BILL_STATUS_DISPLAY) as ReceivableBillStatus[]).map(
  (value) => ({ value, labelKey: BILL_STATUS_DISPLAY[value].labelKey })
)
