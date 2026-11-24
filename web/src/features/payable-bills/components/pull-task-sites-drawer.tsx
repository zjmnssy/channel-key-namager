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
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table'
import {
  sideDrawerContentClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { StatusBadge } from '@/components/status-badge'
import { TruncatedText } from '@/components/truncated-text'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { formatBillDate } from '@/features/receivable-bills/lib/bill-date'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber, formatTimestampToDate } from '@/lib/format'
import { requireServerSuccess } from '@/lib/server-error-message'

import { listPayablePullSites } from '../api'
import { PAYABLE_BILL_QUERY_KEYS, PULL_SITE_STATUS_DISPLAY } from '../constants'
import type { PayablePullTask } from '../types'

const EMPTY = '—'

type PullTaskSitesDrawerProps = {
  task: PayablePullTask | null
  onOpenChange: (open: boolean) => void
}

export function PullTaskSitesDrawer(props: PullTaskSitesDrawerProps) {
  return (
    <Sheet open={props.task !== null} onOpenChange={props.onOpenChange}>
      {props.task && (
        <PullTaskSitesContent key={props.task.id} task={props.task} />
      )}
    </Sheet>
  )
}

function PullTaskSitesContent(props: { task: PayablePullTask }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const query = useQuery({
    queryKey: PAYABLE_BILL_QUERY_KEYS.taskSites(props.task.id),
    queryFn: async () =>
      requireServerSuccess(await listPayablePullSites(props.task.id)).data ??
      [],
  })

  let body: ReactNode
  if (query.isPending) {
    body = <LoadingState />
  } else if (query.isError) {
    body = (
      <ErrorState
        title={t('Failed to load site progress')}
        description={query.error.message}
        onRetry={() => void query.refetch()}
      />
    )
  } else {
    body = (
      <StaticDataTable
        data={query.data}
        getRowKey={(site) => site.id}
        emptyClassName='text-muted-foreground py-8 text-center'
        emptyContent={t('No Data')}
        columns={[
          {
            id: 'site_name',
            header: t('Site Name'),
            cell: (site) => (
              <TruncatedText text={site.site_name} maxWidth='max-w-[180px]' />
            ),
          },
          {
            id: 'status',
            header: t('Status'),
            cell: (site) => {
              const display = PULL_SITE_STATUS_DISPLAY[site.status]
              return (
                <StatusBadge
                  label={display ? t(display.labelKey) : site.status}
                  variant={display?.variant ?? 'neutral'}
                  copyable={false}
                />
              )
            },
          },
          {
            id: 'pulled_pages',
            header: t('Pulled Pages'),
            cell: (site) =>
              formatNumber(Math.max(site.next_page - 1, 0), locale),
          },
          {
            id: 'channel_count',
            header: t('Channel Count'),
            cell: (site) => formatNumber(site.channel_count, locale),
          },
          {
            id: 'attempts',
            header: t('Attempts'),
            cell: (site) => formatNumber(site.attempts, locale),
          },
          {
            id: 'error',
            header: t('Last Error'),
            cell: (site) =>
              site.error ? (
                <TruncatedText
                  text={t(site.error.key, site.error.params)}
                  maxWidth='max-w-[240px]'
                />
              ) : (
                EMPTY
              ),
          },
          {
            id: 'next_retry_at',
            header: t('Next Retry Time'),
            cell: (site) =>
              site.next_retry_at > 0
                ? formatTimestampToDate(site.next_retry_at)
                : EMPTY,
          },
          {
            id: 'note',
            header: t('Note'),
            cell: (site) =>
              site.note ? (
                <TruncatedText
                  text={t(site.note.key, site.note.params)}
                  maxWidth='max-w-[240px]'
                />
              ) : (
                EMPTY
              ),
          },
        ]}
      />
    )
  }

  return (
    <SheetContent className={sideDrawerContentClassName('sm:max-w-5xl')}>
      <SheetHeader className={sideDrawerHeaderClassName()}>
        <SheetTitle>{t('Site Progress')}</SheetTitle>
        <SheetDescription>
          {`${t('Task ID')} ${props.task.id} · ${t('Bill Date')} ${formatBillDate(props.task.bill_date)}`}
        </SheetDescription>
      </SheetHeader>
      <div className={sideDrawerFormClassName()}>{body}</div>
    </SheetContent>
  )
}
