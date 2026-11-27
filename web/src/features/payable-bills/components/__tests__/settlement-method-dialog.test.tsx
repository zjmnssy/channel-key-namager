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
import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { toast } from 'sonner'
import { afterEach, describe, expect, it, vi } from 'vitest'

import * as billApi from '../../api'
import { PAYABLE_BILL_QUERY_KEYS } from '../../constants'
import type {
  ApiResponse,
  PayableSettlement,
  PayableWholeSite,
} from '../../types'
import { SettlementMethodDialog } from '../settlement-method-dialog'

vi.mock('../../api')
vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

const settlement: PayableSettlement = {
  provider_id: 6,
  username: 'cpsupplier3',
  disabled: false,
  payable_discount: 0.9,
  method: 'channel',
  updated_time: 0,
  whole_sites: [{ site_id: 3, site_name: 'Site A', conflict: false }],
}

const saved: ApiResponse<unknown> = {
  success: true,
  data: { provider_id: 6, method: 'usage', updated_time: 1764145211 },
}

const NO_EFFECT_WARNING =
  'This provider has no site that can be settled by usage details yet, so saving has no effect for now'

const clients: QueryClient[] = []

async function renderDialog(row: PayableSettlement = settlement) {
  const i18n = createInstance()
  await i18n.init({ lng: 'en', resources: {}, initAsync: false })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  const onOpenChange = vi.fn()
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <SettlementMethodDialog
          settlement={row}
          open
          onOpenChange={onOpenChange}
        />
      </QueryClientProvider>
    </I18nextProvider>
  )
  return { user: userEvent.setup(), onOpenChange, client }
}

afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  vi.clearAllMocks()
  vi.restoreAllMocks()
})

describe('SettlementMethodDialog', () => {
  it('describes how each method is calculated', async () => {
    await renderDialog()

    expect(
      screen.getByRole('radio', { name: 'Channel details settlement' })
    ).toHaveAccessibleDescription(
      'Calculated from the channel details pulled every day'
    )
    expect(
      screen.getByRole('radio', { name: 'Usage details settlement' })
    ).toHaveAccessibleDescription(
      'Fully assigned sites are calculated from the amount before discount of their daily bills; channels on other sites are still calculated from channel details'
    )
  })

  it('keeps Save disabled until another method is chosen', async () => {
    const { user } = await renderDialog()
    const save = screen.getByRole('button', { name: 'Save' })
    expect(
      screen.getByRole('radio', { name: 'Channel details settlement' })
    ).toBeChecked()
    expect(save).toBeDisabled()

    await user.click(
      screen.getByRole('radio', { name: 'Usage details settlement' })
    )
    expect(save).toBeEnabled()

    await user.click(
      screen.getByRole('radio', { name: 'Channel details settlement' })
    )
    expect(save).toBeDisabled()
  })

  it.each<[string, PayableWholeSite[]]>([
    ['none is assigned', []],
    [
      'every one has a conflict',
      [{ site_id: 4, site_name: '本地测试', conflict: true }],
    ],
  ])(
    'warns that usage settlement has no effect when no fully assigned site can be settled: %s',
    async (_case, wholeSites) => {
      const { user } = await renderDialog({
        ...settlement,
        whole_sites: wholeSites,
      })
      expect(screen.queryByText(NO_EFFECT_WARNING)).not.toBeInTheDocument()

      await user.click(
        screen.getByRole('radio', { name: 'Usage details settlement' })
      )

      expect(screen.getByRole('alert')).toHaveTextContent(NO_EFFECT_WARNING)
      // The warning does not block saving.
      expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled()
    }
  )

  it('does not warn about usage settlement when a fully assigned site can be settled', async () => {
    const { user } = await renderDialog({
      ...settlement,
      whole_sites: [
        { site_id: 3, site_name: 'Site A', conflict: false },
        { site_id: 4, site_name: '本地测试', conflict: true },
      ],
    })

    await user.click(
      screen.getByRole('radio', { name: 'Usage details settlement' })
    )

    expect(
      screen.getByRole('radio', { name: 'Usage details settlement' })
    ).toBeChecked()
    expect(screen.queryByText(NO_EFFECT_WARNING)).not.toBeInTheDocument()
  })

  it('saves the chosen method, refreshes payable bill queries and closes', async () => {
    vi.mocked(billApi.setPayableSettlementMethod).mockResolvedValue(saved)
    const { user, onOpenChange, client } = await renderDialog()
    // Pages cached by this tab, the provider bills tab and the details tab.
    const cachedKeys = [
      PAYABLE_BILL_QUERY_KEYS.settlements({
        p: 1,
        page_size: 10,
        provider_ids: [],
        methods: [],
      }),
      PAYABLE_BILL_QUERY_KEYS.providers({
        p: 1,
        page_size: 10,
        start_date: 20261101,
        end_date: 20261126,
      }),
      PAYABLE_BILL_QUERY_KEYS.usages({
        p: 1,
        page_size: 10,
        start_date: 20261101,
        end_date: 20261126,
      }),
    ]
    for (const key of cachedKeys) {
      client.setQueryData(key, { items: [], total: 0 })
    }

    await user.click(
      screen.getByRole('radio', { name: 'Usage details settlement' })
    )
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(billApi.setPayableSettlementMethod).toHaveBeenCalledWith(6, 'usage')
    for (const key of cachedKeys) {
      expect(client.getQueryState(key)?.isInvalidated).toBe(true)
    }
    expect(toast.success).toHaveBeenCalledWith('Settlement method updated')
  })

  it('keeps the dialog open and Save enabled after a failed save', async () => {
    vi.mocked(billApi.setPayableSettlementMethod).mockResolvedValue({
      success: false,
      message: 'Database error, please contact the administrator',
    })
    const { user, onOpenChange } = await renderDialog()

    await user.click(
      screen.getByRole('radio', { name: 'Usage details settlement' })
    )
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith(
        'Database error, please contact the administrator'
      )
    )
    expect(await screen.findByRole('button', { name: 'Save' })).toBeEnabled()
    expect(onOpenChange).not.toHaveBeenCalled()
    expect(
      screen.getByRole('dialog', {
        name: 'Change settlement method: cpsupplier3',
      })
    ).toBeVisible()
  })

  it('shows Saving... and cannot be closed while saving', async () => {
    let finish: (value: ApiResponse<unknown>) => void = () => {}
    vi.mocked(billApi.setPayableSettlementMethod).mockReturnValue(
      new Promise((resolve) => {
        finish = resolve
      })
    )
    const { user, onOpenChange } = await renderDialog()

    await user.click(
      screen.getByRole('radio', { name: 'Usage details settlement' })
    )
    await user.click(screen.getByRole('button', { name: 'Save' }))
    expect(
      await screen.findByRole('button', { name: 'Saving...' })
    ).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled()
    await user.keyboard('{Escape}')

    expect(onOpenChange).not.toHaveBeenCalled()
    await act(async () => finish(saved))
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
  })
})
