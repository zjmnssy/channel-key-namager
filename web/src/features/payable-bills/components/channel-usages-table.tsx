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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  DataTableFacetedFilter,
  DataTablePage,
  DataTableToolbar,
  useDataTable,
} from '@/components/data-table'
import { BillDateRangePicker } from '@/features/receivable-bills/components/bill-date-range-picker'
import {
  beijingYesterday,
  monthStart,
} from '@/features/receivable-bills/lib/bill-date'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { createServerError } from '@/lib/server-error-message'

import { listPayableUsages } from '../api'
import { PAYABLE_BILL_QUERY_KEYS } from '../constants'
import { usePayableBillOptions } from '../hooks/use-payable-bill-options'
import type { PayableBillQuery, PayableUsage } from '../types'
import { useChannelUsagesColumns } from './channel-usages-columns'
import { ExportPayableButton } from './export-payable-button'
import { SetProviderDialog } from './set-provider-dialog'

const route = getRouteApi('/_authenticated/payable-bills/')
// The shared toolbar reads column filters; this list filters on the server.
const NO_COLUMN_FILTERS: ColumnFiltersState = []

export function ChannelUsagesTable() {
  const { t } = useTranslation()
  const search = route.useSearch()
  const navigate = route.useNavigate()
  const [editingUsage, setEditingUsage] = useState<PayableUsage | null>(null)
  const columns = useChannelUsagesColumns(setEditingUsage)
  const options = usePayableBillOptions()

  const { pagination, onPaginationChange, ensurePageInRange } =
    useTableUrlState({
      search,
      navigate,
      pagination: { defaultPage: 1, defaultPageSize: 10 },
      globalFilter: { enabled: false },
    })

  // The latest bill date is yesterday: a day is pulled after it ends.
  const yesterday = beijingYesterday()
  const startDate = search.start ?? monthStart(yesterday)
  const endDate = search.end ?? yesterday
  const siteValues = search.sites ?? []
  const providerValues = search.providers ?? []

  const query: PayableBillQuery = {
    p: pagination.pageIndex + 1,
    page_size: pagination.pageSize,
    start_date: startDate,
    end_date: endDate,
    site_ids: siteValues.map(Number),
    provider_ids: providerValues.map(Number),
  }

  const { data, isLoading, isFetching } = useQuery({
    queryKey: PAYABLE_BILL_QUERY_KEYS.usages(query),
    queryFn: async () => {
      const result = await listPayableUsages(query)
      if (!result.success) {
        throw createServerError(
          result,
          t('Failed to load channel usage details')
        )
      }
      return {
        items: result.data?.items ?? [],
        total: result.data?.total ?? 0,
      }
    },
    placeholderData: (previousData) => previousData,
  })

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

  // A filter change starts again from the first page. The provider bills tab
  // reads the same date, site and provider filters.
  const updateFilters = (patch: Record<string, unknown>) =>
    void navigate({
      search: (prev) => ({ ...prev, ...patch, page: undefined }),
    })

  const hasFilters =
    search.start !== undefined ||
    search.end !== undefined ||
    siteValues.length > 0 ||
    providerValues.length > 0

  return (
    <>
      <DataTablePage
        table={table}
        columns={columns}
        isLoading={isLoading}
        isFetching={isFetching}
        emptyTitle={t('No channel usage details')}
        skeletonKeyPrefix='payable-usages-skeleton'
        toolbar={
          <DataTableToolbar
            table={table}
            hideViewOptions
            hasAdditionalFilters={hasFilters}
            preActions={
              <ExportPayableButton
                kind='usages'
                filter={{
                  start_date: startDate,
                  end_date: endDate,
                  site_ids: siteValues.length > 0 ? query.site_ids : undefined,
                  provider_ids:
                    providerValues.length > 0 ? query.provider_ids : undefined,
                }}
              />
            }
            onReset={() =>
              updateFilters({
                start: undefined,
                end: undefined,
                sites: undefined,
                providers: undefined,
              })
            }
            customSearch={
              <>
                <BillDateRangePicker
                  start={startDate}
                  end={endDate}
                  max={yesterday}
                  onChange={(start, end) => updateFilters({ start, end })}
                />
                <DataTableFacetedFilter
                  title={t('Site Name')}
                  options={options.siteOptions}
                  value={siteValues}
                  onValueChange={(sites) => updateFilters({ sites })}
                />
                <DataTableFacetedFilter
                  title={t('Provider')}
                  options={options.providerOptions}
                  value={providerValues}
                  onValueChange={(providers) => updateFilters({ providers })}
                />
              </>
            }
          />
        }
      />
      {editingUsage && (
        <SetProviderDialog
          key={editingUsage.id}
          usage={editingUsage}
          open
          onOpenChange={(open) => {
            if (!open) setEditingUsage(null)
          }}
        />
      )}
    </>
  )
}
