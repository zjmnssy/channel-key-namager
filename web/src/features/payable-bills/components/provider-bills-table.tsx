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
import { useQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import type { ColumnFiltersState } from '@tanstack/react-table'
import { TriangleAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { createServerError } from '@/lib/server-error-message'

import { listPayableProviderBills } from '../api'
import { PAYABLE_BILL_QUERY_KEYS } from '../constants'
import { usePayableBillFilter } from '../hooks/use-payable-bill-filter'
import type { PayableBillQuery } from '../types'
import { ExportPayableButton } from './export-payable-button'
import { PayableBillFilterBar } from './payable-bill-filter-bar'
import { useProviderBillsColumns } from './provider-bills-columns'

const route = getRouteApi('/_authenticated/payable-bills/')
// The shared toolbar reads column filters; this list filters on the server.
const NO_COLUMN_FILTERS: ColumnFiltersState = []

export function ProviderBillsTable() {
  const { t } = useTranslation()
  const search = route.useSearch()
  const navigate = route.useNavigate()
  const columns = useProviderBillsColumns()
  // The details tab reads the same date, site and provider filters.
  const filter = usePayableBillFilter()

  const { pagination, onPaginationChange, ensurePageInRange } =
    useTableUrlState({
      search,
      navigate,
      pagination: { defaultPage: 1, defaultPageSize: 10 },
      globalFilter: { enabled: false },
    })

  const query: PayableBillQuery = {
    p: pagination.pageIndex + 1,
    page_size: pagination.pageSize,
    ...filter,
  }

  const { data, isLoading, isFetching } = useQuery({
    queryKey: PAYABLE_BILL_QUERY_KEYS.providers(query),
    queryFn: async () => {
      const result = await listPayableProviderBills(query)
      if (!result.success) {
        throw createServerError(result, t('Failed to load provider bills'))
      }
      return {
        items: result.data?.items ?? [],
        total: result.data?.total ?? 0,
        incompleteSiteCount: result.data?.incomplete_site_count ?? 0,
        mismatchSiteCount: result.data?.mismatch_site_count ?? 0,
        unsyncedUsageSiteCount: result.data?.unsynced_usage_site_count ?? 0,
      }
    },
    placeholderData: (previousData) => previousData,
  })

  const { table } = useDataTable({
    data: data?.items ?? [],
    columns,
    // A bill is one provider on one day under one settlement method; a
    // provider-day settled both ways has two bills.
    getRowId: (row) =>
      `${row.bill_date}-${row.provider_id}-${row.settlement_method}`,
    pagination,
    onPaginationChange,
    manualPagination: true,
    manualFiltering: true,
    columnFilters: NO_COLUMN_FILTERS,
    totalCount: data?.total ?? 0,
    ensurePageInRange,
  })

  // Only complete site pulls are billed, so a failed one makes the bill
  // look smaller than it is.
  const incompleteSiteCount = data?.incompleteSiteCount ?? 0
  // A pull that succeeded with fewer channels than the site reported may
  // have missed some, so the bill may look smaller too.
  const mismatchSiteCount = data?.mismatchSiteCount ?? 0
  // Whole sites whose daily bills have not synced successfully are settled
  // by channel details until they do.
  const unsyncedUsageSiteCount = data?.unsyncedUsageSiteCount ?? 0

  return (
    <div className='flex h-full min-h-0 flex-col gap-3'>
      {incompleteSiteCount > 0 && (
        <Alert className='shrink-0'>
          <TriangleAlert aria-hidden='true' />
          <AlertDescription>
            {t(
              'In the selected dates, {{count}} site syncs did not succeed, so the bill may be incomplete',
              { count: incompleteSiteCount }
            )}
          </AlertDescription>
        </Alert>
      )}
      {mismatchSiteCount > 0 && (
        <Alert className='shrink-0'>
          <TriangleAlert aria-hidden='true' />
          <AlertDescription>
            {t(
              '{{count}} site(s) reported more channels than were pulled in the selected dates; the bills may be incomplete',
              { count: mismatchSiteCount }
            )}
          </AlertDescription>
        </Alert>
      )}
      {unsyncedUsageSiteCount > 0 && (
        <Alert className='shrink-0'>
          <TriangleAlert aria-hidden='true' />
          <AlertDescription>
            {t(
              'In the selected dates, {{count}} sites have daily bills that have not synced successfully; they are settled by channel details for now',
              { count: unsyncedUsageSiteCount }
            )}
          </AlertDescription>
        </Alert>
      )}
      <div className='min-h-0 flex-1'>
        <DataTablePage
          table={table}
          columns={columns}
          isLoading={isLoading}
          isFetching={isFetching}
          emptyTitle={t('No provider bills')}
          skeletonKeyPrefix='payable-provider-bills-skeleton'
          toolbar={
            <PayableBillFilterBar
              table={table}
              actions={<ExportPayableButton kind='providers' filter={filter} />}
            />
          }
        />
      </div>
    </div>
  )
}
