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
import { useTranslation } from 'react-i18next'

import {
  DataTableFacetedFilter,
  DataTableToolbar,
} from '@/components/data-table'
import { UNKNOWN_PROVIDER_VALUE } from '@/features/payable-bills/constants'
import { usePayableBillOptions } from '@/features/payable-bills/hooks/use-payable-bill-options'

import { RULE_TYPE_LABEL_KEYS, RULE_TYPES } from '../constants'

const route = getRouteApi('/_authenticated/provider-mapping-rules/')

const TYPE_OPTIONS = RULE_TYPES.map((type) => ({
  value: type,
  label: RULE_TYPE_LABEL_KEYS[type],
}))

type MappingRulesFilterBarProps<TData> = {
  table: Table<TData>
}

/** The site, rule type and provider filters, kept in the URL. */
export function MappingRulesFilterBar<TData>(
  props: MappingRulesFilterBarProps<TData>
) {
  const { t } = useTranslation()
  const search = route.useSearch()
  const navigate = route.useNavigate()
  const options = usePayableBillOptions()
  const siteValues = search.sites ?? []
  const typeValues = search.types ?? []
  const providerValues = search.providers ?? []
  // A rule always has a provider, so the unknown one is no choice here.
  const providerOptions = options.providerOptions.filter(
    (option) => option.value !== UNKNOWN_PROVIDER_VALUE
  )

  // A filter change starts again from the first page.
  const updateFilters = (patch: Record<string, unknown>) =>
    void navigate({
      search: (prev) => ({ ...prev, ...patch, page: undefined }),
    })

  const hasFilters =
    siteValues.length > 0 || typeValues.length > 0 || providerValues.length > 0

  return (
    <DataTableToolbar
      table={props.table}
      hideViewOptions
      hasAdditionalFilters={hasFilters}
      onReset={() =>
        updateFilters({
          sites: undefined,
          types: undefined,
          providers: undefined,
        })
      }
      customSearch={
        <>
          <DataTableFacetedFilter
            title={t('Third-party site')}
            options={options.siteOptions}
            value={siteValues}
            onValueChange={(sites) => updateFilters({ sites })}
          />
          <DataTableFacetedFilter
            title={t('Rule Type')}
            options={TYPE_OPTIONS}
            value={typeValues}
            onValueChange={(types) => updateFilters({ types })}
          />
          <DataTableFacetedFilter
            title={t('Provider')}
            options={providerOptions}
            value={providerValues}
            onValueChange={(providers) => updateFilters({ providers })}
          />
        </>
      }
    />
  )
}
