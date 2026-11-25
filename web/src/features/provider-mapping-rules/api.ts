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
import { api } from '@/lib/api'
import { joinQueryList } from '@/lib/export-file'

import type {
  ApiResponse,
  Page,
  ProviderMappingChannelOption,
  ProviderMappingCreateBody,
  ProviderMappingListQuery,
  ProviderMappingPreview,
  ProviderMappingPreviewQuery,
  ProviderMappingRule,
  ProviderMappingSaveResult,
  ProviderMappingUpdateBody,
} from './types'

const BASE = '/api/provider_mapping_rule'

export async function listProviderMappingRules(
  query: ProviderMappingListQuery
): Promise<ApiResponse<Page<ProviderMappingRule>>> {
  const res = await api.get(`${BASE}/`, {
    params: {
      p: query.p,
      page_size: query.page_size,
      site_ids: joinQueryList(query.site_ids),
      types: joinQueryList(query.types),
      provider_ids: joinQueryList(query.provider_ids),
    },
  })
  return res.data
}

export async function createProviderMappingRule(
  body: ProviderMappingCreateBody
): Promise<ApiResponse<ProviderMappingSaveResult>> {
  const res = await api.post(`${BASE}/`, body)
  return res.data
}

export async function updateProviderMappingRule(
  id: number,
  body: ProviderMappingUpdateBody
): Promise<ApiResponse<ProviderMappingSaveResult>> {
  const res = await api.put(`${BASE}/${id}`, body)
  return res.data
}

export async function deleteProviderMappingRule(
  id: number
): Promise<ApiResponse<null>> {
  const res = await api.delete(`${BASE}/${id}`)
  return res.data
}

export async function previewProviderMappingRule(
  query: ProviderMappingPreviewQuery
): Promise<ApiResponse<ProviderMappingPreview>> {
  const res = await api.get(`${BASE}/preview`, { params: query })
  return res.data
}

export async function searchProviderMappingChannels(
  siteId: number,
  keyword: string
): Promise<ApiResponse<ProviderMappingChannelOption[]>> {
  const res = await api.get(`${BASE}/channels`, {
    params: { site_id: siteId, keyword },
  })
  return res.data
}
