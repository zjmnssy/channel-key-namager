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
import { z } from 'zod'

import type { ChannelPushTask, ChannelPushTaskPayload } from '../types'

export type TaskFormValues = {
  siteId: string
  supplierIds: string[]
  taskType: 'once' | 'periodic'
  intervalMinutes: string
  targetGroup: string
}

export const TASK_FORM_DEFAULTS: TaskFormValues = {
  siteId: '',
  supplierIds: [],
  taskType: 'once',
  intervalMinutes: '30',
  targetGroup: '',
}

const INTERVAL_MIN = 5
const INTERVAL_MAX = 1440

// Messages are English source keys; FormMessage translates them.
export const taskFormSchema: z.ZodType<TaskFormValues, TaskFormValues> = z
  .object({
    siteId: z.string().min(1, 'Site is required'),
    supplierIds: z.array(z.string()).min(1, 'Select at least one supplier'),
    taskType: z.enum(['once', 'periodic']),
    intervalMinutes: z.string(),
    targetGroup: z.string().trim().min(1, 'Target group is required'),
  })
  .superRefine((values, ctx) => {
    if (values.taskType !== 'periodic') return
    const raw = values.intervalMinutes.trim()
    const minutes = Number(raw)
    if (
      !/^\d+$/.test(raw) ||
      minutes < INTERVAL_MIN ||
      minutes > INTERVAL_MAX
    ) {
      ctx.addIssue({
        code: 'custom',
        path: ['intervalMinutes'],
        message: 'Interval must be an integer from 5 to 1440 minutes',
      })
    }
  })

export function toTaskPayload(values: TaskFormValues): ChannelPushTaskPayload {
  return {
    site_id: Number(values.siteId),
    supplier_user_ids: values.supplierIds.map(Number),
    task_type: values.taskType,
    interval_minutes:
      values.taskType === 'periodic'
        ? Number(values.intervalMinutes.trim())
        : 0,
    target_group: values.targetGroup.trim(),
  }
}

export function taskToFormValues(task: ChannelPushTask): TaskFormValues {
  return {
    siteId: String(task.site_id),
    supplierIds: task.suppliers.map((supplier) => String(supplier.id)),
    taskType: task.task_type,
    intervalMinutes:
      task.task_type === 'periodic'
        ? String(task.interval_minutes)
        : TASK_FORM_DEFAULTS.intervalMinutes,
    targetGroup: task.target_group,
  }
}
