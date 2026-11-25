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
import type { ApiResponse, Page } from '@/features/receivable-bills/types'

export type { ApiResponse, Page }

export type ProviderMappingRuleType = 'all' | 'unresolved' | 'channel'

export type ProviderMappingRule = {
  id: number
  site_id: number
  site_name: string
  site_deleted: boolean
  rule_type: ProviderMappingRuleType
  remote_channel_id: number // 0 = site level
  remote_channel_name: string
  provider_id: number
  provider_name: string
  provider_valid: boolean
  created_time: number
  updated_time: number
}

export type ProviderMappingChannelOption = {
  remote_channel_id: number
  remote_channel_name: string
  has_rule: boolean
}

/** Bill dates (YYYYMMDD) of the details that a saved rule is applied to. */
export type ProviderMappingApply = { start_date: number; end_date: number }

export type ProviderMappingSaveResult = {
  rule: ProviderMappingRule
  applied_rows: number
}

export type ProviderMappingPreview = { rows: number }

export type ProviderMappingListQuery = {
  p: number
  page_size: number
  site_ids?: number[]
  types?: ProviderMappingRuleType[]
  provider_ids?: number[]
}

export type ProviderMappingCreateBody = {
  site_id: number
  rule_type: ProviderMappingRuleType
  remote_channel_id?: number
  provider_id: number
  apply?: ProviderMappingApply
}

export type ProviderMappingUpdateBody = {
  provider_id: number
  apply?: ProviderMappingApply
}

export type ProviderMappingPreviewQuery = {
  site_id: number
  rule_type: ProviderMappingRuleType
  remote_channel_id: number
  provider_id: number
  start_date: number
  end_date: number
}

export type ProviderMappingDialogType = 'create' | 'update' | 'delete'
