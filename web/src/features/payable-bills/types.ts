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
import type {
  Page,
  ReceivableBillSite,
} from '@/features/receivable-bills/types'

export type { ApiResponse, Page } from '@/features/receivable-bills/types'

/** A backend message; render it with `t(message.key, message.params)`. */
export type ServerMessage = { key: string; params?: Record<string, unknown> }

export type PayablePullTaskStatus = 'running' | 'success' | 'partial_failed'

export type PayablePullTask = {
  id: number
  bill_date: number // YYYYMMDD
  site_total: number
  status: PayablePullTaskStatus
  created_time: number
  started_time: number // 0 = not started
  finished_time: number // 0 = not finished
  updated_time: number
  finished_sites: number // succeeded
  retrying_sites: number
  failed_sites: number // failed and superseded
  channel_total: number
  delta_amount: string | null // six decimals; null while running
  used_amount: string | null
}

export type PayablePullSiteStatus =
  | 'pending'
  | 'retrying'
  | 'success'
  | 'failed'
  | 'superseded'

export type PayablePullSite = {
  id: number
  task_id: number
  site_id: number
  bill_date: number
  site_name: string
  site_base_url: string
  status: PayablePullSiteStatus
  next_page: number // pages pulled = next_page - 1
  pass: number
  reported_total: number
  channel_count: number
  attempts: number
  next_retry_at: number // 0 = none
  finished_time: number
  created_time: number
  updated_time: number
  error: ServerMessage | null
  note: ServerMessage | null
}

export type PayableDeltaFlag =
  | 'normal'
  | 'first_sync'
  | 'new_channel'
  | 'rollback'

export type PayableProviderSource =
  | 'manual'
  | 'parsed'
  | 'inherited'
  | 'unknown'

export type PayableUsage = {
  id: number
  task_id: number
  pull_site_id: number
  bill_date: number
  site_id: number
  site_name: string
  site_base_url: string
  remote_channel_id: number
  remote_channel_name: string
  used_quota: number
  delta_quota: number | null // null on the first sync
  delta_flag: PayableDeltaFlag
  quota_per_unit: number
  provider_id: number // 0 = unknown
  provider_name: string
  provider_source: PayableProviderSource
  payable_discount: number | null
  synced_at: number
  created_time: number
  updated_time: number
  counted: boolean // false when the site pull did not complete
  manual: boolean // a manual provider setting exists for this channel
  delta_amount: string | null
  used_amount: string
}

export type PayableBillFilter = {
  start_date: number
  end_date: number
  site_ids?: number[]
  provider_ids?: number[] // 0 = unknown provider
}

export type PayableBillQuery = PayableBillFilter & {
  p: number
  page_size: number
}

export type PayableProviderBill = {
  bill_date: number
  provider_id: number // 0 = unknown provider
  provider_name: string
  amount: string
  discount: number | null
  discount_mixed: boolean
  discount_missing: boolean
  payable_amount: string | null
}

export type PayableProviderBillPage = Page<PayableProviderBill> & {
  incomplete_site_count: number
}

export type PayableProviderOption = {
  id: number
  username: string
  disabled: boolean
  payable_discount: number | null
}

export type PayableBillOptions = {
  sites: ReceivableBillSite[]
  providers: PayableProviderOption[]
}
