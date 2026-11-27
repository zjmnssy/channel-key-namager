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
import type { TFunction } from 'i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  earliestBillStartDate,
  getThirdPartySiteFormSchema,
  type ThirdPartySiteFormValues,
} from '../third-party-site-form'

const t = ((key: string) => key) as unknown as TFunction

const values: ThirdPartySiteFormValues = {
  name: 'Alpha',
  type: 'newapi',
  base_url: 'https://alpha.example.com',
  remote_user_id: '7',
  api_key: 'remote-access-token',
  receivable_discount: '1',
  bill_start_date: 20261126,
}

function startDateError(
  billStartDate: number,
  storedBillStartDate?: number
): string | undefined {
  const schema = getThirdPartySiteFormSchema(
    t,
    storedBillStartDate === undefined ? 'create' : 'update',
    storedBillStartDate
  )
  const result = schema.safeParse({
    ...values,
    bill_start_date: billStartDate,
  })
  return result.error?.issues.find(
    (issue) => issue.path[0] === 'bill_start_date'
  )?.message
}

describe('earliestBillStartDate', () => {
  it.each([
    [20261126, 20251126],
    [20260101, 20250101],
    [20280229, 20270301],
  ])('lets bills on %i start back on %i', (today, earliest) => {
    expect(earliestBillStartDate(today)).toBe(earliest)
  })
})

describe('bill start date validation', () => {
  beforeEach(() => {
    // Beijing 2026-11-26 12:00
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-11-26T04:00:00Z'))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it.each([
    [20261126, undefined],
    [20251126, undefined],
    [20261127, 'Bill start date cannot be later than today'],
    [20251125, 'Bill start date cannot be earlier than one year ago'],
  ])('judges a new start date of %i', (billStartDate, message) => {
    expect(startDateError(billStartDate)).toBe(message)
  })

  it('rejects moving a stored start date older than a year to another old day', () => {
    expect(startDateError(20250102, 20250101)).toBe(
      'Bill start date cannot be earlier than one year ago'
    )
  })
})
