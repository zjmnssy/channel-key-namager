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
export type ApiResponse<T> = {
  success: boolean
  message?: string
  message_key?: string
  message_params?: Record<string, unknown>
  field?: string
  data?: T
}

export type Page<T> = {
  page: number
  page_size: number
  total: number
  items: T[]
}

export type ReceivableBillStatus = 'success' | 'retrying' | 'failed'

export type ReceivableBill = {
  id: number
  site_id: number
  bill_date: number // YYYYMMDD
  site_name: string
  site_base_url: string
  quota: number
  quota_per_unit: number
  receivable_discount: number
  status: ReceivableBillStatus
  attempts: number
  synced_at: number
  next_retry_at: number
  created_time: number
  updated_time: number
  amount: string | null // only for a successful bill
  receivable_amount: string | null // amount × receivable discount, only for a successful bill
  error: { key: string; params?: Record<string, unknown> } | null
  site_deleted: boolean
}

export type ReceivableBillSite = {
  id: number
  name: string
  deleted: boolean
}

export type DailyBillFilter = {
  start_date: number
  end_date: number
  site_ids?: number[]
  statuses?: ReceivableBillStatus[]
}

export type DailyBillQuery = DailyBillFilter & { p: number; page_size: number }

export type TrendView = 'day' | 'month'

export type TrendQuery = {
  view: TrendView
  date?: number // day view, YYYYMMDD
  start_date?: number // month view
  end_date?: number
  site_ids?: number[]
}

export type TrendSeries = {
  site_id: number
  site_name: string
  deleted: boolean
  amounts: (string | null)[]
  provisional: boolean[]
  /** A point whose data could not be fetched; it has a null amount and is drawn at 0. */
  missing: boolean[]
}

export type ReceivableTrend = {
  view: TrendView
  labels: string[]
  series: TrendSeries[]
  missing_count: number
}
