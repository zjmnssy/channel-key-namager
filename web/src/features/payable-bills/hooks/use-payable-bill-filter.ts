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
import { getRouteApi } from '@tanstack/react-router'

import {
  beijingYesterday,
  monthStart,
} from '@/features/receivable-bills/lib/bill-date'
import { useIsChannelSupplier } from '@/hooks/use-admin'

import type { PayableBillFilter } from '../types'

const route = getRouteApi('/_authenticated/payable-bills/')

/**
 * The date, site and provider filters that the details and provider bills
 * tabs share in the URL; `PayableBillFilterBar` edits them. The latest bill
 * date is yesterday: a day is pulled after it ends. A channel supplier gets
 * only their own rows and filters by date alone, so the site and provider
 * filters in the URL do not apply to them.
 */
export function usePayableBillFilter(): Required<PayableBillFilter> {
  const search = route.useSearch()
  const supplier = useIsChannelSupplier()
  const yesterday = beijingYesterday()
  return {
    start_date: search.start ?? monthStart(yesterday),
    end_date: search.end ?? yesterday,
    site_ids: supplier ? [] : (search.sites ?? []).map(Number),
    provider_ids: supplier ? [] : (search.providers ?? []).map(Number),
  }
}
