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
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { toast } from 'sonner'
import { afterEach, describe, expect, it, vi } from 'vitest'

import * as billApi from '../../api'
import type { ReceivableBill } from '../../types'
import { ResyncBillButton } from '../resync-bill-button'

vi.mock('../../api')
vi.mock('sonner', () => ({
  toast: { success: vi.fn(), warning: vi.fn(), error: vi.fn() },
}))

const baseBill: ReceivableBill = {
  id: 7,
  site_id: 2,
  bill_date: 20261122,
  site_name: 'Site A',
  site_base_url: 'https://a.example.com',
  quota: 61728394,
  quota_per_unit: 500000,
  receivable_discount: 0.8,
  status: 'success',
  attempts: 0,
  synced_at: 1700000000,
  next_retry_at: 0,
  created_time: 1700000000,
  updated_time: 1700000000,
  amount: '123.456788',
  receivable_amount: '98.765430',
  error: null,
  site_deleted: false,
}

async function renderButton(bill: ReceivableBill) {
  const i18n = createInstance()
  await i18n.init({ lng: 'en', resources: {}, initAsync: false })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <ResyncBillButton bill={bill} />
      </QueryClientProvider>
    </I18nextProvider>
  )
  return userEvent.setup()
}

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe('ResyncBillButton', () => {
  it('asks for confirmation before resyncing a successful bill', async () => {
    vi.mocked(billApi.resyncReceivableBill).mockResolvedValue({
      success: true,
      data: baseBill,
    })
    const user = await renderButton(baseBill)

    await user.click(screen.getByRole('button', { name: 'Resync' }))

    expect(await screen.findByText(/123\.456788/)).toBeInTheDocument()
    expect(billApi.resyncReceivableBill).not.toHaveBeenCalled()

    await user.click(
      within(screen.getByRole('alertdialog')).getByRole('button', {
        name: 'Resync',
      })
    )

    await waitFor(() =>
      expect(billApi.resyncReceivableBill).toHaveBeenCalledTimes(1)
    )
    expect(billApi.resyncReceivableBill).toHaveBeenCalledWith(7)
    await waitFor(() =>
      expect(toast.success).toHaveBeenCalledWith('Bill synced')
    )
  })

  it('resyncs a failed bill without confirmation', async () => {
    vi.mocked(billApi.resyncReceivableBill).mockResolvedValue({
      success: true,
      data: { ...baseBill, status: 'retrying', amount: null },
    })
    const user = await renderButton({
      ...baseBill,
      status: 'failed',
      amount: null,
    })

    await user.click(screen.getByRole('button', { name: 'Resync' }))

    await waitFor(() =>
      expect(billApi.resyncReceivableBill).toHaveBeenCalledWith(7)
    )
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    await waitFor(() =>
      expect(toast.warning).toHaveBeenCalledWith(
        'Sync failed, will retry automatically'
      )
    )
  })

  it('reports a failed resync result as an error', async () => {
    vi.mocked(billApi.resyncReceivableBill).mockResolvedValue({
      success: true,
      data: { ...baseBill, status: 'failed', amount: null },
    })
    const user = await renderButton({ ...baseBill, status: 'failed' })

    await user.click(screen.getByRole('button', { name: 'Resync' }))

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Sync failed'))
  })

  it('disables resync for bills of deleted sites', async () => {
    await renderButton({ ...baseBill, site_deleted: true })

    expect(screen.getByRole('button', { name: 'Resync' })).toBeDisabled()
  })

  it('shows the server reason when the bill is being synced', async () => {
    vi.mocked(billApi.resyncReceivableBill).mockResolvedValue({
      success: false,
      message: 'This bill is being synced, please try again later',
    })
    const user = await renderButton({ ...baseBill, status: 'failed' })

    await user.click(screen.getByRole('button', { name: 'Resync' }))

    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith(
        'This bill is being synced, please try again later'
      )
    )
    expect(toast.error).toHaveBeenCalledTimes(1)
  })
})
