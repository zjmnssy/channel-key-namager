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

import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { TableId } from '@/components/table-id'
import { TruncatedText } from '@/components/truncated-text'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber, formatTimestampToDate } from '@/lib/format'

import { CHANNEL_PUSH_CATEGORIES } from '../constants'
import type {
  ChannelPushCategory,
  ChannelPushRun,
  ChannelPushRunStatus,
  RunDetailKind,
} from '../types'

const RUN_STATUS_DISPLAY: Record<
  ChannelPushRunStatus,
  { labelKey: string; variant: StatusVariant }
> = {
  running: { labelKey: 'Running', variant: 'info' },
  success: { labelKey: 'Succeeded', variant: 'success' },
  partial: { labelKey: 'Partially failed', variant: 'warning' },
  failed: { labelKey: 'Failed', variant: 'danger' },
}

const CATEGORY_COUNT_FIELDS: Record<ChannelPushCategory, keyof ChannelPushRun> =
  {
    unchanged: 'unchanged_count',
    enable: 'enable_count',
    disable: 'disable_count',
    update_models: 'update_models_count',
    create: 'create_count',
    orphan: 'orphan_count',
    auto_disabled: 'auto_disabled_count',
    unsupported: 'unsupported_count',
  }

export function useRunsColumns(
  onOpenDetail: (run: ChannelPushRun, kind: RunDetailKind) => void
): ColumnDef<ChannelPushRun>[] {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)

  return useMemo<ColumnDef<ChannelPushRun>[]>(() => {
    // A run's details are not readable until the run ends.
    const detailLink = (
      run: ChannelPushRun,
      kind: RunDetailKind,
      label: string
    ) => (
      <Button
        variant='link'
        size='sm'
        className='h-auto p-0'
        disabled={run.status === 'running'}
        onClick={() => onOpenDetail(run, kind)}
      >
        {label}
      </Button>
    )

    return [
      {
        accessorKey: 'task_id',
        header: t('Owning Task ID'),
        cell: ({ row }) => <TableId value={row.original.task_id} />,
      },
      {
        accessorKey: 'started_at',
        header: t('Run Time'),
        cell: ({ row }) => formatTimestampToDate(row.original.started_at),
      },
      {
        accessorKey: 'status',
        header: t('Run Result'),
        cell: ({ row }) => {
          const display = RUN_STATUS_DISPLAY[row.original.status]
          const badge = (
            <StatusBadge
              label={display ? t(display.labelKey) : row.original.status}
              variant={display?.variant ?? 'neutral'}
              pulse={row.original.status === 'running'}
              copyable={false}
            />
          )
          const error = row.original.error
          if (!error) return badge
          return (
            <Tooltip>
              <TooltipTrigger render={<span className='inline-flex' />}>
                {badge}
              </TooltipTrigger>
              <TooltipContent className='max-w-xs break-all'>
                {t(error.key, error.params)}
              </TooltipContent>
            </Tooltip>
          )
        },
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
          <TruncatedText
            text={row.original.site_name}
            maxWidth='max-w-[200px]'
          />
        ),
      },
      {
        accessorKey: 'site_channel_count',
        header: t('Site Before Push'),
        cell: ({ row }) =>
          detailLink(
            row.original,
            'site',
            t('{{n}} items', {
              n: formatNumber(row.original.site_channel_count, locale),
            })
          ),
      },
      {
        accessorKey: 'target_channel_count',
        header: t('Push Target'),
        cell: ({ row }) =>
          detailLink(
            row.original,
            'target',
            t('{{n}} items', {
              n: formatNumber(row.original.target_channel_count, locale),
            })
          ),
      },
      {
        id: 'actions_summary',
        header: t('Push Actions'),
        cell: ({ row }) => {
          const summary = CHANNEL_PUSH_CATEGORIES.flatMap((category) => {
            const count = Number(
              row.original[CATEGORY_COUNT_FIELDS[category.value]]
            )
            if (!count) return []
            return `${t(category.shortLabelKey)} ${formatNumber(count, locale)}`
          }).join(' · ')
          return detailLink(row.original, 'actions', summary || t('No actions'))
        },
      },
    ]
  }, [t, locale, onOpenDetail])
}
