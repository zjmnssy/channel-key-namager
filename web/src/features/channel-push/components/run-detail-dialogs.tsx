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
import type { ColumnDef } from '@tanstack/react-table'
import type { TFunction } from 'i18next'
import { useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import {
  DataTablePagination,
  DataTableView,
  useDataTable,
} from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { StatusBadge } from '@/components/status-badge'
import { TableId } from '@/components/table-id'
import { TruncatedText } from '@/components/truncated-text'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber, formatTimestampToDate } from '@/lib/format'
import { requireServerSuccess } from '@/lib/server-error-message'

import { getChannelPushRunDetail } from '../api'
import {
  CHANNEL_PUSH_CATEGORIES,
  CHANNEL_PUSH_STATUS_LABELS,
} from '../constants'
import type {
  ChannelPushAction,
  ChannelPushCategory,
  ChannelPushRun,
  RunDetailKind,
  SiteChannel,
  SiteSnapshot,
  TargetChannel,
} from '../types'

const DETAIL_PAGE_SIZE = 10

/** Status 1/2/3 by name; any other value is shown as the raw number. */
function channelPushStatusLabel(t: TFunction, status: number): string {
  const labelKey = CHANNEL_PUSH_STATUS_LABELS[status]
  return labelKey ? t(labelKey) : String(status)
}

type RunDetailDialogProps = {
  run: ChannelPushRun
  kind: RunDetailKind
  open: boolean
  onOpenChange: (open: boolean) => void
}

const DIALOG_TITLE_KEYS: Record<RunDetailKind, string> = {
  site: 'Site Before Push',
  target: 'Push Target',
  actions: 'Push Actions',
}

export function RunDetailDialog(props: RunDetailDialogProps) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['channel-push', 'run-detail', props.run.id, props.kind],
    queryFn: async () =>
      requireServerSuccess(
        await getChannelPushRunDetail<unknown>(props.run.id, props.kind)
      ).data ?? null,
    enabled: props.open,
  })

  let body: ReactNode
  if (query.isPending) {
    body = <LoadingState />
  } else if (query.isError) {
    body = (
      <ErrorState
        title={t('Failed to load details')}
        onRetry={() => void query.refetch()}
      />
    )
  } else if (query.data === null) {
    body = <EmptyState title={t('Details were not saved')} />
  } else if (props.kind === 'site') {
    body = <SiteSnapshotView snapshot={query.data as SiteSnapshot} />
  } else if (props.kind === 'target') {
    body = <TargetChannelsTable channels={query.data as TargetChannel[]} />
  } else {
    body = <ActionsView actions={query.data as ChannelPushAction[]} />
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t(DIALOG_TITLE_KEYS[props.kind])}
      description={`${t('Task ID')} ${props.run.task_id} · ${formatTimestampToDate(props.run.started_at)}`}
      contentClassName='max-w-[calc(100%-2rem)] sm:max-w-[90vw] xl:max-w-[1200px]'
      contentHeight='min(70vh, 680px)'
      bodyClassName='flex h-full min-h-0 flex-col gap-3'
    >
      {body}
    </Dialog>
  )
}

function DetailTable<TData>(props: {
  data: TData[]
  columns: ColumnDef<TData, unknown>[]
}) {
  const { t } = useTranslation()
  const { table } = useDataTable({
    data: props.data,
    columns: props.columns,
    initialPagination: { pageIndex: 0, pageSize: DETAIL_PAGE_SIZE },
    columnVisibilityStorageKey: false,
    columnSizingStorageKey: false,
    withSortedRowModel: false,
    withFacetedRowModel: false,
  })

  return (
    <>
      <DataTableView
        table={table}
        containerClassName='min-h-0 flex-1 overflow-auto rounded-md'
        emptyContent={t('No Data')}
        emptyCellClassName='h-24 text-center'
      />
      <div className='shrink-0'>
        <DataTablePagination table={table} />
      </div>
    </>
  )
}

function SiteSnapshotView(props: { snapshot: SiteSnapshot }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const columns = useMemo<ColumnDef<SiteChannel, unknown>[]>(
    () => [
      {
        accessorKey: 'id',
        header: t('Site Channel ID'),
        cell: ({ row }) => <TableId value={row.original.id} />,
      },
      {
        accessorKey: 'name',
        header: t('Name'),
        cell: ({ row }) => <TruncatedText text={row.original.name} />,
      },
      {
        accessorKey: 'used_quota',
        header: t('Used Quota'),
        cell: ({ row }) => formatNumber(row.original.used_quota, locale),
      },
      {
        accessorKey: 'status',
        header: t('Status'),
        cell: ({ row }) => channelPushStatusLabel(t, row.original.status),
      },
      {
        accessorKey: 'models',
        header: t('Models'),
        cell: ({ row }) => (
          <TruncatedText text={row.original.models} maxWidth='max-w-[280px]' />
        ),
      },
    ],
    [t, locale]
  )

  return (
    <>
      <p className='text-muted-foreground shrink-0 text-sm'>
        {t('Fetched at')} {formatTimestampToDate(props.snapshot.fetched_at)}
      </p>
      <DetailTable data={props.snapshot.channels ?? []} columns={columns} />
    </>
  )
}

function TargetChannelsTable(props: { channels: TargetChannel[] }) {
  const { t } = useTranslation()
  const columns = useMemo<ColumnDef<TargetChannel, unknown>[]>(
    () => [
      {
        accessorKey: 'id',
        header: t('Local Channel ID'),
        cell: ({ row }) => <TableId value={row.original.id} />,
      },
      {
        accessorKey: 'name',
        header: t('Name'),
        cell: ({ row }) => <TruncatedText text={row.original.name} />,
      },
      {
        accessorKey: 'supplier_user_id',
        header: t('Supplier User ID'),
        cell: ({ row }) => <TableId value={row.original.supplier_user_id} />,
      },
      {
        accessorKey: 'supplier_username',
        header: t('Supplier Username'),
        cell: ({ row }) => (
          <TruncatedText text={row.original.supplier_username} />
        ),
      },
      {
        accessorKey: 'status',
        header: t('Status'),
        cell: ({ row }) => channelPushStatusLabel(t, row.original.status),
      },
      {
        accessorKey: 'models',
        header: t('Models'),
        cell: ({ row }) => (
          <TruncatedText text={row.original.models} maxWidth='max-w-[280px]' />
        ),
      },
    ],
    [t]
  )

  return <DetailTable data={props.channels ?? []} columns={columns} />
}

function ActionsView(props: { actions: ChannelPushAction[] }) {
  const { t } = useTranslation()
  const actions = props.actions
  const counts = useMemo(() => {
    const result: Partial<Record<ChannelPushCategory, number>> = {}
    for (const action of actions) {
      result[action.category] = (result[action.category] ?? 0) + 1
    }
    return result
  }, [actions])
  const [category, setCategory] = useState<ChannelPushCategory>(
    () =>
      CHANNEL_PUSH_CATEGORIES.find((item) => counts[item.value])?.value ??
      'unchanged'
  )
  const visible = useMemo(
    () => actions.filter((action) => action.category === category),
    [actions, category]
  )

  return (
    <>
      <Tabs
        value={category}
        onValueChange={(value) => setCategory(value as ChannelPushCategory)}
        className='shrink-0'
      >
        <TabsList className='max-w-full flex-wrap justify-start group-data-horizontal/tabs:h-auto'>
          {CHANNEL_PUSH_CATEGORIES.map((item) => (
            <TabsTrigger key={item.value} value={item.value}>
              <span>{t(item.labelKey)}</span>
              <Badge variant='secondary'>{counts[item.value] ?? 0}</Badge>
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
      <ActionsTable key={category} actions={visible} />
    </>
  )
}

function ActionsTable(props: { actions: ChannelPushAction[] }) {
  const { t } = useTranslation()
  const columns = useMemo<ColumnDef<ChannelPushAction, unknown>[]>(
    () => [
      {
        id: 'local',
        header: t('Local Channel'),
        cell: ({ row }) => {
          if (!row.original.local_channel_id) return '—'
          return (
            <span className='flex items-center gap-1.5'>
              <TableId value={row.original.local_channel_id} />
              <TruncatedText text={row.original.local_channel_name} />
            </span>
          )
        },
      },
      {
        id: 'remote',
        header: t('Site Channel'),
        cell: ({ row }) => (
          <span className='flex items-center gap-1.5'>
            {row.original.remote_channel_id > 0 && (
              <TableId value={row.original.remote_channel_id} />
            )}
            {row.original.remote_channel_name ? (
              <TruncatedText text={row.original.remote_channel_name} />
            ) : (
              row.original.remote_channel_id <= 0 && '—'
            )}
            {row.original.duplicate && (
              <StatusBadge
                label={t('Duplicate')}
                variant='warning'
                copyable={false}
              />
            )}
          </span>
        ),
      },
      {
        id: 'change',
        header: t('Change'),
        cell: ({ row }) => <ActionChange action={row.original} />,
      },
      {
        id: 'result',
        header: t('Result'),
        cell: ({ row }) => <ActionResult action={row.original} />,
      },
    ],
    [t]
  )

  return <DetailTable data={props.actions} columns={columns} />
}

function ActionChange(props: { action: ChannelPushAction }) {
  const { t } = useTranslation()
  const action = props.action

  if (action.category === 'enable' || action.category === 'disable') {
    return `${channelPushStatusLabel(t, action.status_from)} → ${channelPushStatusLabel(t, action.status_to)}`
  }
  if (action.category === 'update_models') {
    const parts: string[] = []
    if (action.models_added?.length) {
      parts.push(`+${action.models_added.join(', ')}`)
    }
    if (action.models_removed?.length) {
      parts.push(`−${action.models_removed.join(', ')}`)
    }
    return <TruncatedText text={parts.join(' / ')} maxWidth='max-w-[320px]' />
  }
  if (action.category !== 'orphan' && action.category !== 'unsupported') {
    return '—'
  }

  const reason = action.reason ? t(action.reason.key, action.reason.params) : ''
  return (
    <div className='flex min-w-0 flex-col gap-0.5'>
      <span className='whitespace-normal'>{reason}</span>
      {action.category === 'orphan' && (
        <TruncatedText
          text={`${channelPushStatusLabel(t, action.remote_status)} · ${action.remote_models || '—'}`}
          className='text-muted-foreground text-xs'
          maxWidth='max-w-[320px]'
        />
      )}
    </div>
  )
}

function ActionResult(props: { action: ChannelPushAction }) {
  const { t } = useTranslation()
  const action = props.action

  if (action.result === 'success') {
    return (
      <StatusBadge label={t('Succeeded')} variant='success' copyable={false} />
    )
  }
  if (action.result !== 'failed') {
    return (
      <StatusBadge label={t('Not called')} variant='neutral' copyable={false} />
    )
  }
  return (
    <div className='flex min-w-0 flex-col items-start gap-0.5'>
      <StatusBadge label={t('Failed')} variant='danger' copyable={false} />
      {action.error && (
        <span className='text-destructive text-xs whitespace-normal'>
          {t(action.error.key, action.error.params)}
        </span>
      )}
    </div>
  )
}
