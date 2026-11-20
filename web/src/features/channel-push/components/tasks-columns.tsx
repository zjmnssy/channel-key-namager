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
import type { ColumnDef } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { TableId } from '@/components/table-id'
import { TruncatedText } from '@/components/truncated-text'
import { formatTimestampToDate } from '@/lib/format'

import type { ChannelPushTask } from '../types'
import { TaskRowActions } from './task-row-actions'

export function useTasksColumns(): ColumnDef<ChannelPushTask>[] {
  const { t } = useTranslation()
  return [
    {
      accessorKey: 'id',
      header: t('Task ID'),
      cell: ({ row }) => <TableId value={row.original.id} />,
    },
    {
      id: 'supplier_ids',
      header: t('Supplier User ID'),
      cell: ({ row }) => (
        <TruncatedText
          text={row.original.suppliers
            .map((supplier) => supplier.id)
            .join(', ')}
          maxWidth='max-w-[140px]'
        />
      ),
    },
    {
      id: 'supplier_names',
      header: t('Supplier Username'),
      meta: { mobileTitle: true },
      cell: ({ row }) => (
        <TruncatedText
          text={row.original.suppliers
            .map((supplier) => supplier.username)
            .join(', ')}
          maxWidth='max-w-[200px]'
        />
      ),
    },
    {
      accessorKey: 'site_id',
      header: t('Third-Party Site ID'),
      cell: ({ row }) => <TableId value={row.original.site_id} />,
    },
    {
      accessorKey: 'site_name',
      header: t('Third-Party Site Name'),
      cell: ({ row }) => (
        <span className='flex items-center gap-1.5'>
          <TruncatedText
            text={row.original.site_name}
            maxWidth='max-w-[200px]'
          />
          {row.original.site_deleted && (
            <StatusBadge
              label={t('Deleted')}
              variant='danger'
              copyable={false}
            />
          )}
        </span>
      ),
    },
    {
      accessorKey: 'enabled',
      header: t('Enabled'),
      cell: ({ row }) => (
        <StatusBadge
          label={row.original.enabled ? t('Enabled') : t('Disabled')}
          variant={row.original.enabled ? 'success' : 'neutral'}
          copyable={false}
        />
      ),
    },
    {
      accessorKey: 'task_type',
      header: t('Task Type'),
      cell: ({ row }) =>
        row.original.task_type === 'periodic'
          ? t('Periodic · every {{n}} minutes', {
              n: row.original.interval_minutes,
            })
          : t('Once'),
    },
    {
      accessorKey: 'last_finished_at',
      header: t('Last Push Time'),
      cell: ({ row }) =>
        row.original.last_finished_at > 0
          ? formatTimestampToDate(row.original.last_finished_at)
          : '—',
    },
    {
      accessorKey: 'created_time',
      header: t('Created Time'),
      cell: ({ row }) => formatTimestampToDate(row.original.created_time),
    },
    {
      id: 'actions',
      header: t('Actions'),
      cell: ({ row }) => <TaskRowActions row={row} />,
    },
  ]
}
