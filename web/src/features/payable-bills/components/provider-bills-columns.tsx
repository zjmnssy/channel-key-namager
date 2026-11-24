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
import { formatBillDate } from '@/features/receivable-bills/lib/bill-date'

import type { PayableProviderBill } from '../types'

const EMPTY = '—'

export function useProviderBillsColumns(): ColumnDef<PayableProviderBill>[] {
  const { t } = useTranslation()

  return useMemo<ColumnDef<PayableProviderBill>[]>(
    () => [
      {
        accessorKey: 'bill_date',
        header: t('Bill Date'),
        cell: ({ row }) => formatBillDate(row.original.bill_date),
      },
      {
        accessorKey: 'provider_name',
        header: t('Provider'),
        meta: { mobileTitle: true },
        cell: ({ row }) => {
          // Rows without a provider are billed together so none is missed.
          if (row.original.provider_id === 0) {
            return (
              <span className='text-muted-foreground'>
                {t('Unknown provider')}
              </span>
            )
          }
          return (
            <TruncatedText
              text={row.original.provider_name}
              maxWidth='max-w-[200px]'
            />
          )
        },
      },
      {
        accessorKey: 'amount',
        header: t('Amount Before Discount ($)'),
        cell: ({ row }) => (
          <span className='tabular-nums'>{row.original.amount}</span>
        ),
      },
      {
        accessorKey: 'discount',
        header: t('Discount (Payable)'),
        cell: ({ row }) => {
          const bill = row.original
          if (bill.discount_mixed) {
            return (
              <Tooltip>
                <TooltipTrigger
                  render={
                    <span
                      className='underline decoration-dotted underline-offset-4'
                      tabIndex={0}
                    />
                  }
                >
                  {t('Multiple')}
                </TooltipTrigger>
                <TooltipContent className='max-w-xs'>
                  {t('The rows of this day have different discounts')}
                </TooltipContent>
              </Tooltip>
            )
          }
          if (bill.discount_missing || bill.discount === null) {
            return <span className='text-muted-foreground'>{t('Not set')}</span>
          }
          return (
            <span className='tabular-nums'>
              <DiscountValue value={bill.discount} />
            </span>
          )
        },
      },
      {
        accessorKey: 'payable_amount',
        header: t('Payable Amount ($)'),
        cell: ({ row }) => {
          const bill = row.original
          if (bill.payable_amount === null) return EMPTY
          return (
            <div className='flex flex-wrap items-center gap-1.5'>
              <span className='tabular-nums'>{bill.payable_amount}</span>
              {/* Rows without a discount add nothing to the payable amount. */}
              {bill.discount_missing && (
                <Tooltip>
                  <TooltipTrigger
                    render={<span className='inline-flex' tabIndex={0} />}
                  >
                    <StatusBadge
                      label={t('Incomplete amount')}
                      variant='warning'
                      copyable={false}
                    />
                  </TooltipTrigger>
                  <TooltipContent className='max-w-xs'>
                    {t(
                      'Some rows have no discount and are not included in the payable amount'
                    )}
                  </TooltipContent>
                </Tooltip>
              )}
            </div>
          )
        },
      },
    ],
    [t]
  )
}
