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
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { createServerError } from '@/lib/server-error-message'

import { listPayableSettlementMethods } from '../api'
import {
  PAYABLE_BILL_QUERY_KEYS,
  SETTLEMENT_METHOD_LABEL_KEYS,
  SETTLEMENT_METHODS,
  UNKNOWN_PROVIDER_VALUE,
} from '../constants'
import { usePayableBillOptions } from '../hooks/use-payable-bill-options'
import type { PayableSettlement, PayableSettlementQuery } from '../types'
import { SettlementMethodDialog } from './settlement-method-dialog'
import { useSettlementMethodsColumns } from './settlement-methods-columns'

const route = getRouteApi('/_authenticated/payable-bills/')
// The shared toolbar reads column filters; this list filters on the server.
const NO_COLUMN_FILTERS: ColumnFiltersState = []

const METHOD_OPTIONS = SETTLEMENT_METHODS.map((method) => ({
  value: method,
  label: SETTLEMENT_METHOD_LABEL_KEYS[method],
}))

export function SettlementMethodsTable() {
  const { t } = useTranslation()
  const search = route.useSearch()
  const navigate = route.useNavigate()
  const options = usePayableBillOptions()
  const [editing, setEditing] = useState<PayableSettlement | null>(null)
  const columns = useSettlementMethodsColumns(setEditing)
  // Only channel suppliers are listed, so the unknown provider is no choice.
  const providerOptions = options.providerOptions.filter(
    (option) => option.value !== UNKNOWN_PROVIDER_VALUE
  )
  // Kept apart from the provider filter of the details and provider bills tabs.
  const providerValues = search.settlementProviders ?? []
  const methodValues = search.methods ?? []

  const { pagination, onPaginationChange, ensurePageInRange } =
    useTableUrlState({
      search,
      navigate,
      pagination: { defaultPage: 1, defaultPageSize: 10 },
      globalFilter: { enabled: false },
    })

  const query: PayableSettlementQuery = {
    p: pagination.pageIndex + 1,
    page_size: pagination.pageSize,
    provider_ids: providerValues.map(Number),
    methods: SETTLEMENT_METHODS.filter((method) =>
      methodValues.includes(method)
    ),
  }

  const { data, isLoading, isFetching } = useQuery({
    queryKey: PAYABLE_BILL_QUERY_KEYS.settlements(query),
    queryFn: async () => {
      const result = await listPayableSettlementMethods(query)
      if (!result.success) {
        throw createServerError(result, t('Failed to load settlement methods'))
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
    getRowId: (row) => String(row.provider_id),
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

  return (
    <>
      <DataTablePage
        table={table}
        columns={columns}
        isLoading={isLoading}
        isFetching={isFetching}
        emptyTitle={t('No providers available')}
        skeletonKeyPrefix='payable-settlements-skeleton'
        toolbar={
          <DataTableToolbar
            table={table}
            hideViewOptions
            hasAdditionalFilters={
              providerValues.length > 0 || methodValues.length > 0
            }
            onReset={() =>
              updateFilters({
                settlementProviders: undefined,
                methods: undefined,
              })
            }
            customSearch={
              <>
                <DataTableFacetedFilter
                  title={t('Provider')}
                  options={providerOptions}
                  value={providerValues}
                  onValueChange={(settlementProviders) =>
                    updateFilters({ settlementProviders })
                  }
                />
                <DataTableFacetedFilter
                  title={t('Settlement Method')}
                  options={METHOD_OPTIONS}
                  value={methodValues}
                  onValueChange={(methods) => updateFilters({ methods })}
                />
              </>
            }
          />
        }
      />
      {editing && (
        <SettlementMethodDialog
          key={editing.provider_id}
          settlement={editing}
          open
          onOpenChange={(open) => {
            if (!open) setEditing(null)
          }}
        />
      )}
    </>
  )
}
