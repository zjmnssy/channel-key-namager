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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Combobox } from '@/components/ui/combobox'
import { useDebounce } from '@/hooks/use-debounce'
import { requireServerSuccess } from '@/lib/server-error-message'

import { searchProviderMappingChannels } from '../api'
import { PROVIDER_MAPPING_QUERY_KEYS } from '../constants'
import { siteChannelLabel } from '../lib/site-channel'
import type { ProviderMappingChannelOption } from '../types'

const SEARCH_DEBOUNCE_MS = 300

type ChannelSearchFieldProps = {
  siteId: number
  value: string // remote channel ID, '' for none
  // The chosen channel comes along so its name can be shown elsewhere.
  onValueChange: (
    value: string,
    channel: ProviderMappingChannelOption | null
  ) => void
  disabled?: boolean
  id?: string
  'aria-describedby'?: string
  'aria-invalid'?: boolean
}

/** Searches the channels pulled from the site; channels with a rule are shown but cannot be chosen. */
export function ChannelSearchField(props: ChannelSearchFieldProps) {
  const { t } = useTranslation()
  const [term, setTerm] = useState('')
  const keyword = useDebounce(term.trim(), SEARCH_DEBOUNCE_MS)
  // The list searches again with '' whenever it closes, also right after a
  // choice, and a channel found by its full ID may not be among those results.
  const [chosen, setChosen] = useState<ProviderMappingChannelOption | null>(
    null
  )

  const query = useQuery({
    queryKey: PROVIDER_MAPPING_QUERY_KEYS.channels(props.siteId, keyword),
    queryFn: async () =>
      requireServerSuccess(
        await searchProviderMappingChannels(props.siteId, keyword)
      ).data ?? [],
    enabled: !props.disabled,
  })

  const results = query.data ?? []
  const keepChosen =
    chosen !== null &&
    String(chosen.remote_channel_id) === props.value &&
    !results.some(
      (channel) => channel.remote_channel_id === chosen.remote_channel_id
    )
  const channels = keepChosen ? [chosen, ...results] : results
  const options = channels.map((channel) => ({
    value: String(channel.remote_channel_id),
    label: siteChannelLabel(
      channel.remote_channel_id,
      channel.remote_channel_name
    ),
    disabled: channel.has_rule,
    description: channel.has_rule ? t('Already has a rule') : undefined,
  }))

  return (
    <Combobox
      id={props.id}
      aria-describedby={props['aria-describedby']}
      aria-invalid={props['aria-invalid']}
      disabled={props.disabled}
      options={options}
      value={props.value || null}
      onValueChange={(value) => {
        const channel =
          channels.find((item) => String(item.remote_channel_id) === value) ??
          null
        setChosen(channel)
        props.onValueChange(value ?? '', channel)
      }}
      onSearchChange={setTerm}
      filterOptions={false}
      placeholder={t('Search by channel name or ID')}
      emptyText={query.isFetching ? t('Loading...') : t('No results found')}
    />
  )
}
