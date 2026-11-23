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
import { afterEach, describe, expect, it, vi } from 'vitest'

import { downloadBlob, receivableBillFileName } from '../export'

const t = ((key: string) => key) as unknown as TFunction

describe('receivableBillFileName', () => {
  it('uses the site name and replaces characters invalid in file names', () => {
    expect(receivableBillFileName(['A/B:C'], 20261101, 20261123, t)).toBe(
      'A_B_C-Receivable Bills-20261101-20261123.xlsx'
    )
  })

  it('replaces backslashes, quotes and wildcards in the site name', () => {
    expect(
      receivableBillFileName(['A\\B*"C?<D>|E'], 20261101, 20261123, t)
    ).toBe('A_B__C__D__E-Receivable Bills-20261101-20261123.xlsx')
  })

  it('uses "All Sites" when no site is selected', () => {
    expect(receivableBillFileName([], 20261101, 20261123, t)).toBe(
      'All Sites-Receivable Bills-20261101-20261123.xlsx'
    )
  })

  it('uses "Multiple Sites" when several sites are selected', () => {
    expect(receivableBillFileName(['A', 'B'], 20261101, 20261123, t)).toBe(
      'Multiple Sites-Receivable Bills-20261101-20261123.xlsx'
    )
  })
})

describe('downloadBlob', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('clicks an attached anchor and revokes the URL only after a delay', () => {
    vi.useFakeTimers()
    const createObjectURL = vi.fn(() => 'blob:bills')
    const revokeObjectURL = vi.fn()
    Object.assign(URL, { createObjectURL, revokeObjectURL })
    const attachedOnClick: boolean[] = []
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
      this: HTMLAnchorElement
    ) {
      attachedOnClick.push(document.body.contains(this))
    })

    downloadBlob(new Blob(['x']), 'a.xlsx')

    expect(attachedOnClick).toEqual([true])
    expect(document.body.querySelector('a[download]')).toBeNull()
    expect(revokeObjectURL).not.toHaveBeenCalled()
    vi.runAllTimers()
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:bills')
  })
})
