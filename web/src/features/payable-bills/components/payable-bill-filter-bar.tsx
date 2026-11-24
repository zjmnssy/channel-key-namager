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
import { getRouteApi } from '@tanstack/react-router'
import type { Table } from '@tanstack/react-table'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import {
  DataTableFacetedFilter,
  DataTableToolbar,
} from '@/components/data-table'
import { BillDateRangePicker } from '@/features/receivable-bills/components/bill-date-range-picker'
import { beijingYesterday } from '@/features/receivable-bills/lib/bill-date'

import { usePayableBillFilter } from '../hooks/use-payable-bill-filter'
import { usePayableBillOptions } from '../hooks/use-payable-bill-options'

const route = getRouteApi('/_authenticated/payable-bills/')

type PayableBillFilterBarProps<TData> = {
  table: Table<TData>
  /** Buttons before the toolbar's Reset, such as the export button. */
  actions?: ReactNode
}

/** The date, site and provider filters of the details and provider bills tabs. */
export function PayableBillFilterBar<TData>(
  props: PayableBillFilterBarProps<TData>
) {
  const { t } = useTranslation()
  const search = route.useSearch()
  const navigate = route.useNavigate()
  const options = usePayableBillOptions()
  const filter = usePayableBillFilter()
  const siteValues = search.sites ?? []
  const providerValues = search.providers ?? []

  // A filter change starts again from the first page.
  const updateFilters = (patch: Record<string, unknown>) =>
    void navigate({
      search: (prev) => ({ ...prev, ...patch, page: undefined }),
    })

  const hasFilters =
    search.start !== undefined ||
    search.end !== undefined ||
    siteValues.length > 0 ||
    providerValues.length > 0

  return (
    <DataTableToolbar
      table={props.table}
      hideViewOptions
      hasAdditionalFilters={hasFilters}
      preActions={props.actions}
      onReset={() =>
        updateFilters({
          start: undefined,
          end: undefined,
          sites: undefined,
          providers: undefined,
        })
      }
      customSearch={
        <>
          <BillDateRangePicker
            start={filter.start_date}
            end={filter.end_date}
            max={beijingYesterday()}
            onChange={(start, end) => updateFilters({ start, end })}
          />
          <DataTableFacetedFilter
            title={t('Site Name')}
            options={options.siteOptions}
            value={siteValues}
            onValueChange={(sites) => updateFilters({ sites })}
          />
          <DataTableFacetedFilter
            title={t('Provider')}
            options={options.providerOptions}
            value={providerValues}
            onValueChange={(providers) => updateFilters({ providers })}
          />
        </>
      }
    />
  )
}
