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
import { VChart } from '@visactor/react-vchart'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { useChartTheme } from '@/lib/use-chart-theme'
import { VCHART_OPTION } from '@/lib/vchart'

import { formatTrendTooltip } from '../lib/trend-tooltip'
import type { ReceivableTrend } from '../types'

type TrendDatum = {
  label: string
  site: string
  amount: number | null
  raw: string | null // the backend string, shown in the tooltip
  provisional: boolean
  missing: boolean // the data could not be fetched; drawn at 0
}

const MISSING_POINT_COLOR = '#ef4444'

// The part of VChart's mark style context used here; the package does not export the type.
type SeriesMarkContext = {
  seriesColor: (seriesValue?: string | number) => string
}

type TrendChartProps = { trend: ReceivableTrend }

export function TrendChart(props: TrendChartProps) {
  const { t } = useTranslation()
  const { resolvedTheme, themeReady } = useChartTheme()
  const textColor =
    resolvedTheme === 'dark'
      ? 'rgba(255, 255, 255, 0.68)'
      : 'rgba(15, 23, 42, 0.58)'
  const gridColor =
    resolvedTheme === 'dark'
      ? 'rgba(255, 255, 255, 0.12)'
      : 'rgba(15, 23, 42, 0.12)'

  const spec = useMemo(() => {
    const values: TrendDatum[] = props.trend.series.flatMap((series) => {
      const site = series.deleted
        ? t('{{name}} (deleted)', { name: series.site_name })
        : series.site_name
      return props.trend.labels.map((label, index) => {
        const raw = series.amounts[index]
        const missing = series.missing[index] === true
        // A missing point is drawn at 0; any other point without data (not due yet,
        // before the site's first data) stays null and breaks the line.
        let amount: number | null = null
        if (missing) amount = 0
        else if (raw != null) amount = Number(raw)
        return {
          label,
          site,
          amount,
          raw: raw ?? null,
          provisional: series.provisional[index] === true,
          missing,
        }
      })
    })
    const tooltipPattern = {
      title: { value: (d: TrendDatum) => d.label },
      content: [
        {
          key: (d: TrendDatum) => d.site,
          value: (d: TrendDatum) => formatTrendTooltip(d, t),
          // A blank point has no row; a missing one says it failed.
          visible: (d: TrendDatum) => d.raw != null || d.missing,
        },
      ],
    }
    return {
      type: 'line' as const,
      data: [{ id: 'trend', values }],
      xField: 'label',
      yField: 'amount',
      seriesField: 'site',
      invalidType: 'break' as const,
      point: {
        visible: true,
        style: {
          size: 5,
          fill: (d: TrendDatum, ctx: SeriesMarkContext) =>
            d.missing ? MISSING_POINT_COLOR : ctx.seriesColor(d.site),
        },
      },
      line: { style: { lineWidth: 2 } },
      legends: { visible: true, orient: 'top' as const },
      // Mark and dimension tooltips share one pattern: the dimension tooltip
      // is on by default and would otherwise show Number(amount).
      tooltip: { mark: tooltipPattern, dimension: tooltipPattern },
      axes: [
        {
          orient: 'bottom' as const,
          label: { style: { fill: textColor, fontSize: 10 } },
          tick: { visible: false },
        },
        {
          orient: 'left' as const,
          title: {
            visible: true,
            text:
              props.trend.view === 'day'
                ? t('Hourly Amount ($)')
                : t('Daily Amount ($)'),
            style: { fill: textColor, fontSize: 11 },
          },
          label: { style: { fill: textColor, fontSize: 10 } },
          grid: {
            visible: true,
            style: { lineDash: [3, 3], stroke: gridColor },
          },
        },
      ],
    }
  }, [gridColor, props.trend, t, textColor])

  return (
    <div className='h-80 min-h-0 w-full'>
      {themeReady && (
        <VChart
          key={`receivable-trend-${resolvedTheme}`}
          spec={{
            ...spec,
            theme: resolvedTheme === 'dark' ? 'dark' : 'light',
            background: 'transparent',
          }}
          option={VCHART_OPTION}
        />
      )}
    </div>
  )
}
