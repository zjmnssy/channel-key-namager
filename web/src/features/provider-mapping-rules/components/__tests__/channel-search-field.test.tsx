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
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { useState } from 'react'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { searchProviderMappingChannels } from '../../api'
import type { ApiResponse, ProviderMappingChannelOption } from '../../types'
import { ChannelSearchField } from '../channel-search-field'

vi.mock('../../api')

// The latest pull of site 3 does not have channel 9001; only its full ID finds it.
const CHANNELS: Record<string, ProviderMappingChannelOption[]> = {
  '': [
    {
      remote_channel_id: 18704,
      remote_channel_name: 'az-gpt-eastus2-22536',
      has_rule: false,
    },
    { remote_channel_id: 17, remote_channel_name: 'claude-17', has_rule: true },
  ],
  '9001': [
    {
      remote_channel_id: 9001,
      remote_channel_name: 'old-gpt-9001',
      has_rule: false,
    },
  ],
}

const clients: QueryClient[] = []

function Field() {
  const [value, setValue] = useState('')
  return (
    <>
      <label htmlFor='site-channel'>Site Channel</label>
      <ChannelSearchField
        id='site-channel'
        siteId={3}
        value={value}
        onValueChange={setValue}
      />
      <output aria-label='Chosen channel'>{value}</output>
    </>
  )
}

async function renderField() {
  const i18n = createInstance()
  await i18n.init({ lng: 'en', resources: {}, initAsync: false })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <Field />
      </QueryClientProvider>
    </I18nextProvider>
  )
  return screen.getByRole('combobox', { name: 'Site Channel' })
}

beforeEach(() => {
  vi.mocked(searchProviderMappingChannels).mockImplementation(
    async (_siteId, keyword) => ({
      success: true,
      data: CHANNELS[keyword] ?? [],
    })
  )
})

afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  vi.useRealTimers()
  vi.resetAllMocks()
})

describe('ChannelSearchField', () => {
  it('searches the typed keyword only after typing pauses for 300 ms', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    const input = await renderField()

    // Synchronous input events: user-event waits on a timer that vitest's fake
    // timers would never fire.
    for (const typed of ['9', '90', '900', '9001']) {
      fireEvent.change(input, { target: { value: typed } })
    }
    await act(() => vi.advanceTimersByTimeAsync(299))
    expect(searchProviderMappingChannels).not.toHaveBeenCalledWith(3, '9001')

    await act(() => vi.advanceTimersByTimeAsync(1))
    expect(searchProviderMappingChannels).toHaveBeenCalledWith(3, '9001')
    for (const partial of ['9', '90', '900']) {
      expect(searchProviderMappingChannels).not.toHaveBeenCalledWith(3, partial)
    }
  })

  it('disables a channel that already has a rule and marks it', async () => {
    const user = userEvent.setup()
    const input = await renderField()

    await user.click(input)

    const taken = await screen.findByRole('option', { name: /^17 claude-17/ })
    expect(taken).toHaveAttribute('aria-disabled', 'true')
    expect(taken).toHaveTextContent('Already has a rule')
    expect(
      screen.getByRole('option', { name: '18704 az-gpt-eastus2-22536' })
    ).not.toHaveAttribute('aria-disabled')
  })

  it('keeps the chosen channel and its name when the next results do not include it', async () => {
    const user = userEvent.setup()
    const input = await renderField()
    await user.click(input)
    await user.type(input, '9001')
    await user.click(
      await screen.findByRole('option', { name: '9001 old-gpt-9001' })
    )
    expect(
      screen.getByRole('status', { name: 'Chosen channel' })
    ).toHaveTextContent('9001')

    // Closing the list searches the latest pull again, which lacks channel 9001.
    await user.click(input)
    expect(
      await screen.findByRole('option', { name: '18704 az-gpt-eastus2-22536' })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('option', { name: '9001 old-gpt-9001' })
    ).toBeInTheDocument()
    await user.keyboard('{Escape}')

    await waitFor(() => expect(input).toHaveValue('9001 old-gpt-9001'))
    expect(
      screen.getByRole('status', { name: 'Chosen channel' })
    ).toHaveTextContent('9001')
  })

  it('shows Loading... while the search is running', async () => {
    let finish: (
      value: ApiResponse<ProviderMappingChannelOption[]>
    ) => void = () => {}
    vi.mocked(searchProviderMappingChannels).mockReturnValue(
      new Promise((resolve) => {
        finish = resolve
      })
    )
    const user = userEvent.setup()
    const input = await renderField()

    await user.click(input)

    expect(await screen.findByText('Loading...')).toBeInTheDocument()
    expect(screen.queryByText('No results found')).not.toBeInTheDocument()
    await act(async () => finish({ success: true, data: [] }))
  })

  it('shows No results found when the search finds no channel', async () => {
    const user = userEvent.setup()
    const input = await renderField()
    await user.click(input)
    await user.type(input, 'nothing-like-this')

    expect(await screen.findByText('No results found')).toBeInTheDocument()
    expect(searchProviderMappingChannels).toHaveBeenLastCalledWith(
      3,
      'nothing-like-this'
    )
    expect(screen.queryByRole('option')).not.toBeInTheDocument()
  })
})
