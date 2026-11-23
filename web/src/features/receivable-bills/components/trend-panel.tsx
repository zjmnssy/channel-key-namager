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

import { DataTableFacetedFilter } from '@/components/data-table'
import { DatePicker } from '@/components/date-picker'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import {
  getServerErrorMessage,
  requireServerSuccess,
} from '@/lib/server-error-message'

import { getReceivableBillTrend } from '../api'
import { RECEIVABLE_BILL_QUERY_KEYS } from '../constants'
import { useReceivableBillSiteOptions } from '../hooks/use-receivable-bill-site-options'
import {
  beijingToday,
  billDateToLocalDate,
  localDateToBillDate,
  monthStart,
} from '../lib/bill-date'
import type { TrendQuery, TrendView } from '../types'
import { BillDateRangePicker } from './bill-date-range-picker'
import { TrendChart } from './trend-chart'

const route = getRouteApi('/_authenticated/receivable-bills/')

export function TrendPanel() {
  const { t } = useTranslation()
  const search = route.useSearch()
  const navigate = route.useNavigate()

  const view: TrendView = search.view === 'month' ? 'month' : 'day'
  const today = beijingToday()
  const date = search.date ?? today
  const startDate = search.trendStart ?? monthStart(today)
  const endDate = search.trendEnd ?? today
  const siteValues = search.trendSites ?? []

  const query: TrendQuery =
    view === 'day'
      ? { view, date }
      : { view, start_date: startDate, end_date: endDate }
  if (siteValues.length > 0) query.site_ids = siteValues.map(Number)

  const trendQuery = useQuery({
    queryKey: RECEIVABLE_BILL_QUERY_KEYS.trend(query),
    queryFn: async () =>
      requireServerSuccess(await getReceivableBillTrend(query)),
    // The ErrorState below already shows the reason.
    meta: { errorToast: false },
  })
  const trend = trendQuery.data?.data

  const sites = useReceivableBillSiteOptions()

  const updateSearch = (patch: Record<string, unknown>) =>
    void navigate({ search: (prev) => ({ ...prev, ...patch }) })

  let content
  if (trendQuery.isError) {
    content = (
      <ErrorState
        description={getServerErrorMessage(trendQuery.error)}
        onRetry={() => void trendQuery.refetch()}
      />
    )
  } else if (trend === undefined) {
    content = <LoadingState />
  } else if (trend.series.length === 0) {
    content = <EmptyState title={t('No trend data')} />
  } else {
    content = (
      <>
        <TrendChart trend={trend} />
        {trend.missing_count > 0 && (
          <p className='text-muted-foreground text-sm'>
            {t('{{count}} points have no data yet', {
              count: trend.missing_count,
            })}
          </p>
        )}
      </>
    )
  }

  return (
    <div className='flex flex-col gap-4'>
      <div className='flex flex-wrap items-center gap-2'>
        <ToggleGroup
          value={[view]}
          onValueChange={(values) => {
            const next = values[0]
            if ((next !== 'day' && next !== 'month') || next === view) return
            // Each view starts from its own default dates.
            updateSearch({
              view: next,
              date: undefined,
              trendStart: undefined,
              trendEnd: undefined,
            })
          }}
          variant='outline'
          size='sm'
        >
          <ToggleGroupItem value='day'>{t('By Day')}</ToggleGroupItem>
          <ToggleGroupItem value='month'>{t('By Month')}</ToggleGroupItem>
        </ToggleGroup>
        {view === 'day' ? (
          <DatePicker
            selected={billDateToLocalDate(date)}
            maxDate={billDateToLocalDate(today)}
            onSelect={(selected) =>
              selected && updateSearch({ date: localDateToBillDate(selected) })
            }
          />
        ) : (
          <BillDateRangePicker
            start={startDate}
            end={endDate}
            max={today}
            onChange={(start, end) =>
              updateSearch({ trendStart: start, trendEnd: end })
            }
          />
        )}
        <DataTableFacetedFilter
          title={t('Site Name')}
          options={sites.options}
          value={siteValues}
          onValueChange={(sites) => updateSearch({ trendSites: sites })}
        />
      </div>
      {content}
    </div>
  )
}
