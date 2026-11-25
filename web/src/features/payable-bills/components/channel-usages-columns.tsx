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
import { type ReactNode, useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { TableId } from '@/components/table-id'
import { TruncatedText } from '@/components/truncated-text'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { formatBillDate } from '@/features/receivable-bills/lib/bill-date'
import { formatTimestampToDate } from '@/lib/format'

import type { PayableUsage } from '../types'

const EMPTY = '—'

export function useChannelUsagesColumns(
  onSetProvider: (usage: PayableUsage) => void
): ColumnDef<PayableUsage>[] {
  const { t } = useTranslation()

  return useMemo<ColumnDef<PayableUsage>[]>(
    () => [
      {
        accessorKey: 'task_id',
        header: t('Task ID'),
        cell: ({ row }) => <TableId value={row.original.task_id} />,
      },
      {
        accessorKey: 'bill_date',
        header: t('Bill Date'),
        cell: ({ row }) => formatBillDate(row.original.bill_date),
      },
      {
        accessorKey: 'site_id',
        header: t('Site ID'),
        cell: ({ row }) => <TableId value={row.original.site_id} />,
      },
      {
        accessorKey: 'site_name',
        header: t('Site Name'),
        cell: ({ row }) => (
          <TruncatedText
            text={row.original.site_name}
            maxWidth='max-w-[160px]'
          />
        ),
      },
      {
        accessorKey: 'remote_channel_name',
        header: t('Channel Name'),
        meta: { mobileTitle: true },
        cell: ({ row }) => (
          <TruncatedText
            text={row.original.remote_channel_name}
            maxWidth='max-w-[220px]'
          />
        ),
      },
      {
        accessorKey: 'delta_amount',
        header: t('Daily Amount Before Discount ($)'),
        cell: ({ row }) => {
          const usage = row.original
          let flag: ReactNode = null
          if (usage.delta_amount === null) {
            // The site had no complete pull before, so there is no baseline.
            flag = (
              <StatusBadge
                label={t('First sync, no baseline')}
                variant='neutral'
                copyable={false}
              />
            )
          } else if (usage.delta_flag === 'rollback') {
            flag = (
              <Tooltip>
                <TooltipTrigger render={<span className='inline-flex' />}>
                  <StatusBadge
                    label={t('Cumulative value rolled back')}
                    variant='warning'
                    copyable={false}
                  />
                </TooltipTrigger>
                <TooltipContent className='max-w-xs'>
                  {t(
                    'The cumulative value is lower than in the last complete pull, so the daily amount is 0. Check it manually.'
                  )}
                </TooltipContent>
              </Tooltip>
            )
          } else if (usage.delta_flag === 'new_channel') {
            flag = (
              <StatusBadge
                label={t('New channel')}
                variant='info'
                copyable={false}
              />
            )
          }
          return (
            <div className='flex flex-wrap items-center gap-1.5'>
              <span className='tabular-nums'>
                {usage.delta_amount ?? EMPTY}
              </span>
              {flag}
              {!usage.counted && (
                <StatusBadge
                  label={t('Site pull incomplete, not counted in the bill')}
                  variant='danger'
                  copyable={false}
                />
              )}
            </div>
          )
        },
      },
      {
        accessorKey: 'used_amount',
        header: t('Cumulative Amount ($)'),
        cell: ({ row }) => (
          <span className='tabular-nums'>{row.original.used_amount}</span>
        ),
      },
      {
        accessorKey: 'provider_name',
        header: t('Provider Name'),
        cell: ({ row }) => {
          const usage = row.original
          if (usage.provider_id === 0) {
            return <span className='text-muted-foreground'>{t('Unknown')}</span>
          }
          return (
            <div className='flex items-center gap-1.5'>
              <TruncatedText
                text={usage.provider_name}
                maxWidth='max-w-[160px]'
              />
              {/* The row's provider came from a manual setting. */}
              {usage.provider_source === 'manual' && (
                <StatusBadge
                  label={t('Manual')}
                  variant='info'
                  copyable={false}
                />
              )}
              {/* Decided by a site-level mapping rule, not by the channel. */}
              {(usage.provider_source === 'rule_all' ||
                usage.provider_source === 'rule_unresolved') && (
                <StatusBadge
                  label={t('Rule')}
                  variant='neutral'
                  copyable={false}
                />
              )}
            </div>
          )
        },
      },
      {
        accessorKey: 'synced_at',
        header: t('Sync Time'),
        cell: ({ row }) => formatTimestampToDate(row.original.synced_at),
      },
      {
        id: 'actions',
        header: t('Actions'),
        cell: ({ row }) => (
          <Button
            variant='link'
            size='sm'
            className='h-auto p-0'
            onClick={() => onSetProvider(row.original)}
          >
            {t('Set Provider')}
          </Button>
        ),
      },
    ],
    [t, onSetProvider]
  )
}
