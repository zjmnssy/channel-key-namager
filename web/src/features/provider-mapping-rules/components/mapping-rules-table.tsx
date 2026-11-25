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

import { DataTablePage, useDataTable } from '@/components/data-table'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { createServerError } from '@/lib/server-error-message'

import { listProviderMappingRules } from '../api'
import { PROVIDER_MAPPING_QUERY_KEYS, RULE_TYPES } from '../constants'
import type { ProviderMappingListQuery } from '../types'
import { useMappingRulesColumns } from './mapping-rules-columns'
import { MappingRulesFilterBar } from './mapping-rules-filter-bar'

const route = getRouteApi('/_authenticated/provider-mapping-rules/')
// The shared toolbar reads column filters; this list filters on the server.
const NO_COLUMN_FILTERS: ColumnFiltersState = []

export function MappingRulesTable() {
  const { t } = useTranslation()
  const search = route.useSearch()
  const columns = useMappingRulesColumns()

  const { pagination, onPaginationChange, ensurePageInRange } =
    useTableUrlState({
      search,
      navigate: route.useNavigate(),
      pagination: { defaultPage: 1, defaultPageSize: 10 },
      globalFilter: { enabled: false },
    })

  const query: ProviderMappingListQuery = {
    p: pagination.pageIndex + 1,
    page_size: pagination.pageSize,
    site_ids: (search.sites ?? []).map(Number),
    types: RULE_TYPES.filter((type) => (search.types ?? []).includes(type)),
    provider_ids: (search.providers ?? []).map(Number),
  }

  const { data, isLoading, isFetching } = useQuery({
    queryKey: PROVIDER_MAPPING_QUERY_KEYS.list(query),
    queryFn: async () => {
      const result = await listProviderMappingRules(query)
      if (!result.success) {
        throw createServerError(result, t('Failed to load mapping rules'))
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
    <DataTablePage
      table={table}
      columns={columns}
      isLoading={isLoading}
      isFetching={isFetching}
      emptyTitle={t('No mapping rules')}
      emptyDescription={t('Add a rule to assign site channels to a provider.')}
      skeletonKeyPrefix='provider-mapping-rules-skeleton'
      toolbar={<MappingRulesFilterBar table={table} />}
    />
  )
}
