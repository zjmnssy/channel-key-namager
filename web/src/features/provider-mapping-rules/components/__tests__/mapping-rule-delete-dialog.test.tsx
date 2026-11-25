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

import { api } from '@/lib/api'

import type { ProviderMappingRule } from '../../types'
import { MappingRuleDeleteDialog } from '../mapping-rule-delete-dialog'

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

const rule: ProviderMappingRule = {
  id: 7,
  site_id: 3,
  site_name: 'Alpha',
  site_deleted: false,
  rule_type: 'channel',
  remote_channel_id: 18704,
  remote_channel_name: 'az-gpt-eastus2-22536',
  provider_id: 5,
  provider_name: 'alice',
  provider_valid: true,
  created_time: 1,
  updated_time: 1,
}

const clients: QueryClient[] = []

// Label and text of each entry the confirmation lists about the rule.
function ruleSummaryOf(dialog: HTMLElement) {
  return Object.fromEntries(
    within(dialog)
      .getAllByRole('term')
      .map((term) => [term.textContent, term.nextElementSibling?.textContent])
  )
}

async function renderDialog(currentRule: ProviderMappingRule = rule) {
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
        <MappingRuleDeleteDialog
          open
          onOpenChange={onOpenChange}
          rule={currentRule}
        />
      </QueryClientProvider>
    </I18nextProvider>
  )
  return { user: userEvent.setup(), onOpenChange, invalidate }
}

afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  vi.clearAllMocks()
  vi.restoreAllMocks()
})

describe('MappingRuleDeleteDialog', () => {
  it('explains that pulled details stay unchanged before deleting', async () => {
    await renderDialog()

    const dialog = screen.getByRole('alertdialog', {
      name: 'Delete Mapping Rule',
    })
    expect(dialog).toHaveTextContent('Delete the mapping rule of Alpha?')
    expect(dialog).toHaveTextContent(
      'The rule is no longer used from the next pull. Details already pulled stay unchanged.'
    )
  })

  it('names the site, type, channel and provider of a specified-channel rule', async () => {
    await renderDialog()

    const dialog = screen.getByRole('alertdialog', {
      name: 'Delete Mapping Rule',
    })
    expect(ruleSummaryOf(dialog)).toEqual({
      'Third-party site': 'Alpha',
      'Rule Type': 'Specified channel',
      'Site Channel': '18704 az-gpt-eastus2-22536',
      Provider: 'alice',
    })
  })

  it('names a site-level rule without a site channel and marks a provider that is no longer valid', async () => {
    await renderDialog({
      ...rule,
      rule_type: 'all',
      remote_channel_id: 0,
      remote_channel_name: '',
      provider_valid: false,
    })

    const dialog = screen.getByRole('alertdialog', {
      name: 'Delete Mapping Rule',
    })
    expect(Object.keys(ruleSummaryOf(dialog))).toEqual([
      'Third-party site',
      'Rule Type',
      'Provider',
    ])
    expect(ruleSummaryOf(dialog)['Rule Type']).toBe('All site channels')
    const provider = within(dialog).getByText('Provider').nextElementSibling
    expect(provider).toHaveTextContent('alice')
    expect(provider).toHaveTextContent('No longer valid')
  })

  it('deletes the rule after confirming, then refreshes the lists and closes', async () => {
    const remove = vi
      .spyOn(api, 'delete')
      .mockResolvedValue({ data: { success: true, data: null } })
    const { user, onOpenChange, invalidate } = await renderDialog()

    await user.click(screen.getByRole('button', { name: 'Delete' }))

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(remove).toHaveBeenCalledExactlyOnceWith(
      '/api/provider_mapping_rule/7'
    )
    expect(toast.success).toHaveBeenCalledWith('Mapping rule deleted')
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: ['provider-mapping-rules'],
    })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['payable-bills'] })
  })

  it('keeps the dialog open and shows the reason when deleting fails', async () => {
    vi.spyOn(api, 'delete').mockResolvedValue({
      data: { success: false, message: 'Mapping rule does not exist' },
    })
    const { user, onOpenChange } = await renderDialog()

    await user.click(screen.getByRole('button', { name: 'Delete' }))

    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith('Mapping rule does not exist')
    )
    expect(onOpenChange).not.toHaveBeenCalled()
    expect(
      screen.getByRole('alertdialog', { name: 'Delete Mapping Rule' })
    ).toBeVisible()
  })
})
