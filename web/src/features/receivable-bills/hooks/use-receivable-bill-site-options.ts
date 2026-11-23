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

import { listReceivableBillSites } from '../api'
import { RECEIVABLE_BILL_QUERY_KEYS } from '../constants'

/** Sites for the filters: `sites` keeps plain names, `options` adds "(deleted)". */
export function useReceivableBillSiteOptions() {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: RECEIVABLE_BILL_QUERY_KEYS.sites,
    queryFn: async () =>
      requireServerSuccess(await listReceivableBillSites()).data ?? [],
  })
  const sites = query.data ?? []
  const options = sites.map((site) => ({
    value: String(site.id),
    label: site.deleted
      ? t('{{name}} (deleted)', { name: site.name })
      : site.name,
  }))
  return { sites, options }
}
