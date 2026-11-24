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
  beijingToday,
  beijingYesterday,
  billDateToLocalDate,
  formatBillDate,
  localDateToBillDate,
  monthStart,
} from '../bill-date'

describe('bill-date', () => {
  it('beijingToday rolls to the next day at 16:00 UTC', () => {
    expect(beijingToday(new Date('2026-11-22T16:30:00Z'))).toBe(20261123)
    expect(beijingToday(new Date('2026-11-22T15:59:59Z'))).toBe(20261122)
  })

  it('beijingYesterday rolls back across a month boundary', () => {
    // 2026-12-01 00:30 in Beijing.
    expect(beijingYesterday(new Date('2026-11-30T16:30:00Z'))).toBe(20261130)
  })

  it('monthStart returns the first day of the month', () => {
    expect(monthStart(20261123)).toBe(20261101)
  })

  it('formatBillDate pads month and day', () => {
    expect(formatBillDate(20261105)).toBe('2026-11-05')
  })

  it('round-trips through the local calendar date', () => {
    expect(localDateToBillDate(billDateToLocalDate(20260301))).toBe(20260301)
  })
})
