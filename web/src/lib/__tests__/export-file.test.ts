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

import { fetchExportFile, joinQueryList } from '../export-file'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn() } }))

describe('joinQueryList', () => {
  it('joins values with commas and leaves an empty list out', () => {
    expect(joinQueryList([1, 2])).toBe('1,2')
    expect(joinQueryList([])).toBeUndefined()
    expect(joinQueryList(undefined)).toBeUndefined()
  })
})

describe('fetchExportFile', () => {
  beforeEach(() => {
    vi.mocked(api.get).mockReset()
  })

  it('requests a blob in the interface language and reports failures itself', async () => {
    const file = new Blob(['xlsx'])
    vi.mocked(api.get).mockResolvedValue({ data: file })

    await expect(
      fetchExportFile('/api/x/export', { start_date: 1 }, 'Export failed')
    ).resolves.toBe(file)

    expect(api.get).toHaveBeenCalledWith('/api/x/export', {
      params: { start_date: 1 },
      headers: { 'Accept-Language': 'en' },
      responseType: 'blob',
      disableDuplicate: true,
      skipErrorHandler: true,
    })
  })

  it('throws the fallback message when a JSON body cannot be parsed', async () => {
    vi.mocked(api.get).mockResolvedValue({
      data: new Blob(['not json'], { type: 'application/json' }),
    })

    await expect(
      fetchExportFile('/api/x/export', {}, 'Export failed')
    ).rejects.toThrow('Export failed')
  })
})
