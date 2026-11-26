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
import { TriangleAlert } from 'lucide-react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { BadgeListCell } from '@/components/data-table'
import { DiscountValue } from '@/components/discount-value'
import { StatusBadge } from '@/components/status-badge'
import { TableId } from '@/components/table-id'
import { TruncatedText } from '@/components/truncated-text'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { formatTimestampToDate } from '@/lib/format'

import { SETTLEMENT_METHOD_LABEL_KEYS } from '../constants'
import type { PayableSettlement } from '../types'

const EMPTY = '—'

export function useSettlementMethodsColumns(
  onChange: (row: PayableSettlement) => void
): ColumnDef<PayableSettlement>[] {
  const { t } = useTranslation()

  return useMemo<ColumnDef<PayableSettlement>[]>(
    () => [
      {
        accessorKey: 'provider_id',
        header: t('Provider ID'),
        cell: ({ row }) => <TableId value={row.original.provider_id} />,
      },
      {
        accessorKey: 'username',
        header: t('Provider'),
        meta: { mobileTitle: true },
        cell: ({ row }) => (
          <TruncatedText
            text={
              row.original.disabled
                ? t('{{name}} (disabled)', { name: row.original.username })
                : row.original.username
            }
            maxWidth='max-w-[200px]'
          />
        ),
      },
      {
        accessorKey: 'payable_discount',
        header: t('Payable Discount'),
        cell: ({ row }) => {
          const discount = row.original.payable_discount
          if (discount === null) {
            return <span className='text-muted-foreground'>{t('Not set')}</span>
          }
          return (
            <span className='tabular-nums'>
              <DiscountValue value={discount} />
            </span>
          )
        },
      },
      {
        accessorKey: 'method',
        header: t('Settlement Method'),
        cell: ({ row }) => t(SETTLEMENT_METHOD_LABEL_KEYS[row.original.method]),
      },
      {
        accessorKey: 'whole_sites',
        header: t('Fully Assigned Sites'),
        cell: ({ row }) => {
          const sites = row.original.whole_sites
          if (sites.length === 0) return EMPTY
          return (
            <BadgeListCell
              expandable
              items={sites.map((site) => {
                if (!site.conflict) {
                  return (
                    <StatusBadge
                      key={site.site_id}
                      label={site.site_name}
                      variant='neutral'
                      copyable={false}
                    />
                  )
                }
                // The site is still settled by channel details, whatever the
                // provider's method is.
                return (
                  <Tooltip key={site.site_id}>
                    <TooltipTrigger
                      render={<span className='inline-flex' tabIndex={0} />}
                    >
                      <StatusBadge
                        label={site.site_name}
                        icon={TriangleAlert}
                        variant='warning'
                        copyable={false}
                      />
                    </TooltipTrigger>
                    <TooltipContent className='max-w-xs'>
                      {t(
                        'Some channels of this site are assigned to other providers, so it is still settled by channel details'
                      )}
                    </TooltipContent>
                  </Tooltip>
                )
              })}
            />
          )
        },
      },
      {
        accessorKey: 'updated_time',
        header: t('Updated At'),
        cell: ({ row }) => {
          // 0 when the method was never set, so it is channel details.
          const updatedTime = row.original.updated_time
          return updatedTime === 0 ? EMPTY : formatTimestampToDate(updatedTime)
        },
      },
      {
        id: 'actions',
        header: t('Actions'),
        cell: ({ row }) => (
          <Button
            variant='link'
            size='sm'
            className='h-auto p-0'
            onClick={() => onChange(row.original)}
          >
            {t('Modify')}
          </Button>
        ),
      },
    ],
    [t, onChange]
  )
}
