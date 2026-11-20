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
import type { Table } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'

import { DataTableToolbar } from '@/components/data-table'
import { DateTimePicker } from '@/components/datetime-picker'
import { Combobox } from '@/components/ui/combobox'
import { requireServerSuccess } from '@/lib/server-error-message'

import { getChannelPushOptions } from '../api'
import { CHANNEL_PUSH_QUERY_KEYS } from '../constants'
import type { ChannelPushRun } from '../types'

export type RunFilters = {
  startFrom?: number // Unix seconds
  startTo?: number
  supplierId?: number
  siteId?: number
}

const ALL = 'all'

export function RunsFilters(props: {
  table: Table<ChannelPushRun>
  filters: RunFilters
  onChange: (patch: RunFilters) => void
  onReset: () => void
}) {
  const { t } = useTranslation()
  // Same key and shape as the task dialog, so both share one cache entry.
  const optionsQuery = useQuery({
    queryKey: CHANNEL_PUSH_QUERY_KEYS.options,
    queryFn: async () =>
      requireServerSuccess(await getChannelPushOptions()).data ?? {
        sites: [],
        suppliers: [],
      },
  })
  const supplierOptions = [
    { value: ALL, label: t('All provider users') },
    ...(optionsQuery.data?.suppliers ?? []).map((supplier) => ({
      value: String(supplier.id),
      label: `${supplier.username} (#${supplier.id})`,
    })),
  ]
  const siteOptions = [
    { value: ALL, label: t('All third-party sites') },
    ...(optionsQuery.data?.sites ?? []).map((site) => ({
      value: String(site.id),
      label: site.name,
    })),
  ]
  const hasFilters = Object.values(props.filters).some(
    (value) => value !== undefined
  )

  return (
    <DataTableToolbar
      table={props.table}
      hideViewOptions
      hasAdditionalFilters={hasFilters}
      onReset={props.onReset}
      customSearch={
        <>
          <DateTimePicker
            className='w-full sm:w-[300px]'
            placeholder={t('Start time from')}
            value={
              props.filters.startFrom === undefined
                ? undefined
                : new Date(props.filters.startFrom * 1000)
            }
            onChange={(date) =>
              props.onChange({
                startFrom: date ? Math.floor(date.getTime() / 1000) : undefined,
              })
            }
          />
          <DateTimePicker
            className='w-full sm:w-[300px]'
            placeholder={t('Start time to')}
            value={
              props.filters.startTo === undefined
                ? undefined
                : new Date(props.filters.startTo * 1000)
            }
            onChange={(date) =>
              props.onChange({
                startTo: date ? Math.floor(date.getTime() / 1000) : undefined,
              })
            }
          />
          <Combobox
            aria-label={t('Provider user')}
            className='w-full sm:w-[200px]'
            options={supplierOptions}
            value={
              props.filters.supplierId === undefined
                ? ALL
                : String(props.filters.supplierId)
            }
            onValueChange={(value) =>
              props.onChange({
                supplierId: !value || value === ALL ? undefined : Number(value),
              })
            }
          />
          <Combobox
            aria-label={t('Third-party site')}
            className='w-full sm:w-[200px]'
            options={siteOptions}
            value={
              props.filters.siteId === undefined
                ? ALL
                : String(props.filters.siteId)
            }
            onValueChange={(value) =>
              props.onChange({
                siteId: !value || value === ALL ? undefined : Number(value),
              })
            }
          />
        </>
      }
    />
  )
}
