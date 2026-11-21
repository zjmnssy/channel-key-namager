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
export type ApiResponse<T> = {
  success: boolean
  message?: string
  message_key?: string
  message_params?: Record<string, unknown>
  field?: string
  data?: T
}

export type Page<T> = {
  page: number
  page_size: number
  total: number
  items: T[]
}

export type SupplierRef = { id: number; username: string }

export type ChannelPushTaskType = 'once' | 'periodic'

export type ChannelPushTask = {
  id: number
  site_id: number
  site_name: string
  site_deleted: boolean
  suppliers: SupplierRef[]
  task_type: ChannelPushTaskType
  interval_minutes: number
  target_group: string
  enabled: boolean
  next_run_at: number
  last_finished_at: number
  created_time: number
}

export type ChannelPushTaskPayload = {
  site_id: number
  supplier_user_ids: number[]
  task_type: ChannelPushTaskType
  interval_minutes: number
  target_group: string
}

export type ServerMessage = { key: string; params?: Record<string, unknown> }

export type ChannelPushRunStatus = 'running' | 'success' | 'partial' | 'failed'

export type ChannelPushRun = {
  id: number
  task_id: number
  site_id: number
  site_name: string
  target_group: string
  suppliers: SupplierRef[]
  status: ChannelPushRunStatus
  error: ServerMessage | null
  started_at: number
  finished_at: number
  site_channel_count: number
  target_channel_count: number
  unchanged_count: number
  enable_count: number
  disable_count: number
  update_models_count: number
  create_count: number
  orphan_count: number
  auto_disabled_count: number
  unsupported_count: number
  failed_action_count: number
}

export type ChannelPushCategory =
  | 'unchanged'
  | 'enable'
  | 'disable'
  | 'update_models'
  | 'create'
  | 'orphan'
  | 'auto_disabled'
  | 'unsupported'

export type SiteChannel = {
  id: number
  name: string
  used_quota: number
  status: number
  models: string
}

export type SiteSnapshot = { fetched_at: number; channels: SiteChannel[] }

export type TargetChannel = {
  id: number
  name: string
  type: number
  supplier_user_id: number
  supplier_username: string
  status: number
  models: string
}

export type ChannelPushAction = {
  category: ChannelPushCategory
  local_channel_id: number
  local_channel_name: string
  remote_channel_id: number
  remote_channel_name: string
  status_from: number
  status_to: number
  models_added: string[] | null
  models_removed: string[] | null
  models: string
  remote_status: number
  remote_models: string
  reason?: ServerMessage
  duplicate: boolean
  result: 'pending' | 'success' | 'failed' | 'skipped'
  error?: ServerMessage
}

export type ChannelPushOptions = {
  sites: { id: number; name: string }[]
  suppliers: { id: number; username: string; display_name: string }[]
}

export type RunDetailKind = 'site' | 'target' | 'actions'

export type ChannelPushDialogType = 'create' | 'update'
