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

import { DataTablePage, useDataTable } from '@/components/data-table'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { createServerError } from '@/lib/server-error-message'

import { listPayableUsages } from '../api'
import { PAYABLE_BILL_QUERY_KEYS } from '../constants'
import { usePayableBillFilter } from '../hooks/use-payable-bill-filter'
import type { PayableBillQuery, PayableUsage } from '../types'
import { useChannelUsagesColumns } from './channel-usages-columns'
import { ExportPayableButton } from './export-payable-button'
import { PayableBillFilterBar } from './payable-bill-filter-bar'
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
  // The provider bills tab reads the same date, site and provider filters.
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
          <PayableBillFilterBar
            table={table}
            actions={<ExportPayableButton kind='usages' filter={filter} />}
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
