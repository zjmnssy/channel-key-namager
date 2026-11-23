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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, test, vi } from 'vitest'

import { DataTableFacetedFilter } from '../toolbar/faceted-filter'

const options = [
  { label: 'Site A', value: '1' },
  { label: 'Site B', value: '2' },
]

async function openFilter() {
  await userEvent.click(screen.getByRole('button', { name: /Site A/ }))
}

test('reports added options through onValueChange without a column', async () => {
  const onValueChange = vi.fn()
  render(
    <DataTableFacetedFilter
      title='Site'
      options={options}
      value={['1']}
      onValueChange={onValueChange}
    />
  )

  await openFilter()
  await userEvent.click(await screen.findByRole('option', { name: /Site B/ }))

  expect(onValueChange).toHaveBeenCalledWith(['1', '2'])
})

test('reports undefined when the last option is cleared', async () => {
  const onValueChange = vi.fn()
  render(
    <DataTableFacetedFilter
      title='Site'
      options={options}
      value={['1']}
      onValueChange={onValueChange}
    />
  )

  await openFilter()
  await userEvent.click(await screen.findByRole('option', { name: /Site A/ }))
  expect(onValueChange).toHaveBeenLastCalledWith(undefined)

  onValueChange.mockClear()
  await userEvent.click(
    await screen.findByRole('option', { name: 'Clear filters' })
  )
  expect(onValueChange).toHaveBeenLastCalledWith(undefined)
})
