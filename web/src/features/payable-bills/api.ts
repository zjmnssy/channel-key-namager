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
import i18n from 'i18next'

import { api } from '@/lib/api'
import { fetchExportFile, joinQueryList } from '@/lib/export-file'

import type {
  ApiResponse,
  Page,
  PayableBillFilter,
  PayableBillOptions,
  PayableBillQuery,
  PayableProviderBillPage,
  PayablePullSite,
  PayablePullTask,
  PayableUsage,
} from './types'

const BASE = '/api/payable_bill'

// The list and export endpoints share one filter.
function filterParams(filter: PayableBillFilter) {
  return {
    start_date: filter.start_date,
    end_date: filter.end_date,
    site_ids: joinQueryList(filter.site_ids),
    provider_ids: joinQueryList(filter.provider_ids),
  }
}

export async function listPayablePullTasks(
  p: number,
  page_size: number
): Promise<ApiResponse<Page<PayablePullTask>>> {
  const res = await api.get(`${BASE}/tasks`, { params: { p, page_size } })
  return res.data
}

export async function listPayablePullSites(
  taskId: number
): Promise<ApiResponse<PayablePullSite[]>> {
  const res = await api.get(`${BASE}/tasks/${taskId}/sites`)
  return res.data
}

export async function listPayableUsages(
  query: PayableBillQuery
): Promise<ApiResponse<Page<PayableUsage>>> {
  const res = await api.get(`${BASE}/usages`, {
    params: { p: query.p, page_size: query.page_size, ...filterParams(query) },
  })
  return res.data
}

export function exportPayableUsages(filter: PayableBillFilter): Promise<Blob> {
  return fetchExportFile(
    `${BASE}/usages/export`,
    filterParams(filter),
    i18n.t('Failed to export bills')
  )
}

/** A provider id of 0 clears the manual setting of the channel. */
export async function setPayableUsageProvider(
  id: number,
  providerId: number
): Promise<ApiResponse<PayableUsage>> {
  const res = await api.put(`${BASE}/usages/${id}/provider`, {
    provider_id: providerId,
  })
  return res.data
}

export async function listPayableProviderBills(
  query: PayableBillQuery
): Promise<ApiResponse<PayableProviderBillPage>> {
  const res = await api.get(`${BASE}/providers`, {
    params: { p: query.p, page_size: query.page_size, ...filterParams(query) },
  })
  return res.data
}

export function exportPayableProviderBills(
  filter: PayableBillFilter
): Promise<Blob> {
  return fetchExportFile(
    `${BASE}/providers/export`,
    filterParams(filter),
    i18n.t('Failed to export bills')
  )
}

export async function getPayableBillOptions(): Promise<
  ApiResponse<PayableBillOptions>
> {
  const res = await api.get(`${BASE}/options`)
  return res.data
}
