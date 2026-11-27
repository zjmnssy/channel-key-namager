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
import { useTranslation } from 'react-i18next'

import { requireServerSuccess } from '@/lib/server-error-message'

import { getPayableBillOptions } from '../api'
import { PAYABLE_BILL_QUERY_KEYS, UNKNOWN_PROVIDER_VALUE } from '../constants'

/**
 * Sites and providers for the filters and the provider dialog. `sites` and
 * `providers` keep the plain records; the options mark deleted sites and
 * disabled providers, and the provider options start with the unknown one.
 * Pass `enabled: false` where the user may not read them, such as a channel
 * supplier.
 */
export function usePayableBillOptions(options: { enabled?: boolean } = {}) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: PAYABLE_BILL_QUERY_KEYS.options,
    queryFn: async () =>
      requireServerSuccess(await getPayableBillOptions()).data ?? {
        sites: [],
        providers: [],
      },
    enabled: options.enabled ?? true,
  })
  const sites = query.data?.sites ?? []
  const providers = query.data?.providers ?? []
  const siteOptions = sites.map((site) => ({
    value: String(site.id),
    label: site.deleted
      ? t('{{name}} (deleted)', { name: site.name })
      : site.name,
  }))
  const providerOptions = [
    { value: UNKNOWN_PROVIDER_VALUE, label: t('Unknown provider') },
    ...providers.map((provider) => ({
      value: String(provider.id),
      label: provider.disabled
        ? t('{{name}} (disabled)', { name: provider.username })
        : provider.username,
    })),
  ]
  return { sites, siteOptions, providers, providerOptions }
}
