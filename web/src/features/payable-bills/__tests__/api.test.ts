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
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import {
  exportPayableProviderBills,
  exportPayableUsages,
  listPayableSettlementMethods,
  setPayableSettlementMethod,
} from '../api'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), put: vi.fn() } }))

const file = new Blob(['xlsx'], {
  type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
})

describe('payable bill exports', () => {
  beforeEach(() => {
    vi.mocked(api.get).mockReset()
    vi.mocked(api.get).mockResolvedValue({ data: file })
  })

  it('exports usages with the filter lists joined by commas', async () => {
    await expect(
      exportPayableUsages({
        start_date: 20261101,
        end_date: 20261123,
        site_ids: [1, 2],
        provider_ids: [0, 7],
      })
    ).resolves.toBe(file)

    expect(api.get).toHaveBeenCalledWith(
      '/api/payable_bill/usages/export',
      expect.objectContaining({
        params: {
          start_date: 20261101,
          end_date: 20261123,
          site_ids: '1,2',
          provider_ids: '0,7',
        },
        responseType: 'blob',
      })
    )
  })

  it('exports provider bills without the empty filter lists', async () => {
    await expect(
      exportPayableProviderBills({
        start_date: 20261101,
        end_date: 20261123,
        site_ids: [],
      })
    ).resolves.toBe(file)

    expect(api.get).toHaveBeenCalledWith(
      '/api/payable_bill/providers/export',
      expect.objectContaining({
        params: {
          start_date: 20261101,
          end_date: 20261123,
          site_ids: undefined,
          provider_ids: undefined,
        },
        responseType: 'blob',
      })
    )
  })
})

describe('payable settlement methods', () => {
  beforeEach(() => {
    vi.mocked(api.get).mockReset()
    vi.mocked(api.put).mockReset()
  })

  it('lists settlement methods with the filter lists joined by commas', async () => {
    const page = {
      success: true,
      data: { page: 2, page_size: 20, total: 0, items: [] },
    }
    vi.mocked(api.get).mockResolvedValue({ data: page })

    await expect(
      listPayableSettlementMethods({
        p: 2,
        page_size: 20,
        provider_ids: [5, 6],
        methods: ['usage'],
      })
    ).resolves.toBe(page)

    expect(api.get).toHaveBeenCalledWith(
      '/api/payable_bill/settlement_methods',
      {
        params: { p: 2, page_size: 20, provider_ids: '5,6', methods: 'usage' },
      }
    )
  })

  it('sends the settlement method in the request body', async () => {
    const response = {
      success: true,
      data: { provider_id: 6, method: 'usage', updated_time: 1764145211 },
    }
    vi.mocked(api.put).mockResolvedValue({ data: response })

    await expect(setPayableSettlementMethod(6, 'usage')).resolves.toBe(response)

    expect(api.put).toHaveBeenCalledWith(
      '/api/payable_bill/settlement_methods/6',
      { method: 'usage' }
    )
  })
})
