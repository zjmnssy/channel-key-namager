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
import { useTranslation } from 'react-i18next'

import {
  DataTableFacetedFilter,
  DataTablePage,
  DataTableToolbar,
  useDataTable,
} from '@/components/data-table'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import {
  createServerError,
  requireServerSuccess,
} from '@/lib/server-error-message'

import { listReceivableBills, listReceivableBillSites } from '../api'
import { BILL_STATUS_OPTIONS, RECEIVABLE_BILL_QUERY_KEYS } from '../constants'
import { beijingToday, monthStart } from '../lib/bill-date'
import type { DailyBillQuery } from '../types'
import { BillDateRangePicker } from './bill-date-range-picker'
import { useDailyBillsColumns } from './daily-bills-columns'

const route = getRouteApi('/_authenticated/receivable-bills/')
// The shared toolbar reads column filters; this list filters on the server.
const NO_COLUMN_FILTERS: ColumnFiltersState = []

export function DailyBillsTable() {
  const { t } = useTranslation()
  const search = route.useSearch()
  const navigate = route.useNavigate()
  const columns = useDailyBillsColumns()

  const { pagination, onPaginationChange, ensurePageInRange } =
    useTableUrlState({
      search,
      navigate,
      pagination: { defaultPage: 1, defaultPageSize: 10 },
      globalFilter: { enabled: false },
    })

  const today = beijingToday()
  const startDate = search.start ?? monthStart(today)
  const endDate = search.end ?? today
  const siteValues = search.sites ?? []
  const statusValues = search.statuses ?? []

  const query: DailyBillQuery = {
    p: pagination.pageIndex + 1,
    page_size: pagination.pageSize,
    start_date: startDate,
    end_date: endDate,
    site_ids: siteValues.map(Number),
    statuses: statusValues,
  }

  const { data, isLoading, isFetching } = useQuery({
    queryKey: RECEIVABLE_BILL_QUERY_KEYS.daily(query),
    queryFn: async () => {
      const result = await listReceivableBills(query)
      if (!result.success) {
        throw createServerError(result, t('Failed to load bills'))
      }
      return {
        items: result.data?.items ?? [],
        total: result.data?.total ?? 0,
      }
    },
    placeholderData: (previousData) => previousData,
  })

  const sitesQuery = useQuery({
    queryKey: RECEIVABLE_BILL_QUERY_KEYS.sites,
    queryFn: async () =>
      requireServerSuccess(await listReceivableBillSites()).data ?? [],
  })
  const siteOptions = (sitesQuery.data ?? []).map((site) => ({
    value: String(site.id),
    label: site.deleted
      ? t('{{name}} (deleted)', { name: site.name })
      : site.name,
  }))
  const statusOptions = BILL_STATUS_OPTIONS.map((option) => ({
    value: option.value,
    label: t(option.labelKey),
  }))

  const { table } = useDataTable({
    data: data?.items ?? [],
    columns,
    getRowId: (row) => String(row.id),
    pagination,
    onPaginationChange,
    manualPagination: true,
    manualFiltering: true,
    columnFilters: NO_COLUMN_FILTERS,
    totalCount: data?.total ?? 0,
    ensurePageInRange,
  })

  // A filter change starts again from the first page.
  const updateFilters = (patch: Record<string, unknown>) =>
    void navigate({
      search: (prev) => ({ ...prev, ...patch, page: undefined }),
    })

  const hasFilters =
    search.start !== undefined ||
    search.end !== undefined ||
    siteValues.length > 0 ||
    statusValues.length > 0

  return (
    <DataTablePage
      table={table}
      columns={columns}
      isLoading={isLoading}
      isFetching={isFetching}
      emptyTitle={t('No bills yet')}
      emptyDescription={t(
        'Bills are generated at 1:00 Beijing time every day.'
      )}
      skeletonKeyPrefix='receivable-bills-daily-skeleton'
      toolbar={
        <DataTableToolbar
          table={table}
          hideViewOptions
          hasAdditionalFilters={hasFilters}
          onReset={() =>
            updateFilters({
              start: undefined,
              end: undefined,
              sites: undefined,
              statuses: undefined,
            })
          }
          customSearch={
            <>
              <BillDateRangePicker
                start={startDate}
                end={endDate}
                max={today}
                onChange={(start, end) => updateFilters({ start, end })}
              />
              <DataTableFacetedFilter
                title={t('Site Name')}
                options={siteOptions}
                value={siteValues}
                onValueChange={(sites) => updateFilters({ sites })}
              />
              <DataTableFacetedFilter
                title={t('Sync Result')}
                options={statusOptions}
                value={statusValues}
                onValueChange={(statuses) => updateFilters({ statuses })}
              />
            </>
          }
        />
      }
    />
  )
}
