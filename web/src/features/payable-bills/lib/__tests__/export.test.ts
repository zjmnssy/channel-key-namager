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
import { createInstance, type TFunction } from 'i18next'
import { beforeAll, describe, expect, it } from 'vitest'

import zh from '@/i18n/locales/zh.json'

import { channelUsageFileName, payableBillFileName } from '../export'

let t: TFunction

beforeAll(async () => {
  const i18n = createInstance()
  await i18n.init({ lng: 'zh', resources: { zh }, initAsync: false })
  t = i18n.t
})

describe('payable bill export file names', () => {
  it('names the usage export after the date range', () => {
    expect(channelUsageFileName(20261101, 20261123, t)).toBe(
      '渠道消费明细-20261101-20261123.xlsx'
    )
  })

  it('names the provider bill export after the date range', () => {
    expect(payableBillFileName(20261101, 20261123, t)).toBe(
      '应付账单-20261101-20261123.xlsx'
    )
  })
})
