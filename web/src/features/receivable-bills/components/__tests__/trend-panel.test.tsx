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
import { type QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { toast } from 'sonner'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { createAppQueryClient } from '@/lib/query-client'

import * as billApi from '../../api'
import type { ReceivableTrend } from '../../types'
import { TrendPanel } from '../trend-panel'

vi.mock('../../api')
vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }))

type ChartDatum = {
  label: string
  site: string
  amount: number | null
  raw: string | null
  provisional: boolean
}
const chart = vi.hoisted(() =>
  vi.fn((_props: { spec: { data: { values: ChartDatum[] }[] } }) => null)
)
vi.mock('@visactor/react-vchart', () => ({ VChart: chart }))
vi.mock('@visactor/vchart', () => ({
  ThemeManager: { setCurrentTheme: vi.fn() },
}))

type TooltipPattern = {
  title: { value: (d: ChartDatum) => string }
  content: {
    key: (d: ChartDatum) => string
    value: (d: ChartDatum) => string
    visible: (d: ChartDatum) => boolean
  }[]
}

const clients: QueryClient[] = []

function trend(patch: Partial<ReceivableTrend> = {}): ReceivableTrend {
  return {
    view: 'day',
    labels: ['00:00', '01:00'],
    series: [
      {
        site_id: 1,
        site_name: 'Site A',
        deleted: false,
        amounts: ['1.000000', '2.500000'],
        provisional: [false, false],
      },
    ],
    missing_count: 0,
    ...patch,
  }
}

async function renderPanel(data: ReceivableTrend = trend()) {
  vi.mocked(billApi.getReceivableBillTrend).mockResolvedValue({
    success: true,
    data,
  })
  vi.mocked(billApi.listReceivableBillSites).mockResolvedValue({
    success: true,
    data: [{ id: 1, name: 'Site A', deleted: false }],
  })
  const i18n = createInstance()
  await i18n.init({ lng: 'en', resources: {}, initAsync: false })
  const client = createAppQueryClient()
  clients.push(client)
  const root = createRootRoute()
  const auth = createRoute({ getParentRoute: () => root, id: '_authenticated' })
  const pageRoute = createRoute({
    getParentRoute: () => auth,
    path: 'receivable-bills/',
    component: TrendPanel,
  })
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([pageRoute])]),
    history: createMemoryHistory({ initialEntries: ['/receivable-bills/'] }),
  })
  await router.load()
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  )
}

function lastValues(): ChartDatum[] {
  const props = chart.mock.calls.at(-1)?.[0]
  return props?.spec.data[0].values ?? []
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-11-22T16:30:00Z'))
  chart.mockClear()
})

afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  vi.useRealTimers()
  vi.restoreAllMocks()
  vi.mocked(billApi.getReceivableBillTrend).mockReset()
})

describe('TrendPanel', () => {
  it('loads the Beijing day view by default', async () => {
    await renderPanel()

    await waitFor(() =>
      expect(billApi.getReceivableBillTrend).toHaveBeenCalledWith(
        expect.objectContaining({ view: 'day', date: 20261123 })
      )
    )
  })

  it('switching to month resets the range to the current Beijing month', async () => {
    await renderPanel()
    await userEvent.click(screen.getByRole('button', { name: 'By Month' }))

    await waitFor(() =>
      expect(billApi.getReceivableBillTrend).toHaveBeenCalledWith(
        expect.objectContaining({
          view: 'month',
          start_date: 20261101,
          end_date: 20261123,
        })
      )
    )
  })

  it('keeps missing points as null instead of zero', async () => {
    await renderPanel(
      trend({
        series: [
          {
            site_id: 1,
            site_name: 'Site A',
            deleted: false,
            amounts: ['1.000000', null],
            provisional: [false, false],
          },
        ],
      })
    )

    await waitFor(() => expect(chart).toHaveBeenCalled())
    expect(lastValues().map((value) => value.amount)).toEqual([1, null])
  })

  it('marks provisional points', async () => {
    await renderPanel(
      trend({
        series: [
          {
            site_id: 1,
            site_name: 'Site A',
            deleted: false,
            amounts: ['1.000000', '2.000000'],
            provisional: [false, true],
          },
        ],
      })
    )

    await waitFor(() => expect(chart).toHaveBeenCalled())
    expect(lastValues().map((value) => value.provisional)).toEqual([
      false,
      true,
    ])
  })

  it('formats mark and dimension tooltips from the backend string and hides missing points', async () => {
    await renderPanel(
      trend({
        series: [
          {
            site_id: 1,
            site_name: 'Site A',
            deleted: false,
            amounts: ['1.500000', null],
            provisional: [true, false],
          },
        ],
      })
    )

    await waitFor(() => expect(chart).toHaveBeenCalled())
    const spec = chart.mock.calls.at(-1)?.[0].spec as unknown as {
      tooltip: Record<'mark' | 'dimension', TooltipPattern>
    }
    const [valued, missing] = lastValues()
    for (const pattern of [spec.tooltip.mark, spec.tooltip.dimension]) {
      const row = pattern.content[0]
      expect(pattern.title.value(valued)).toBe('00:00')
      expect(row.key(valued)).toBe('Site A')
      expect(row.value(valued)).toBe('$1.500000 (Provisional data)')
      expect(row.visible(valued)).toBe(true)
      expect(row.visible(missing)).toBe(false)
    }
  })

  it('labels a deleted site in the chart data', async () => {
    await renderPanel(
      trend({
        series: [
          {
            site_id: 1,
            site_name: 'Old',
            deleted: true,
            amounts: ['1.000000', '2.000000'],
            provisional: [false, false],
          },
        ],
      })
    )

    await waitFor(() => expect(chart).toHaveBeenCalled())
    expect(lastValues()[0].site).toBe('Old (deleted)')
  })

  it('shows how many points have no data', async () => {
    await renderPanel(trend({ missing_count: 2 }))

    expect(await screen.findByText('2 points have no data yet')).toBeVisible()
  })

  it('shows the empty state when there are no series', async () => {
    await renderPanel(trend({ series: [] }))

    expect(await screen.findByText('No trend data')).toBeVisible()
  })

  it('shows the server message and retries when the request fails', async () => {
    await renderPanel()
    vi.mocked(billApi.getReceivableBillTrend).mockResolvedValue({
      success: false,
      message: 'Date range cannot exceed 366 days',
    })
    await userEvent.click(screen.getByRole('button', { name: 'By Month' }))

    expect(
      await screen.findByText('Date range cannot exceed 366 days')
    ).toBeVisible()
    await userEvent.click(screen.getByRole('button', { name: 'Retry' }))
    await waitFor(() =>
      expect(billApi.getReceivableBillTrend).toHaveBeenCalledTimes(3)
    )
  })

  it('shows the failure once in the page without a toast', async () => {
    await renderPanel()
    vi.mocked(billApi.getReceivableBillTrend).mockResolvedValue({
      success: false,
      message: 'Date range cannot exceed 366 days',
    })
    await userEvent.click(screen.getByRole('button', { name: 'By Month' }))

    expect(
      await screen.findByText('Date range cannot exceed 366 days')
    ).toBeVisible()
    expect(toast.error).not.toHaveBeenCalled()
  })
})
