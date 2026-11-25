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

import {
  beijingYesterday,
  billDateToLocalDate,
  monthStart,
} from '@/features/receivable-bills/lib/bill-date'

import type {
  ProviderMappingCreateBody,
  ProviderMappingPreviewQuery,
  ProviderMappingRule,
  ProviderMappingRuleType,
  ProviderMappingUpdateBody,
} from '../types'

export type RuleFormValues = {
  siteId: string
  ruleType: ProviderMappingRuleType
  channelId: string // remote channel ID; only a specified-channel rule has one
  providerId: string
  applyEnabled: boolean // also change the details already pulled
  applyStart: number // YYYYMMDD
  applyEnd: number
}

/** The longest backfill the server accepts, both ends included. */
export const RULE_APPLY_MAX_DAYS = 92

const DAY_MS = 24 * 60 * 60 * 1000

/** Days from start to end (YYYYMMDD), both ends included. */
export function applyDaySpan(start: number, end: number): number {
  const ms =
    billDateToLocalDate(end).getTime() - billDateToLocalDate(start).getTime()
  // Local midnights are 23 or 25 hours apart across a daylight saving change.
  return Math.round(ms / DAY_MS) + 1
}

/** A new rule covers all channels and changes no pulled details. */
export function createRuleFormDefaults(): RuleFormValues {
  // A backfill starts as this Beijing month up to yesterday, the latest bill.
  const yesterday = beijingYesterday()
  return {
    siteId: '',
    ruleType: 'all',
    channelId: '',
    providerId: '',
    applyEnabled: false,
    applyStart: monthStart(yesterday),
    applyEnd: yesterday,
  }
}

/** A provider that is no longer a channel supplier must be chosen again. */
export function ruleToFormValues(rule: ProviderMappingRule): RuleFormValues {
  return {
    ...createRuleFormDefaults(),
    siteId: String(rule.site_id),
    ruleType: rule.rule_type,
    channelId:
      rule.rule_type === 'channel' ? String(rule.remote_channel_id) : '',
    providerId: rule.provider_valid ? String(rule.provider_id) : '',
  }
}

// Messages are English source keys; FormMessage translates them.
export const ruleFormSchema: z.ZodType<RuleFormValues, RuleFormValues> = z
  .object({
    siteId: z.string().min(1, 'Site is required'),
    ruleType: z.enum(['all', 'unresolved', 'channel']),
    channelId: z.string(),
    providerId: z.string().min(1, 'Provider is required'),
    applyEnabled: z.boolean(),
    applyStart: z.number(),
    applyEnd: z.number(),
  })
  .superRefine((values, ctx) => {
    if (values.ruleType === 'channel' && values.channelId === '') {
      ctx.addIssue({
        code: 'custom',
        path: ['channelId'],
        message: 'Site channel is required',
      })
    }
    if (!values.applyEnabled) return
    // Both date errors are shown under the date range.
    if (values.applyStart > values.applyEnd) {
      ctx.addIssue({
        code: 'custom',
        path: ['applyStart'],
        message: 'Start date must not be after end date',
      })
      return
    }
    if (
      applyDaySpan(values.applyStart, values.applyEnd) > RULE_APPLY_MAX_DAYS
    ) {
      ctx.addIssue({
        code: 'custom',
        path: ['applyStart'],
        message: 'Date range cannot exceed 92 days',
      })
    }
  })

export function toRuleUpdatePayload(
  values: RuleFormValues
): ProviderMappingUpdateBody {
  const body: ProviderMappingUpdateBody = {
    provider_id: Number(values.providerId),
  }
  if (values.applyEnabled) {
    body.apply = { start_date: values.applyStart, end_date: values.applyEnd }
  }
  return body
}

export function toRuleCreatePayload(
  values: RuleFormValues
): ProviderMappingCreateBody {
  const body: ProviderMappingCreateBody = {
    site_id: Number(values.siteId),
    rule_type: values.ruleType,
    ...toRuleUpdatePayload(values),
  }
  // The server refuses a channel on a site-level rule.
  if (values.ruleType === 'channel') {
    body.remote_channel_id = Number(values.channelId)
  }
  return body
}

/** Counts the details that saving the same values would change. */
export function toRulePreviewQuery(
  values: RuleFormValues
): ProviderMappingPreviewQuery {
  return {
    site_id: Number(values.siteId),
    rule_type: values.ruleType,
    remote_channel_id:
      values.ruleType === 'channel' ? Number(values.channelId) : 0,
    provider_id: Number(values.providerId),
    start_date: values.applyStart,
    end_date: values.applyEnd,
  }
}
