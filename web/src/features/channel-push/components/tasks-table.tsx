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
import { useTranslation } from 'react-i18next'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { createServerError } from '@/lib/server-error-message'

import { getChannelPushTasks } from '../api'
import { CHANNEL_PUSH_QUERY_KEYS } from '../constants'
import { useTasksColumns } from './tasks-columns'

const route = getRouteApi('/_authenticated/channel-push/')

export function TasksTable() {
  const { t } = useTranslation()
  const columns = useTasksColumns()

  const { pagination, onPaginationChange, ensurePageInRange } =
    useTableUrlState({
      search: route.useSearch(),
      navigate: route.useNavigate(),
      pagination: {
        pageKey: 'taskPage',
        pageSizeKey: 'taskPageSize',
        defaultPage: 1,
        defaultPageSize: 10,
      },
      globalFilter: { enabled: false },
    })

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      ...CHANNEL_PUSH_QUERY_KEYS.tasks,
      pagination.pageIndex + 1,
      pagination.pageSize,
    ],
    queryFn: async () => {
      const result = await getChannelPushTasks({
        p: pagination.pageIndex + 1,
        page_size: pagination.pageSize,
      })
      if (!result.success) {
        throw createServerError(result, t('Failed to load push tasks'))
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
    totalCount: data?.total ?? 0,
    ensurePageInRange,
  })

  return (
    <DataTablePage
      table={table}
      columns={columns}
      isLoading={isLoading}
      isFetching={isFetching}
      emptyTitle={t('No Push Tasks Found')}
      emptyDescription={t(
        'No push tasks yet. Create your first push task to get started.'
      )}
      skeletonKeyPrefix='channel-push-tasks-skeleton'
      toolbarProps={null}
    />
  )
}
