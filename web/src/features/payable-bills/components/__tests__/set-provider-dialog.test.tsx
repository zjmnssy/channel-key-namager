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
import { toast } from 'sonner'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import * as billApi from '../../api'
import type { PayableUsage } from '../../types'
import { SetProviderDialog } from '../set-provider-dialog'

vi.mock('../../api')
vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

const usage: PayableUsage = {
  id: 101,
  task_id: 12,
  pull_site_id: 31,
  bill_date: 20261123,
  site_id: 3,
  site_name: 'Site A',
  site_base_url: 'https://a.example.com',
  remote_channel_id: 17,
  remote_channel_name: 'manual-channel',
  used_quota: 5000000,
  delta_quota: null,
  delta_flag: 'first_sync',
  quota_per_unit: 500000,
  provider_id: 0,
  provider_name: '',
  provider_source: 'unknown',
  payable_discount: null,
  synced_at: 1763914205,
  created_time: 1763914205,
  updated_time: 1763914205,
  counted: true,
  manual: false,
  delta_amount: null,
  used_amount: '10.000000',
}

const clients: QueryClient[] = []

async function renderDialog(row: PayableUsage = usage) {
  const i18n = createInstance()
  await i18n.init({ lng: 'en', resources: {}, initAsync: false })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  const invalidate = vi.spyOn(client, 'invalidateQueries')
  const onOpenChange = vi.fn()
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <SetProviderDialog usage={row} open onOpenChange={onOpenChange} />
      </QueryClientProvider>
    </I18nextProvider>
  )
  return { user: userEvent.setup(), onOpenChange, invalidate }
}

beforeEach(() => {
  vi.mocked(billApi.getPayableBillOptions).mockResolvedValue({
    success: true,
    data: {
      sites: [],
      providers: [
        { id: 5, username: 'alice', disabled: false, payable_discount: 0.8 },
        { id: 6, username: 'bob', disabled: true, payable_discount: null },
      ],
    },
  })
})

afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  vi.clearAllMocks()
  vi.restoreAllMocks()
})

describe('SetProviderDialog', () => {
  it('lists suppliers with disabled ones labelled and without the unknown provider', async () => {
    const { user } = await renderDialog()

    await user.click(screen.getByRole('combobox', { name: 'Provider' }))

    expect(
      await screen.findByRole('option', { name: 'bob (disabled)' })
    ).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'alice' })).toBeInTheDocument()
    expect(
      screen.queryByRole('option', { name: 'Unknown provider' })
    ).not.toBeInTheDocument()
  })

  it('submits the chosen supplier and closes', async () => {
    vi.mocked(billApi.setPayableUsageProvider).mockResolvedValue({
      success: true,
      data: { ...usage, provider_id: 5, provider_source: 'manual' },
    })
    const { user, onOpenChange, invalidate } = await renderDialog()

    await user.click(screen.getByRole('combobox', { name: 'Provider' }))
    await user.click(await screen.findByRole('option', { name: 'alice' }))
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(billApi.setPayableUsageProvider).toHaveBeenCalledWith(101, 5)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['payable-bills'] })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: ['provider-mapping-rules'],
    })
    expect(toast.success).toHaveBeenCalledWith('Provider updated successfully')
  })

  it('hides the clear action when no manual setting exists, even for a manual provider', async () => {
    await renderDialog({ ...usage, provider_source: 'manual', manual: false })

    expect(
      screen.queryByRole('button', { name: 'Clear Manual Setting' })
    ).not.toBeInTheDocument()
  })

  it('shows the clear action only for a manual row and submits 0', async () => {
    vi.mocked(billApi.setPayableUsageProvider).mockResolvedValue({
      success: true,
      data: { ...usage, manual: false },
    })
    // The override exists but the row was resolved from the channel name.
    const { user, onOpenChange } = await renderDialog({
      ...usage,
      provider_id: 5,
      provider_name: 'alice',
      provider_source: 'parsed',
      manual: true,
    })

    await user.click(
      screen.getByRole('button', { name: 'Clear Manual Setting' })
    )

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(billApi.setPayableUsageProvider).toHaveBeenCalledWith(101, 0)
    expect(toast.success).toHaveBeenCalledWith('Manual setting cleared')
  })

  it('keeps the dialog open and shows the server message on failure', async () => {
    vi.mocked(billApi.setPayableUsageProvider).mockResolvedValue({
      success: false,
      message: 'The selected user is not a channel supplier',
    })
    const { user, onOpenChange } = await renderDialog()

    await user.click(screen.getByRole('combobox', { name: 'Provider' }))
    await user.click(
      await screen.findByRole('option', { name: 'bob (disabled)' })
    )
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith(
        'The selected user is not a channel supplier'
      )
    )
    expect(billApi.setPayableUsageProvider).toHaveBeenCalledWith(101, 6)
    expect(onOpenChange).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog', { name: 'Set Provider' })).toBeVisible()
  })
})
