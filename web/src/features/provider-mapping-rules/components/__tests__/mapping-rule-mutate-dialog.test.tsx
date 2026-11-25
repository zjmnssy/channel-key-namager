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
  act,
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AxiosError } from 'axios'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { toast } from 'sonner'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { getPayableBillOptions } from '@/features/payable-bills/api'

import {
  createProviderMappingRule,
  listProviderMappingRules,
  previewProviderMappingRule,
  searchProviderMappingChannels,
  updateProviderMappingRule,
} from '../../api'
import type {
  ApiResponse,
  ProviderMappingPreview,
  ProviderMappingRule,
  ProviderMappingSaveResult,
} from '../../types'
import { MappingRuleMutateDialog } from '../mapping-rule-mutate-dialog'

vi.mock('../../api')
vi.mock('@/features/payable-bills/api')
vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
}))

type User = ReturnType<typeof userEvent.setup>

const savedRule: ProviderMappingRule = {
  id: 7,
  site_id: 3,
  site_name: 'Alpha',
  site_deleted: false,
  rule_type: 'channel',
  remote_channel_id: 18704,
  remote_channel_name: 'az-gpt-eastus2-22536',
  provider_id: 6,
  provider_name: 'bob',
  provider_valid: false,
  created_time: 1,
  updated_time: 1,
}

const BACKFILL = 'Also apply to details already pulled'
const RULE_TYPE_NAMES = [
  'All site channels',
  'Unresolved channels',
  'Specified channel',
]
const NO_DISCOUNT_WARNING =
  'This provider has no payable discount, so the changed details will have no payable amount.'
const SWEEP_FAILED_WARNING =
  'Some details pulled while saving may not have been changed. Edit the rule and apply it to pulled details again.'

const clients: QueryClient[] = []

async function renderDialog(currentRow?: ProviderMappingRule) {
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
        <MappingRuleMutateDialog
          open
          onOpenChange={onOpenChange}
          currentRow={currentRow}
        />
      </QueryClientProvider>
    </I18nextProvider>
  )
  return { user: userEvent.setup(), onOpenChange, invalidate }
}

async function chooseSite(user: User, name: string) {
  await user.click(screen.getByRole('combobox', { name: 'Third-party site' }))
  await user.click(
    within(await screen.findByRole('listbox')).getByRole('option', { name })
  )
}

async function chooseProvider(user: User, name: string) {
  await user.click(screen.getByRole('combobox', { name: 'Provider' }))
  await user.click(await screen.findByRole('option', { name }))
}

async function fillAllChannelsRule(user: User) {
  await chooseSite(user, 'Alpha')
  await chooseProvider(user, 'alice')
}

function saved(
  appliedRows: number,
  sweepFailed = false
): ApiResponse<ProviderMappingSaveResult> {
  return {
    success: true,
    data: {
      rule: savedRule,
      applied_rows: appliedRows,
      sweep_failed: sweepFailed,
    },
  }
}

// Label and text of each entry the confirmation lists about the rule.
function ruleSummaryOf(dialog: HTMLElement) {
  return Object.fromEntries(
    within(dialog)
      .getAllByRole('term')
      .map((term) => [term.textContent, term.nextElementSibling?.textContent])
  )
}

const day = (label: string) =>
  screen.findByRole('button', { name: new RegExp(label) })

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  // 00:30 on 2026-11-25 in Beijing, so the latest bill date is 2026-11-24.
  vi.setSystemTime(new Date('2026-11-24T16:30:00Z'))
  vi.mocked(getPayableBillOptions).mockResolvedValue({
    success: true,
    data: {
      sites: [
        { id: 3, name: 'Alpha', deleted: false },
        { id: 4, name: 'Beta', deleted: false },
        { id: 8, name: 'Gamma', deleted: true },
      ],
      providers: [
        { id: 5, username: 'alice', disabled: false, payable_discount: 0.8 },
        { id: 6, username: 'bob', disabled: false, payable_discount: null },
      ],
    },
  })
  vi.mocked(listProviderMappingRules).mockResolvedValue({
    success: true,
    data: { items: [], total: 0, page: 1, page_size: 1 },
  })
  vi.mocked(searchProviderMappingChannels).mockResolvedValue({
    success: true,
    data: [
      {
        remote_channel_id: 18704,
        remote_channel_name: 'az-gpt-eastus2-22536',
        has_rule: false,
      },
    ],
  })
  vi.mocked(previewProviderMappingRule).mockResolvedValue({
    success: true,
    data: { rows: 3 },
  })
  vi.mocked(createProviderMappingRule).mockResolvedValue(saved(0))
  vi.mocked(updateProviderMappingRule).mockResolvedValue(saved(0))
})

afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  vi.useRealTimers()
  vi.resetAllMocks()
  vi.restoreAllMocks()
})

describe('MappingRuleMutateDialog when creating', () => {
  it('offers only the sites that are not deleted', async () => {
    const { user } = await renderDialog()

    await user.click(screen.getByRole('combobox', { name: 'Third-party site' }))
    const list = await screen.findByRole('listbox')
    await within(list).findByRole('option', { name: 'Alpha' })

    expect(
      within(list)
        .getAllByRole('option')
        .map((option) => option.textContent)
    ).toEqual(['Alpha', 'Beta'])
  })

  it('disables the site-level types when the site already has one and switches to a specified channel', async () => {
    vi.mocked(listProviderMappingRules).mockResolvedValue({
      success: true,
      data: {
        items: [
          {
            ...savedRule,
            id: 2,
            rule_type: 'unresolved',
            remote_channel_id: 0,
            remote_channel_name: '',
            provider_valid: true,
          },
        ],
        total: 1,
        page: 1,
        page_size: 1,
      },
    })
    const { user } = await renderDialog()
    expect(
      screen.getByRole('radio', { name: 'All site channels' })
    ).toBeChecked()

    await chooseSite(user, 'Alpha')

    await waitFor(() =>
      expect(
        screen.getByRole('radio', { name: 'Specified channel' })
      ).toBeChecked()
    )
    expect(listProviderMappingRules).toHaveBeenCalledWith({
      p: 1,
      page_size: 1,
      site_ids: [3],
      types: ['all', 'unresolved'],
    })
    for (const name of ['All site channels', 'Unresolved channels']) {
      expect(screen.getByRole('radio', { name })).toHaveAttribute(
        'aria-disabled',
        'true'
      )
    }
    expect(
      screen.getByRole('radio', { name: 'Specified channel' })
    ).not.toHaveAttribute('aria-disabled', 'true')
    expect(
      screen.getByText(
        'This site already has a "Unresolved channels" rule. Edit the existing rule instead.'
      )
    ).toBeInTheDocument()
    expect(
      screen.getByRole('combobox', { name: 'Site Channel' })
    ).toBeInTheDocument()
  })

  it('still lets a site-level type be saved when the site check fails', async () => {
    vi.mocked(listProviderMappingRules).mockRejectedValue(
      new AxiosError('Network Error', 'ERR_NETWORK')
    )
    const { user, onOpenChange } = await renderDialog()
    await fillAllChannelsRule(user)
    await waitFor(() => expect(listProviderMappingRules).toHaveBeenCalled())

    await user.click(screen.getByRole('radio', { name: 'Unresolved channels' }))
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(createProviderMappingRule).toHaveBeenCalledExactlyOnceWith({
      site_id: 3,
      rule_type: 'unresolved',
      provider_id: 5,
    })
  })

  it('clears the selected channel when the site changes', async () => {
    const { user } = await renderDialog()
    await chooseSite(user, 'Alpha')
    await user.click(screen.getByRole('radio', { name: 'Specified channel' }))
    await user.click(screen.getByRole('combobox', { name: 'Site Channel' }))
    await user.click(
      await screen.findByRole('option', { name: '18704 az-gpt-eastus2-22536' })
    )
    await waitFor(() =>
      expect(
        screen.getByRole('combobox', { name: 'Site Channel' })
      ).toHaveValue('18704 az-gpt-eastus2-22536')
    )

    await chooseSite(user, 'Beta')

    await waitFor(() =>
      expect(
        screen.getByRole('combobox', { name: 'Site Channel' })
      ).toHaveValue('')
    )
    await chooseProvider(user, 'alice')
    await user.click(screen.getByRole('button', { name: 'Save' }))
    expect(
      await screen.findByText('Site channel is required')
    ).toBeInTheDocument()
    expect(createProviderMappingRule).not.toHaveBeenCalled()
  })

  it('creates an all-channels rule without backfill', async () => {
    const { user, onOpenChange, invalidate } = await renderDialog()
    await fillAllChannelsRule(user)

    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(createProviderMappingRule).toHaveBeenCalledExactlyOnceWith({
      site_id: 3,
      rule_type: 'all',
      provider_id: 5,
    })
    expect(previewProviderMappingRule).not.toHaveBeenCalled()
    expect(toast.success).toHaveBeenCalledWith('Mapping rule saved')
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: ['provider-mapping-rules'],
    })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['payable-bills'] })
  })

  it('creates a specified-channel rule with the chosen channel', async () => {
    const { user, onOpenChange } = await renderDialog()
    await chooseSite(user, 'Alpha')
    await user.click(screen.getByRole('radio', { name: 'Specified channel' }))
    await user.click(screen.getByRole('combobox', { name: 'Site Channel' }))
    await user.click(
      await screen.findByRole('option', { name: '18704 az-gpt-eastus2-22536' })
    )
    await chooseProvider(user, 'alice')

    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(createProviderMappingRule).toHaveBeenCalledExactlyOnceWith({
      site_id: 3,
      rule_type: 'channel',
      remote_channel_id: 18704,
      provider_id: 5,
    })
  })

  it('shows the preview count and asks for confirmation before saving a backfill', async () => {
    vi.mocked(createProviderMappingRule).mockResolvedValue(saved(3))
    const { user, onOpenChange } = await renderDialog()
    await fillAllChannelsRule(user)
    await user.click(screen.getByRole('checkbox', { name: BACKFILL }))
    expect(
      screen.getByRole('button', { name: /2026-11-01 ~ 2026-11-24/ })
    ).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Save' }))
    const confirm = await screen.findByRole('alertdialog', {
      name: 'Apply to details already pulled?',
    })
    expect(confirm).toHaveTextContent(
      'This will change 3 details, and provider bills will change accordingly. This cannot be undone.'
    )
    expect(ruleSummaryOf(confirm)).toEqual({
      'Third-party site': 'Alpha',
      'Rule Type': 'All site channels',
      Provider: 'alice',
    })
    expect(previewProviderMappingRule).toHaveBeenCalledExactlyOnceWith({
      site_id: 3,
      rule_type: 'all',
      remote_channel_id: 0,
      provider_id: 5,
      start_date: 20261101,
      end_date: 20261124,
    })
    expect(createProviderMappingRule).not.toHaveBeenCalled()

    await user.click(within(confirm).getByRole('button', { name: 'Cancel' }))
    await waitFor(() =>
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    )
    expect(createProviderMappingRule).not.toHaveBeenCalled()
    expect(onOpenChange).not.toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: 'Save' }))
    await user.click(
      within(await screen.findByRole('alertdialog')).getByRole('button', {
        name: 'Save',
      })
    )

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(createProviderMappingRule).toHaveBeenCalledExactlyOnceWith({
      site_id: 3,
      rule_type: 'all',
      provider_id: 5,
      apply: { start_date: 20261101, end_date: 20261124 },
    })
    expect(toast.success).toHaveBeenCalledWith(
      'Mapping rule saved, 3 details changed'
    )
  })

  it('names the chosen site channel in the backfill confirmation of a specified-channel rule', async () => {
    const { user } = await renderDialog()
    await chooseSite(user, 'Alpha')
    await user.click(screen.getByRole('radio', { name: 'Specified channel' }))
    await user.click(screen.getByRole('combobox', { name: 'Site Channel' }))
    await user.click(
      await screen.findByRole('option', { name: '18704 az-gpt-eastus2-22536' })
    )
    await chooseProvider(user, 'bob')
    await user.click(screen.getByRole('checkbox', { name: BACKFILL }))

    await user.click(screen.getByRole('button', { name: 'Save' }))

    expect(ruleSummaryOf(await screen.findByRole('alertdialog'))).toEqual({
      'Third-party site': 'Alpha',
      'Rule Type': 'Specified channel',
      'Site Channel': '18704 az-gpt-eastus2-22536',
      Provider: 'bob',
    })
  })

  it.each([
    { sweepFailed: true, warnings: [[SWEEP_FAILED_WARNING]] },
    { sweepFailed: false, warnings: [] },
  ])(
    'warns about details pulled while saving only when their changes failed (sweep_failed $sweepFailed)',
    async ({ sweepFailed, warnings }) => {
      vi.mocked(createProviderMappingRule).mockResolvedValue(
        saved(3, sweepFailed)
      )
      const { user, onOpenChange } = await renderDialog()
      await fillAllChannelsRule(user)
      await user.click(screen.getByRole('checkbox', { name: BACKFILL }))
      await user.click(screen.getByRole('button', { name: 'Save' }))

      await user.click(
        within(await screen.findByRole('alertdialog')).getByRole('button', {
          name: 'Save',
        })
      )

      await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
      expect(toast.success).toHaveBeenCalledExactlyOnceWith(
        'Mapping rule saved, 3 details changed'
      )
      expect(vi.mocked(toast.warning).mock.calls).toEqual(warnings)
    }
  )

  it('returns to the form without saving when the confirmation is dismissed with Escape', async () => {
    const { user, onOpenChange } = await renderDialog()
    await fillAllChannelsRule(user)
    await user.click(screen.getByRole('checkbox', { name: BACKFILL }))
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await screen.findByRole('alertdialog')

    await user.keyboard('{Escape}')

    await waitFor(() =>
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    )
    expect(onOpenChange).not.toHaveBeenCalled()
    expect(createProviderMappingRule).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled()
  })

  it('tells the user when nothing needs to change', async () => {
    vi.mocked(previewProviderMappingRule).mockResolvedValue({
      success: true,
      data: { rows: 0 },
    })
    const { user, onOpenChange } = await renderDialog()
    await fillAllChannelsRule(user)
    await user.click(screen.getByRole('checkbox', { name: BACKFILL }))

    await user.click(screen.getByRole('button', { name: 'Save' }))

    const confirm = await screen.findByRole('alertdialog')
    expect(confirm).toHaveTextContent(
      'No details in the selected dates need to change. Only the rule will be saved.'
    )
    expect(confirm).not.toHaveTextContent('This will change')
    await user.click(within(confirm).getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(createProviderMappingRule).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({
        apply: { start_date: 20261101, end_date: 20261124 },
      })
    )
  })

  it('reports a failed preview and saves nothing', async () => {
    vi.mocked(previewProviderMappingRule).mockResolvedValue({
      success: false,
      message: 'Date range cannot exceed 92 days',
    })
    const { user, onOpenChange } = await renderDialog()
    await fillAllChannelsRule(user)
    await user.click(screen.getByRole('checkbox', { name: BACKFILL }))

    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith(
        'Date range cannot exceed 92 days'
      )
    )
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    expect(createProviderMappingRule).not.toHaveBeenCalled()
    expect(onOpenChange).not.toHaveBeenCalled()
  })

  it('warns about a provider without a payable discount only while backfill is on', async () => {
    const { user } = await renderDialog()
    await chooseProvider(user, 'bob')
    expect(screen.queryByText(NO_DISCOUNT_WARNING)).not.toBeInTheDocument()

    await user.click(screen.getByRole('checkbox', { name: BACKFILL }))
    expect(screen.getByText(NO_DISCOUNT_WARNING)).toBeInTheDocument()

    await chooseProvider(user, 'alice')
    expect(screen.queryByText(NO_DISCOUNT_WARNING)).not.toBeInTheDocument()
  })

  it('rejects a range over 92 days without calling the preview', async () => {
    const { user } = await renderDialog()
    await fillAllChannelsRule(user)
    await user.click(screen.getByRole('checkbox', { name: BACKFILL }))
    await user.click(
      screen.getByRole('button', { name: /2026-11-01 ~ 2026-11-24/ })
    )
    for (let step = 0; step < 3; step++) {
      await user.click(
        screen.getByRole('button', { name: 'Go to the Previous Month' })
      )
    }
    await user.click(await day('August 23rd, 2026'))
    for (let step = 0; step < 2; step++) {
      await user.click(
        screen.getByRole('button', { name: 'Go to the Next Month' })
      )
    }
    await user.click(await day('November 24th, 2026'))

    expect(
      await screen.findByText('Date range cannot exceed 92 days')
    ).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Save' }))
    expect(
      screen.getByText('Date range cannot exceed 92 days')
    ).toBeInTheDocument()
    expect(previewProviderMappingRule).not.toHaveBeenCalled()
    expect(createProviderMappingRule).not.toHaveBeenCalled()
  })

  it.each([
    {
      reason: 'the server refuses it',
      arrange: () =>
        vi.mocked(createProviderMappingRule).mockResolvedValue({
          success: false,
          message: 'This site already has a site-level mapping rule',
        }),
      message: 'This site already has a site-level mapping rule',
    },
    {
      reason: 'the request fails',
      arrange: () =>
        vi
          .mocked(createProviderMappingRule)
          .mockRejectedValue(new AxiosError('Network Error', 'ERR_NETWORK')),
      message: 'Network Error',
    },
  ])(
    'keeps the dialog open and refreshes the lists when saving fails because $reason',
    async (failure) => {
      failure.arrange()
      const { user, onOpenChange, invalidate } = await renderDialog()
      await fillAllChannelsRule(user)

      await user.click(screen.getByRole('button', { name: 'Save' }))

      await waitFor(() =>
        expect(toast.error).toHaveBeenCalledWith(failure.message)
      )
      expect(invalidate).toHaveBeenCalledWith({
        queryKey: ['provider-mapping-rules'],
      })
      expect(invalidate).toHaveBeenCalledWith({ queryKey: ['payable-bills'] })
      expect(onOpenChange).not.toHaveBeenCalled()
      expect(
        screen.getByRole('dialog', { name: 'Create Mapping Rule' })
      ).toBeVisible()
      expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled()
    }
  )

  it('cannot be closed while saving', async () => {
    let finish: (
      value: ApiResponse<ProviderMappingSaveResult>
    ) => void = () => {}
    vi.mocked(createProviderMappingRule).mockReturnValue(
      new Promise((resolve) => {
        finish = resolve
      })
    )
    const { user, onOpenChange } = await renderDialog()
    await fillAllChannelsRule(user)

    await user.click(screen.getByRole('button', { name: 'Save' }))
    expect(
      await screen.findByRole('button', { name: 'Saving...' })
    ).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled()
    await user.keyboard('{Escape}')

    expect(onOpenChange).not.toHaveBeenCalled()
    await act(async () => finish(saved(0)))
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
  })

  it('keeps the confirmation open while the backfill is saving', async () => {
    let finish: (
      value: ApiResponse<ProviderMappingSaveResult>
    ) => void = () => {}
    vi.mocked(createProviderMappingRule).mockReturnValue(
      new Promise((resolve) => {
        finish = resolve
      })
    )
    const { user, onOpenChange } = await renderDialog()
    await fillAllChannelsRule(user)
    await user.click(screen.getByRole('checkbox', { name: BACKFILL }))
    await user.click(screen.getByRole('button', { name: 'Save' }))
    const confirm = await screen.findByRole('alertdialog')

    await user.click(within(confirm).getByRole('button', { name: 'Save' }))
    expect(
      await within(confirm).findByRole('button', { name: 'Saving...' })
    ).toBeDisabled()
    expect(
      within(confirm).getByRole('button', { name: 'Cancel' })
    ).toBeDisabled()
    await user.keyboard('{Escape}')

    expect(screen.getByRole('alertdialog')).toBeInTheDocument()
    expect(onOpenChange).not.toHaveBeenCalled()
    await act(async () => finish(saved(3)))
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
  })

  it('locks the form while the preview runs and saves the previewed values', async () => {
    let finishPreview: (
      value: ApiResponse<ProviderMappingPreview>
    ) => void = () => {}
    vi.mocked(previewProviderMappingRule).mockReturnValue(
      new Promise((resolve) => {
        finishPreview = resolve
      })
    )
    vi.mocked(createProviderMappingRule).mockResolvedValue(saved(3))
    const { user, onOpenChange } = await renderDialog()
    await fillAllChannelsRule(user)
    await user.click(screen.getByRole('checkbox', { name: BACKFILL }))

    await user.click(screen.getByRole('button', { name: 'Save' }))

    const provider = screen.getByRole('combobox', { name: 'Provider' })
    await waitFor(() => expect(provider).toBeDisabled())
    expect(
      screen.getByRole('combobox', { name: 'Third-party site' })
    ).toBeDisabled()
    for (const name of RULE_TYPE_NAMES) {
      expect(screen.getByRole('radio', { name })).toHaveAttribute(
        'aria-disabled',
        'true'
      )
    }
    expect(screen.getByRole('checkbox', { name: BACKFILL })).toHaveAttribute(
      'aria-disabled',
      'true'
    )
    expect(
      screen.getByRole('button', { name: /2026-11-01 ~ 2026-11-24/ })
    ).toBeDisabled()
    await user.click(provider)
    expect(
      screen.queryByRole('option', { name: 'bob' })
    ).not.toBeInTheDocument()
    expect(provider).toHaveValue('alice')

    await act(async () => finishPreview({ success: true, data: { rows: 3 } }))
    await user.click(
      within(await screen.findByRole('alertdialog')).getByRole('button', {
        name: 'Save',
      })
    )

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(previewProviderMappingRule).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ provider_id: 5 })
    )
    expect(createProviderMappingRule).toHaveBeenCalledExactlyOnceWith({
      site_id: 3,
      rule_type: 'all',
      provider_id: 5,
      apply: { start_date: 20261101, end_date: 20261124 },
    })
  })

  it('locks the form while saving', async () => {
    let finish: (
      value: ApiResponse<ProviderMappingSaveResult>
    ) => void = () => {}
    vi.mocked(createProviderMappingRule).mockReturnValue(
      new Promise((resolve) => {
        finish = resolve
      })
    )
    const { user, onOpenChange } = await renderDialog()
    await chooseSite(user, 'Alpha')
    await user.click(screen.getByRole('radio', { name: 'Specified channel' }))
    await user.click(screen.getByRole('combobox', { name: 'Site Channel' }))
    await user.click(
      await screen.findByRole('option', { name: '18704 az-gpt-eastus2-22536' })
    )
    await chooseProvider(user, 'alice')

    await user.click(screen.getByRole('button', { name: 'Save' }))

    await screen.findByRole('button', { name: 'Saving...' })
    for (const name of ['Third-party site', 'Site Channel', 'Provider']) {
      expect(screen.getByRole('combobox', { name })).toBeDisabled()
    }
    expect(screen.getByRole('combobox', { name: 'Site Channel' })).toHaveValue(
      '18704 az-gpt-eastus2-22536'
    )
    for (const name of RULE_TYPE_NAMES) {
      expect(screen.getByRole('radio', { name })).toHaveAttribute(
        'aria-disabled',
        'true'
      )
    }
    expect(screen.getByRole('checkbox', { name: BACKFILL })).toHaveAttribute(
      'aria-disabled',
      'true'
    )
    await act(async () => finish(saved(0)))
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(createProviderMappingRule).toHaveBeenCalledExactlyOnceWith({
      site_id: 3,
      rule_type: 'channel',
      remote_channel_id: 18704,
      provider_id: 5,
    })
  })
})

describe('MappingRuleMutateDialog when editing', () => {
  it('edits only the provider and asks to choose again when the provider is no longer valid', async () => {
    const { user, onOpenChange } = await renderDialog(savedRule)

    const dialog = screen.getByRole('dialog', { name: 'Edit Mapping Rule' })
    expect(dialog).toHaveTextContent('Alpha')
    expect(dialog).toHaveTextContent('Specified channel')
    expect(dialog).toHaveTextContent('18704 az-gpt-eastus2-22536')
    expect(
      screen.queryByRole('combobox', { name: 'Third-party site' })
    ).not.toBeInTheDocument()
    expect(screen.queryByRole('radio')).not.toBeInTheDocument()
    expect(screen.getByRole('combobox', { name: 'Provider' })).toHaveValue('')

    await user.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('Provider is required')).toBeInTheDocument()
    expect(updateProviderMappingRule).not.toHaveBeenCalled()

    await chooseProvider(user, 'alice')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(updateProviderMappingRule).toHaveBeenCalledExactlyOnceWith(7, {
      provider_id: 5,
    })
    expect(createProviderMappingRule).not.toHaveBeenCalled()
  })

  it('starts from the provider of the rule while it is still valid', async () => {
    await renderDialog({ ...savedRule, provider_valid: true })

    await waitFor(() =>
      expect(screen.getByRole('combobox', { name: 'Provider' })).toHaveValue(
        'bob'
      )
    )
  })

  it('previews and saves a backfill with the rule of the edited channel', async () => {
    vi.mocked(updateProviderMappingRule).mockResolvedValue(saved(3))
    const { user, onOpenChange } = await renderDialog({
      ...savedRule,
      provider_valid: true,
    })
    await chooseProvider(user, 'alice')
    await user.click(screen.getByRole('checkbox', { name: BACKFILL }))

    await user.click(screen.getByRole('button', { name: 'Save' }))
    const confirm = await screen.findByRole('alertdialog')
    // The edited rule with the provider being saved, not the old one.
    expect(ruleSummaryOf(confirm)).toEqual({
      'Third-party site': 'Alpha',
      'Rule Type': 'Specified channel',
      'Site Channel': '18704 az-gpt-eastus2-22536',
      Provider: 'alice',
    })
    await user.click(within(confirm).getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(previewProviderMappingRule).toHaveBeenCalledExactlyOnceWith({
      site_id: 3,
      rule_type: 'channel',
      remote_channel_id: 18704,
      provider_id: 5,
      start_date: 20261101,
      end_date: 20261124,
    })
    expect(updateProviderMappingRule).toHaveBeenCalledExactlyOnceWith(7, {
      provider_id: 5,
      apply: { start_date: 20261101, end_date: 20261124 },
    })
  })
})
