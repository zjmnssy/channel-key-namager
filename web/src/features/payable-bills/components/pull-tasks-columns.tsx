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
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { TableId } from '@/components/table-id'
import { Button } from '@/components/ui/button'
import { formatBillDate } from '@/features/receivable-bills/lib/bill-date'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber, formatTimestampToDate } from '@/lib/format'

import { PULL_TASK_STATUS_VARIANTS } from '../constants'
import type { PayablePullTask } from '../types'

const EMPTY = '—'

export function usePullTasksColumns(
  onViewSites: (task: PayablePullTask) => void
): ColumnDef<PayablePullTask>[] {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)

  return useMemo<ColumnDef<PayablePullTask>[]>(
    () => [
      {
        accessorKey: 'id',
        header: t('Task ID'),
        cell: ({ row }) => <TableId value={row.original.id} />,
      },
      {
        accessorKey: 'bill_date',
        header: t('Bill Date'),
        meta: { mobileTitle: true },
        cell: ({ row }) => formatBillDate(row.original.bill_date),
      },
      {
        accessorKey: 'created_time',
        header: t('Created At'),
        cell: ({ row }) => formatTimestampToDate(row.original.created_time),
      },
      {
        accessorKey: 'started_time',
        header: t('Started At'),
        cell: ({ row }) =>
          row.original.started_time > 0
            ? formatTimestampToDate(row.original.started_time)
            : EMPTY,
      },
      {
        accessorKey: 'channel_total',
        header: t('Channel Total'),
        cell: ({ row }) => formatNumber(row.original.channel_total, locale),
      },
      {
        accessorKey: 'status',
        header: t('Current Status'),
        cell: ({ row }) => {
          const task = row.original
          // An unknown status is shown as it is.
          let label: string = task.status
          if (task.status === 'success') {
            // Sites that succeeded with fewer channels than they reported.
            label =
              task.mismatch_sites > 0
                ? t('Done ({{count}} site(s) with mismatched totals)', {
                    count: task.mismatch_sites,
                  })
                : t('Done')
          } else if (task.status === 'running') {
            // Sites that ended, whether they succeeded or failed.
            const progress = {
              finished: formatNumber(
                task.finished_sites + task.failed_sites,
                locale
              ),
              total: formatNumber(task.site_total, locale),
            }
            label =
              task.retrying_sites > 0
                ? t(
                    'Pulling {{finished}}/{{total}} sites, {{count}} retrying',
                    {
                      ...progress,
                      count: task.retrying_sites,
                    }
                  )
                : t('Pulling {{finished}}/{{total}} sites', progress)
          } else if (task.status === 'partial_failed') {
            label = t('Partially failed {{finished}}/{{total}}', {
              finished: formatNumber(task.finished_sites, locale),
              total: formatNumber(task.site_total, locale),
            })
          }
          return (
            <StatusBadge
              label={label}
              variant={PULL_TASK_STATUS_VARIANTS[task.status] ?? 'neutral'}
              pulse={task.status === 'running'}
              copyable={false}
            />
          )
        },
      },
      {
        accessorKey: 'finished_time',
        header: t('Finished At'),
        cell: ({ row }) =>
          row.original.finished_time > 0
            ? formatTimestampToDate(row.original.finished_time)
            : EMPTY,
      },
      {
        // Totals are only final once the task ends.
        accessorKey: 'delta_amount',
        header: t('Delta Total ($)'),
        cell: ({ row }) => row.original.delta_amount ?? EMPTY,
      },
      {
        accessorKey: 'used_amount',
        header: t('Cumulative Total ($)'),
        cell: ({ row }) => row.original.used_amount ?? EMPTY,
      },
      {
        id: 'actions',
        header: () => <span className='sr-only'>{t('Actions')}</span>,
        cell: ({ row }) => (
          <Button
            variant='link'
            size='sm'
            className='h-auto p-0'
            onClick={(event) => {
              // The row opens the same drawer on click.
              event.stopPropagation()
              onViewSites(row.original)
            }}
          >
            {t('View Sites')}
          </Button>
        ),
      },
    ],
    [t, locale, onViewSites]
  )
}
