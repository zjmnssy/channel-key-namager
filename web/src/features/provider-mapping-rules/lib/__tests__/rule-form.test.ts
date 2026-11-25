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
import { describe, expect, it } from 'vitest'

import {
  applyDaySpan,
  ruleFormSchema,
  toRuleCreatePayload,
  toRuleUpdatePayload,
  type RuleFormValues,
} from '../rule-form'

const valid: RuleFormValues = {
  siteId: '3',
  ruleType: 'all',
  channelId: '',
  channelName: '',
  providerId: '5',
  applyEnabled: false,
  applyStart: 20261101,
  applyEnd: 20261124,
}

function issuesOf(values: RuleFormValues) {
  const result = ruleFormSchema.safeParse(values)
  if (result.success) return {}
  return Object.fromEntries(
    result.error.issues.map((issue) => [issue.path.join('.'), issue.message])
  )
}

describe('applyDaySpan', () => {
  it('counts a single day as 1', () => {
    expect(applyDaySpan(20261101, 20261101)).toBe(1)
  })

  it('counts both ends across months', () => {
    expect(applyDaySpan(20260901, 20261201)).toBe(92)
    expect(applyDaySpan(20260901, 20261202)).toBe(93)
  })
})

describe('ruleFormSchema', () => {
  it('reports the missing site, site channel and provider with their own messages', () => {
    expect(
      issuesOf({ ...valid, siteId: '', ruleType: 'channel', providerId: '' })
    ).toEqual({
      siteId: 'Site is required',
      channelId: 'Site channel is required',
      providerId: 'Provider is required',
    })
  })

  it('requires a site channel only for a specified-channel rule', () => {
    expect(issuesOf({ ...valid, ruleType: 'all' })).toEqual({})
    expect(issuesOf({ ...valid, ruleType: 'unresolved' })).toEqual({})
    expect(issuesOf({ ...valid, ruleType: 'channel' })).toEqual({
      channelId: 'Site channel is required',
    })
    expect(
      issuesOf({ ...valid, ruleType: 'channel', channelId: '18704' })
    ).toEqual({})
  })

  it('accepts a backfill of 92 days and rejects 93 days', () => {
    const backfill = { ...valid, applyEnabled: true, applyStart: 20260901 }

    expect(issuesOf({ ...backfill, applyEnd: 20261201 })).toEqual({})
    expect(issuesOf({ ...backfill, applyEnd: 20261202 })).toEqual({
      applyStart: 'Date range cannot exceed 92 days',
    })
  })

  it('rejects a backfill whose start date is after its end date', () => {
    expect(
      issuesOf({
        ...valid,
        applyEnabled: true,
        applyStart: 20261124,
        applyEnd: 20261101,
      })
    ).toEqual({ applyStart: 'Start date must not be after end date' })
  })

  it('ignores the dates when backfill is off', () => {
    expect(
      issuesOf({ ...valid, applyStart: 20260101, applyEnd: 20261124 })
    ).toEqual({})
    expect(
      issuesOf({ ...valid, applyStart: 20261124, applyEnd: 20261101 })
    ).toEqual({})
  })
})

describe('toRuleCreatePayload', () => {
  it('sends a site-level rule without a channel or backfill', () => {
    expect(toRuleCreatePayload(valid)).toStrictEqual({
      site_id: 3,
      rule_type: 'all',
      provider_id: 5,
    })
  })

  it('sends the channel and the backfill dates of a specified-channel rule', () => {
    expect(
      toRuleCreatePayload({
        ...valid,
        ruleType: 'channel',
        channelId: '18704',
        applyEnabled: true,
      })
    ).toStrictEqual({
      site_id: 3,
      rule_type: 'channel',
      remote_channel_id: 18704,
      provider_id: 5,
      apply: { start_date: 20261101, end_date: 20261124 },
    })
  })

  it('leaves out a channel kept in the form of a site-level rule', () => {
    expect(
      toRuleCreatePayload({ ...valid, ruleType: 'unresolved', channelId: '9' })
    ).toStrictEqual({ site_id: 3, rule_type: 'unresolved', provider_id: 5 })
  })
})

describe('toRuleUpdatePayload', () => {
  it('sends only the provider when backfill is off', () => {
    expect(
      toRuleUpdatePayload({ ...valid, ruleType: 'channel', channelId: '18704' })
    ).toStrictEqual({ provider_id: 5 })
  })

  it('adds the backfill dates when backfill is on', () => {
    expect(toRuleUpdatePayload({ ...valid, applyEnabled: true })).toStrictEqual(
      {
        provider_id: 5,
        apply: { start_date: 20261101, end_date: 20261124 },
      }
    )
  })
})
