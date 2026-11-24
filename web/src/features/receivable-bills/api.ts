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
  DailyBillFilter,
  DailyBillQuery,
  Page,
  ReceivableBill,
  ReceivableBillSite,
  ReceivableTrend,
  TrendQuery,
} from './types'

export async function listReceivableBills(
  query: DailyBillQuery
): Promise<ApiResponse<Page<ReceivableBill>>> {
  const res = await api.get('/api/receivable_bill/daily', {
    params: {
      p: query.p,
      page_size: query.page_size,
      start_date: query.start_date,
      end_date: query.end_date,
      site_ids: joinQueryList(query.site_ids),
      statuses: joinQueryList(query.statuses),
    },
  })
  return res.data
}

export function exportReceivableBills(filter: DailyBillFilter): Promise<Blob> {
  return fetchExportFile(
    '/api/receivable_bill/daily/export',
    {
      start_date: filter.start_date,
      end_date: filter.end_date,
      site_ids: joinQueryList(filter.site_ids),
      statuses: joinQueryList(filter.statuses),
    },
    i18n.t('Failed to export bills')
  )
}

export async function resyncReceivableBill(
  id: number
): Promise<ApiResponse<ReceivableBill>> {
  const res = await api.post(`/api/receivable_bill/daily/${id}/sync`)
  return res.data
}

export async function getReceivableBillTrend(
  query: TrendQuery
): Promise<ApiResponse<ReceivableTrend>> {
  const res = await api.get('/api/receivable_bill/trend', {
    params: {
      view: query.view,
      date: query.date,
      start_date: query.start_date,
      end_date: query.end_date,
      site_ids: joinQueryList(query.site_ids),
    },
  })
  return res.data
}

export async function listReceivableBillSites(): Promise<
  ApiResponse<ReceivableBillSite[]>
> {
  const res = await api.get('/api/receivable_bill/sites')
  return res.data
}
