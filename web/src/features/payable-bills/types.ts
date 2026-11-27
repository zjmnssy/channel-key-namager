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
  mismatch_sites: number // succeeded with fewer channels than the site reported
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
  | 'rule_all'
  | 'rule_unresolved'
  | 'parsed'
  | 'inherited'
  | 'unknown'

export type PayableUsage = {
  id: number
  task_id: number
  // The site and its pull are left out of a channel supplier's rows.
  pull_site_id?: number
  bill_date: number
  site_id?: number
  site_name?: string
  site_base_url?: string
  remote_channel_id: number
  remote_channel_name: string
  // Site channel status when pulled: 1 enabled, 2 manually disabled,
  // 3 auto disabled, anything else unknown; null on rows pulled before it was recorded.
  remote_channel_status: number | null
  remote_channel_group: string // comma-separated; empty when not recorded
  local_channel_id: number // platform channel in the push marker; 0 = none
  local_channel_name: string | null // null when the platform channel no longer exists
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
  // The site's day is settled by its daily bill, so the row is not counted
  // whether or not its pull completed.
  usage_settled: boolean
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
  // A provider-day settled both ways has a row for each method. A channel
  // supplier gets one row a day with both added up, and no method.
  settlement_method?: PayableSettlementMethod
  amount: string
  discount: number | null
  discount_mixed: boolean
  discount_missing: boolean
  payable_amount: string | null
}

// The site sync counts cover every site, so a channel supplier gets none.
export type PayableProviderBillPage = Page<PayableProviderBill> & {
  incomplete_site_count?: number
  mismatch_site_count?: number // succeeded with fewer channels than the site reported
  // Site-days that would be settled by usage details but whose daily bills
  // have not synced successfully, so channel details settle them for now.
  unsynced_usage_site_count?: number
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

/** How a provider's daily consumption becomes payable bills. */
export type PayableSettlementMethod = 'channel' | 'usage'

/** A site whose "all site channels" rule points to the provider. */
export type PayableWholeSite = {
  site_id: number
  site_name: string
  // Some channels are assigned to other providers, so the site is still
  // settled by channel details.
  conflict: boolean
}

export type PayableSettlement = {
  provider_id: number
  username: string
  disabled: boolean
  payable_discount: number | null
  method: PayableSettlementMethod // channel when never set
  updated_time: number // 0 = never set
  whole_sites: PayableWholeSite[] // empty when none
}

export type PayableSettlementQuery = {
  p: number
  page_size: number
  provider_ids: number[]
  methods: PayableSettlementMethod[]
}
