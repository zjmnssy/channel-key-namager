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
import { useCallback, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { createServerError } from '@/lib/server-error-message'

import { getChannelPushRuns } from '../api'
import { CHANNEL_PUSH_QUERY_KEYS } from '../constants'
import type { ChannelPushRun, RunDetailKind } from '../types'
import { RunDetailDialog } from './run-detail-dialogs'
import { useRunsColumns } from './runs-columns'
import { RunsFilters, type RunFilters } from './runs-filters'

const route = getRouteApi('/_authenticated/channel-push/')
// The shared toolbar reads column filters; this list filters on the server.
const NO_COLUMN_FILTERS: ColumnFiltersState = []

export function RunsTable() {
  const { t } = useTranslation()
  const search = route.useSearch()
  const navigate = route.useNavigate()
  const [detail, setDetail] = useState<{
    run: ChannelPushRun
    kind: RunDetailKind
  } | null>(null)
  const openDetail = useCallback(
    (run: ChannelPushRun, kind: RunDetailKind) => setDetail({ run, kind }),
    []
  )
  const columns = useRunsColumns(openDetail)

  const { pagination, onPaginationChange, ensurePageInRange } =
    useTableUrlState({
      search,
      navigate,
      pagination: {
        pageKey: 'runPage',
        pageSizeKey: 'runPageSize',
        defaultPage: 1,
        defaultPageSize: 10,
      },
      globalFilter: { enabled: false },
    })

  const filters: RunFilters = {
    startFrom: search.startFrom,
    startTo: search.startTo,
    supplierId: search.supplierId,
    siteId: search.siteId,
  }
  const params = {
    p: pagination.pageIndex + 1,
    page_size: pagination.pageSize,
    start_from: filters.startFrom,
    start_to: filters.startTo,
    supplier_user_id: filters.supplierId,
    site_id: filters.siteId,
  }

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [...CHANNEL_PUSH_QUERY_KEYS.runs, params],
    queryFn: async () => {
      const result = await getChannelPushRuns(params)
      if (!result.success) {
        throw createServerError(result, t('Failed to load push results'))
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

  // A filter change starts again from the first page.
  const updateFilters = (patch: RunFilters) =>
    void navigate({
      search: (prev) => ({ ...prev, ...patch, runPage: undefined }),
    })

  return (
    <>
      <DataTablePage
        table={table}
        columns={columns}
        isLoading={isLoading}
        isFetching={isFetching}
        emptyTitle={t('No Push Results Found')}
        emptyDescription={t(
          'Push tasks record a result here each time they run.'
        )}
        skeletonKeyPrefix='channel-push-runs-skeleton'
        toolbar={
          <RunsFilters
            table={table}
            filters={filters}
            onChange={updateFilters}
            onReset={() =>
              updateFilters({
                startFrom: undefined,
                startTo: undefined,
                supplierId: undefined,
                siteId: undefined,
              })
            }
          />
        }
      />
      {detail && (
        <RunDetailDialog
          run={detail.run}
          kind={detail.kind}
          open
          onOpenChange={(open) => {
            if (!open) setDetail(null)
          }}
        />
      )}
    </>
  )
}
