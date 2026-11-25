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
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { BillDateRangePicker } from '../bill-date-range-picker'

async function renderPicker(
  onChange: (start: number, end: number) => void,
  disabled?: boolean
) {
  const i18n = createInstance()
  await i18n.init({ lng: 'en', resources: {}, initAsync: false })
  render(
    <I18nextProvider i18n={i18n}>
      <BillDateRangePicker
        start={20261101}
        end={20261110}
        max={20261123}
        onChange={onChange}
        disabled={disabled}
      />
    </I18nextProvider>
  )
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: /2026-11-01/ }))
  return user
}

const day = (label: string) =>
  screen.findByRole('button', { name: new RegExp(label) })

afterEach(cleanup)

describe('BillDateRangePicker', () => {
  it('does not commit on the first click and commits on the second', async () => {
    const onChange = vi.fn()
    const user = await renderPicker(onChange)

    await user.click(await day('November 15th, 2026'))
    expect(onChange).not.toHaveBeenCalled()

    await user.click(await day('November 20th, 2026'))
    expect(onChange).toHaveBeenCalledExactlyOnceWith(20261115, 20261120)
  })

  it('orders the range when the second click is before the first', async () => {
    const onChange = vi.fn()
    const user = await renderPicker(onChange)

    await user.click(await day('November 20th, 2026'))
    await user.click(await day('November 15th, 2026'))

    expect(onChange).toHaveBeenCalledExactlyOnceWith(20261115, 20261120)
  })

  it('does not allow picking a day after the max date', async () => {
    const onChange = vi.fn()
    const user = await renderPicker(onChange)

    await user.click(await day('November 15th, 2026'))

    expect(await day('November 24th, 2026')).toBeDisabled()
  })

  it('keeps the calendar closed while disabled', async () => {
    await renderPicker(vi.fn(), true)

    expect(screen.getByRole('button', { name: /2026-11-01/ })).toBeDisabled()
    expect(
      screen.queryByRole('button', { name: /November 15th, 2026/ })
    ).not.toBeInTheDocument()
  })
})
