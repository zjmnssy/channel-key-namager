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
  DataTablePage,
  DataTableRow,
  useDataTable,
} from '@/components/data-table'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { createServerError } from '@/lib/server-error-message'

import { listPayablePullTasks } from '../api'
import { PAYABLE_BILL_QUERY_KEYS } from '../constants'
import type { PayablePullTask } from '../types'
import { PullTaskSitesDrawer } from './pull-task-sites-drawer'
import { usePullTasksColumns } from './pull-tasks-columns'

const route = getRouteApi('/_authenticated/payable-bills/')
// The task list has no filters; it only pages on the server.
const NO_COLUMN_FILTERS: ColumnFiltersState = []

export function PullTasksTable() {
  const { t } = useTranslation()
  const search = route.useSearch()
  const navigate = route.useNavigate()
  const [selectedTask, setSelectedTask] = useState<PayablePullTask | null>(null)
  const columns = usePullTasksColumns(setSelectedTask)

  const { pagination, onPaginationChange, ensurePageInRange } =
    useTableUrlState({
      search,
      navigate,
      pagination: { defaultPage: 1, defaultPageSize: 10 },
      globalFilter: { enabled: false },
    })
  const page = pagination.pageIndex + 1
  const pageSize = pagination.pageSize

  const { data, isLoading, isFetching } = useQuery({
    queryKey: PAYABLE_BILL_QUERY_KEYS.tasks(page, pageSize),
    queryFn: async () => {
      const result = await listPayablePullTasks(page, pageSize)
      if (!result.success) {
        throw createServerError(result, t('Failed to load pull tasks'))
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
        emptyTitle={t('No pull tasks yet')}
        emptyDescription={t(
          'A pull task is created at 00:10 Beijing time every day.'
        )}
        skeletonKeyPrefix='payable-pull-tasks-skeleton'
        toolbarProps={null}
        renderRow={(row) => (
          <DataTableRow
            key={row.id}
            row={row}
            className='cursor-pointer'
            onClick={() => setSelectedTask(row.original)}
            cellRenderColumns={table.options.columns}
          />
        )}
      />
      <PullTaskSitesDrawer
        task={selectedTask}
        onOpenChange={(open) => {
          if (!open) setSelectedTask(null)
        }}
      />
    </>
  )
}
