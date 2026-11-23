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

import { exportReceivableBills } from '../api'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn() } }))

const filter = { start_date: 20261101, end_date: 20261123 }

describe('exportReceivableBills', () => {
  beforeEach(() => {
    vi.mocked(api.get).mockReset()
  })

  it('returns the workbook when the server sends a file', async () => {
    const file = new Blob(['xlsx'], {
      type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    })
    vi.mocked(api.get).mockResolvedValue({ data: file })

    await expect(exportReceivableBills(filter)).resolves.toBe(file)
  })

  it('throws the server message when the body is a JSON error', async () => {
    const body = new Blob(
      [JSON.stringify({ success: false, message: 'Invalid parameters' })],
      { type: 'application/json' }
    )
    vi.mocked(api.get).mockResolvedValue({ data: body })

    await expect(exportReceivableBills(filter)).rejects.toThrow(
      'Invalid parameters'
    )
  })
})
