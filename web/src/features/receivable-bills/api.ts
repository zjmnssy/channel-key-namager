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

import { toIntlLocale } from '@/i18n/languages'
import { api } from '@/lib/api'
import { createServerError } from '@/lib/server-error-message'

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

// Array parameters travel as comma-separated strings and are left out when empty.
function joinList(values?: (string | number)[]): string | undefined {
  return values && values.length > 0 ? values.join(',') : undefined
}

export async function listReceivableBills(
  query: DailyBillQuery
): Promise<ApiResponse<Page<ReceivableBill>>> {
  const res = await api.get('/api/receivable_bill/daily', {
    params: {
      p: query.p,
      page_size: query.page_size,
      start_date: query.start_date,
      end_date: query.end_date,
      site_ids: joinList(query.site_ids),
      statuses: joinList(query.statuses),
    },
  })
  return res.data
}

// Returns the xlsx file; parameter errors come back as a JSON body instead.
export async function exportReceivableBills(
  filter: DailyBillFilter
): Promise<Blob> {
  const res = await api.get<Blob>('/api/receivable_bill/daily/export', {
    params: {
      start_date: filter.start_date,
      end_date: filter.end_date,
      site_ids: joinList(filter.site_ids),
      statuses: joinList(filter.statuses),
    },
    headers: {
      // Without a saved language preference the file follows the interface.
      'Accept-Language': toIntlLocale(i18n.resolvedLanguage || i18n.language),
    },
    responseType: 'blob',
    disableDuplicate: true,
    // Only silences the session-expired toast; the caller reports failures.
    skipErrorHandler: true,
  })
  if (res.data.type.includes('json')) {
    let body: unknown
    try {
      body = JSON.parse(await res.data.text())
    } catch {
      throw new Error(i18n.t('Failed to export bills'))
    }
    throw createServerError(body)
  }
  return res.data
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
      site_ids: joinList(query.site_ids),
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
