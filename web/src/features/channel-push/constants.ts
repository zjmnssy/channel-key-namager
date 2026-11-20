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
import type { ChannelPushCategory } from './types'

export const CHANNEL_PUSH_QUERY_KEYS = {
  tasks: ['channel-push', 'tasks'],
  runs: ['channel-push', 'runs'],
  options: ['channel-push', 'options'],
} as const

/**
 * Fixed category order, matching the order the backend lists actions in.
 * `labelKey` titles the detail tabs; `shortLabelKey` fits the list summary.
 */
export const CHANNEL_PUSH_CATEGORIES: {
  value: ChannelPushCategory
  labelKey: string
  shortLabelKey: string
}[] = [
  { value: 'unchanged', labelKey: 'Unchanged', shortLabelKey: 'Unchanged' },
  { value: 'enable', labelKey: 'Enable channel', shortLabelKey: 'Enable' },
  { value: 'disable', labelKey: 'Disable channel', shortLabelKey: 'Disable' },
  {
    value: 'update_models',
    labelKey: 'Update model list',
    shortLabelKey: 'Update models',
  },
  { value: 'create', labelKey: 'Create', shortLabelKey: 'Create' },
  { value: 'orphan', labelKey: 'Orphan channel', shortLabelKey: 'Orphan' },
  {
    value: 'auto_disabled',
    labelKey: 'Auto-disabled on site',
    shortLabelKey: 'Auto-disabled',
  },
  {
    value: 'unsupported',
    labelKey: 'Unsupported for push',
    shortLabelKey: 'Unsupported',
  },
]

/** Channel status values shared by this platform and NewAPI sites. */
export const CHANNEL_PUSH_STATUS_LABELS: Record<number, string> = {
  1: 'Enabled',
  2: 'Manually disabled',
  3: 'Auto disabled',
}
