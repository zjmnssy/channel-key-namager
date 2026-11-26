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
import type { ProviderMappingListQuery, ProviderMappingRuleType } from './types'

export const PROVIDER_MAPPING_QUERY_KEYS = {
  all: ['provider-mapping-rules'],
  list: (query: ProviderMappingListQuery) =>
    ['provider-mapping-rules', 'list', query] as const,
  channels: (siteId: number, keyword: string) =>
    ['provider-mapping-rules', 'channels', siteId, keyword] as const,
  siteRules: (siteId: number) =>
    ['provider-mapping-rules', 'site-rules', siteId] as const,
}

export const RULE_TYPE_LABEL_KEYS: Record<ProviderMappingRuleType, string> = {
  all: 'All site channels',
  unresolved: 'Unresolved channels',
  channel: 'Specified channel',
}

export const RULE_TYPES: ProviderMappingRuleType[] = [
  'all',
  'unresolved',
  'channel',
]
