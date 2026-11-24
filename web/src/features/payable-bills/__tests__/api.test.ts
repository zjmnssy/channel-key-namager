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

import { exportPayableProviderBills, exportPayableUsages } from '../api'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn() } }))

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
