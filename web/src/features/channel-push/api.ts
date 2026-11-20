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

import type {
  ApiResponse,
  ChannelPushOptions,
  ChannelPushRun,
  ChannelPushTask,
  ChannelPushTaskPayload,
  Page,
  RunDetailKind,
} from './types'

export async function getChannelPushTasks(params: {
  p: number
  page_size: number
}): Promise<ApiResponse<Page<ChannelPushTask>>> {
  const res = await api.get('/api/channel_push/tasks', { params })
  return res.data
}

export async function createChannelPushTask(
  payload: ChannelPushTaskPayload
): Promise<ApiResponse<ChannelPushTask>> {
  const res = await api.post('/api/channel_push/tasks', payload)
  return res.data
}

export async function updateChannelPushTask(
  id: number,
  payload: ChannelPushTaskPayload
): Promise<ApiResponse<ChannelPushTask>> {
  const res = await api.put(`/api/channel_push/tasks/${id}`, payload)
  return res.data
}

export async function setChannelPushTaskEnabled(
  id: number,
  enabled: boolean
): Promise<ApiResponse<ChannelPushTask>> {
  const res = await api.post(`/api/channel_push/tasks/${id}/enabled`, {
    enabled,
  })
  return res.data
}

export async function getChannelPushRuns(params: {
  p: number
  page_size: number
  start_from?: number
  start_to?: number
  supplier_user_id?: number
  site_id?: number
}): Promise<ApiResponse<Page<ChannelPushRun>>> {
  const res = await api.get('/api/channel_push/runs', { params })
  return res.data
}

export async function getChannelPushRunDetail<T>(
  id: number,
  kind: RunDetailKind
): Promise<ApiResponse<T | null>> {
  const res = await api.get(`/api/channel_push/runs/${id}/${kind}`)
  return res.data
}

export async function getChannelPushSiteGroups(
  siteId: number
): Promise<ApiResponse<string[]>> {
  const res = await api.get(`/api/channel_push/sites/${siteId}/groups`)
  return res.data
}

export async function getChannelPushOptions(): Promise<
  ApiResponse<ChannelPushOptions>
> {
  const res = await api.get('/api/channel_push/options')
  return res.data
}
