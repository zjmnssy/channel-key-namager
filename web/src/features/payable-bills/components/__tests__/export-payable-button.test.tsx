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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import * as billApi from '../../api'
import type { PayableBillFilter } from '../../types'
import { ExportPayableButton } from '../export-payable-button'

vi.mock('../../api')

const filter: PayableBillFilter = {
  start_date: 20261101,
  end_date: 20261123,
  provider_ids: [0],
}

let downloads: string[] = []

async function renderButton(kind: 'usages' | 'providers') {
  const i18n = createInstance()
  await i18n.init({ lng: 'en', resources: {}, initAsync: false })
  const client = new QueryClient()
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <ExportPayableButton kind={kind} filter={filter} />
      </QueryClientProvider>
    </I18nextProvider>
  )
  return userEvent.setup()
}

beforeEach(() => {
  downloads = []
  Object.assign(URL, {
    createObjectURL: vi.fn(() => 'blob:payable'),
    revokeObjectURL: vi.fn(),
  })
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
    this: HTMLAnchorElement
  ) {
    downloads.push(this.download)
  })
})

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  vi.restoreAllMocks()
})

describe('ExportPayableButton', () => {
  it('downloads the usage details of the filter', async () => {
    vi.mocked(billApi.exportPayableUsages).mockResolvedValue(new Blob(['x']))
    const user = await renderButton('usages')

    await user.click(screen.getByRole('button', { name: 'Export Excel' }))

    await waitFor(() =>
      expect(downloads).toEqual([
        'Channel Usage Details-20261101-20261123.xlsx',
      ])
    )
    expect(billApi.exportPayableUsages).toHaveBeenCalledWith(filter)
    expect(billApi.exportPayableProviderBills).not.toHaveBeenCalled()
  })

  it('downloads the provider bills of the filter', async () => {
    vi.mocked(billApi.exportPayableProviderBills).mockResolvedValue(
      new Blob(['x'])
    )
    const user = await renderButton('providers')

    await user.click(screen.getByRole('button', { name: 'Export Excel' }))

    await waitFor(() =>
      expect(downloads).toEqual(['Payable Bills-20261101-20261123.xlsx'])
    )
    expect(billApi.exportPayableProviderBills).toHaveBeenCalledWith(filter)
    expect(billApi.exportPayableUsages).not.toHaveBeenCalled()
  })
})
