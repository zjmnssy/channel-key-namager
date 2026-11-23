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

import { DiscountValue } from '@/components/discount-value'
import { StatusBadge } from '@/components/status-badge'
import { TruncatedText } from '@/components/truncated-text'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { formatTimestampToDate } from '@/lib/format'

import { BILL_STATUS_DISPLAY } from '../constants'
import { formatBillDate } from '../lib/bill-date'
import type { ReceivableBill } from '../types'
import { ResyncBillButton } from './resync-bill-button'

const EMPTY = '—'

export function useDailyBillsColumns(): ColumnDef<ReceivableBill>[] {
  const { t } = useTranslation()

  return useMemo<ColumnDef<ReceivableBill>[]>(
    () => [
      {
        accessorKey: 'bill_date',
        header: t('Bill Date'),
        cell: ({ row }) => formatBillDate(row.original.bill_date),
      },
      {
        accessorKey: 'site_name',
        header: t('Site Name'),
        meta: { mobileTitle: true },
        cell: ({ row }) => (
          <TruncatedText
            text={
              row.original.site_deleted
                ? t('{{name}} (deleted)', { name: row.original.site_name })
                : row.original.site_name
            }
            maxWidth='max-w-[200px]'
          />
        ),
      },
      {
        accessorKey: 'site_base_url',
        header: t('Site URL'),
        cell: ({ row }) => (
          <TruncatedText
            text={row.original.site_base_url}
            maxWidth='max-w-[240px]'
          />
        ),
      },
      {
        accessorKey: 'amount',
        header: t('Amount Before Discount ($)'),
        cell: ({ row }) =>
          row.original.status === 'success' && row.original.amount !== null
            ? row.original.amount
            : EMPTY,
      },
      {
        accessorKey: 'receivable_discount',
        header: t('Receivable Discount'),
        cell: ({ row }) =>
          row.original.status === 'success' ? (
            <DiscountValue value={row.original.receivable_discount} />
          ) : (
            EMPTY
          ),
      },
      {
        accessorKey: 'status',
        header: t('Sync Result'),
        cell: ({ row }) => {
          const bill = row.original
          const display = BILL_STATUS_DISPLAY[bill.status]
          const label =
            bill.status === 'retrying' && bill.attempts >= 1
              ? t('Retrying (attempt {{attempts}})', {
                  attempts: bill.attempts,
                })
              : t(display.labelKey)
          const badge = (
            <StatusBadge
              label={label}
              variant={display.variant}
              copyable={false}
            />
          )
          const hasRetry = bill.status !== 'success' && bill.next_retry_at > 0
          if (!bill.error && !hasRetry) return badge
          return (
            <Tooltip>
              <TooltipTrigger render={<span className='inline-flex' />}>
                {badge}
              </TooltipTrigger>
              <TooltipContent className='max-w-xs break-all'>
                {bill.error && <p>{t(bill.error.key, bill.error.params)}</p>}
                {hasRetry && (
                  <p>
                    {t('Next retry at {{time}}', {
                      time: formatTimestampToDate(bill.next_retry_at),
                    })}
                  </p>
                )}
              </TooltipContent>
            </Tooltip>
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
        header: () => <span className='sr-only'>{t('Actions')}</span>,
        cell: ({ row }) => <ResyncBillButton bill={row.original} />,
      },
    ],
    [t]
  )
}
