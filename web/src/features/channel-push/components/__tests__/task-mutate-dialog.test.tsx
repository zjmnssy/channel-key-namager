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
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { toast } from 'sonner'
import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi,
  type Mock,
} from 'vitest'

import {
  createChannelPushTask,
  getChannelPushOptions,
  getChannelPushSiteGroups,
  updateChannelPushTask,
} from '../../api'
import { CHANNEL_PUSH_QUERY_KEYS } from '../../constants'
import type {
  ApiResponse,
  ChannelPushOptions,
  ChannelPushTask,
} from '../../types'
import { TaskMutateDialog } from '../task-mutate-dialog'

vi.mock('../../api', () => ({
  getChannelPushOptions: vi.fn(),
  getChannelPushSiteGroups: vi.fn(),
  createChannelPushTask: vi.fn(),
  updateChannelPushTask: vi.fn(),
}))

type User = ReturnType<typeof userEvent.setup>

const options: ChannelPushOptions = {
  sites: [
    { id: 1, name: 'Alpha' },
    { id: 2, name: 'Beta' },
  ],
  suppliers: [
    { id: 5, username: 'alice', display_name: 'Alice' },
    { id: 6, username: 'bob', display_name: '' },
  ],
}

const siteGroups: Record<number, string[]> = {
  1: ['default', 'svip'],
  2: ['default', 'vip'],
}

const savedTask: ChannelPushTask = {
  id: 3,
  site_id: 1,
  site_name: 'Alpha',
  site_deleted: false,
  suppliers: [{ id: 5, username: 'alice' }],
  task_type: 'periodic',
  interval_minutes: 60,
  target_group: 'svip',
  enabled: true,
  next_run_at: 0,
  last_finished_at: 0,
  created_time: 1,
}

let client: QueryClient
let onOpenChange: Mock<(open: boolean) => void>

function wrap(currentTask?: ChannelPushTask) {
  return (
    <QueryClientProvider client={client}>
      <TaskMutateDialog
        open
        onOpenChange={onOpenChange}
        currentTask={currentTask}
      />
    </QueryClientProvider>
  )
}

async function chooseOption(user: User, field: string, option: string) {
  await user.click(await screen.findByRole('combobox', { name: field }))
  await user.click(
    within(await screen.findByRole('listbox')).getByRole('option', {
      name: option,
    })
  )
}

async function chooseSuppliers(user: User, names: string[]) {
  await user.click(screen.getByLabelText('Provider users'))
  for (const name of names) {
    await user.click(
      await screen.findByRole('option', { name: new RegExp(`^${name}`) })
    )
  }
  await user.keyboard('{Escape}')
}

async function fillCreateForm(user: User) {
  await chooseOption(user, 'Third-party site', 'Beta')
  await chooseSuppliers(user, ['alice', 'bob'])
  await waitFor(() =>
    expect(screen.getByRole('combobox', { name: 'Target group' })).toBeEnabled()
  )
  await chooseOption(user, 'Target group', 'vip')
}

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  onOpenChange = vi.fn()
  vi.mocked(getChannelPushOptions).mockResolvedValue({
    success: true,
    data: options,
  })
  vi.mocked(getChannelPushSiteGroups).mockImplementation(async (siteId) => ({
    success: true,
    data: siteGroups[siteId] ?? [],
  }))
})

afterEach(() => {
  vi.resetAllMocks()
  vi.restoreAllMocks()
})

describe('TaskMutateDialog', () => {
  it('shows required errors when submitting an empty form', async () => {
    const user = userEvent.setup()
    render(wrap())

    await user.click(screen.getByRole('button', { name: 'Save' }))

    expect(await screen.findByText('Site is required')).toBeInTheDocument()
    expect(screen.getByText('Select at least one supplier')).toBeInTheDocument()
    expect(screen.getByText('Target group is required')).toBeInTheDocument()
    expect(createChannelPushTask).not.toHaveBeenCalled()
  })

  it('shows the interval input only for periodic tasks and rejects 4 and 1441', async () => {
    const user = userEvent.setup()
    render(wrap())
    expect(screen.queryByLabelText('Interval (minutes)')).toBeNull()

    await user.click(screen.getByRole('radio', { name: 'Periodic' }))
    const interval = screen.getByLabelText('Interval (minutes)')
    expect(interval).toHaveValue('30')

    for (const value of ['4', '1441']) {
      await user.clear(interval)
      await user.type(interval, value)
      await user.click(screen.getByRole('button', { name: 'Save' }))
      expect(
        await screen.findByText(
          'Interval must be an integer from 5 to 1440 minutes'
        )
      ).toBeInTheDocument()
    }
    expect(createChannelPushTask).not.toHaveBeenCalled()
  })

  it('disables target group until a site is selected', async () => {
    const user = userEvent.setup()
    render(wrap())
    expect(
      screen.getByRole('combobox', { name: 'Target group' })
    ).toBeDisabled()

    await chooseOption(user, 'Third-party site', 'Alpha')

    await waitFor(() =>
      expect(
        screen.getByRole('combobox', { name: 'Target group' })
      ).toBeEnabled()
    )
  })

  it("shows loading then the site's groups after choosing a site", async () => {
    let resolveGroups: (value: ApiResponse<string[]>) => void = () => {}
    vi.mocked(getChannelPushSiteGroups).mockReturnValue(
      new Promise((resolve) => {
        resolveGroups = resolve
      })
    )
    const user = userEvent.setup()
    render(wrap())

    await chooseOption(user, 'Third-party site', 'Beta')
    expect(await screen.findByText('Loading groups...')).toBeInTheDocument()
    resolveGroups({ success: true, data: ['default', 'vip'] })
    await waitFor(() =>
      expect(screen.queryByText('Loading groups...')).toBeNull()
    )

    await user.click(screen.getByRole('combobox', { name: 'Target group' }))
    const names = within(await screen.findByRole('listbox'))
      .getAllByRole('option')
      .map((option) => option.textContent)
    expect(names).toEqual(['default', 'vip'])
    expect(getChannelPushSiteGroups).toHaveBeenCalledWith(2)
  })

  it('shows the failure reason with a retry button when groups fail to load', async () => {
    vi.mocked(getChannelPushSiteGroups)
      .mockResolvedValueOnce({
        success: false,
        message: 'Site returned an HTML page',
        message_key: 'Site returned an HTML page',
      })
      .mockResolvedValueOnce({ success: true, data: ['default', 'vip'] })
    const user = userEvent.setup()
    render(wrap())

    await chooseOption(user, 'Third-party site', 'Beta')
    expect(
      await screen.findByText('Site returned an HTML page')
    ).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Retry' }))

    await waitFor(() =>
      expect(screen.queryByText('Site returned an HTML page')).toBeNull()
    )
    await chooseOption(user, 'Target group', 'vip')
    expect(
      screen.getByRole('combobox', { name: 'Target group' })
    ).toHaveTextContent('vip')
  })

  it('clears the selected group when the site changes', async () => {
    const user = userEvent.setup()
    render(wrap())
    await chooseOption(user, 'Third-party site', 'Alpha')
    await waitFor(() =>
      expect(
        screen.getByRole('combobox', { name: 'Target group' })
      ).toBeEnabled()
    )
    await chooseOption(user, 'Target group', 'svip')

    await chooseOption(user, 'Third-party site', 'Beta')

    expect(
      screen.getByRole('combobox', { name: 'Target group' })
    ).toHaveTextContent('Select a target group')
  })

  it('keeps a saved group missing on the site and marks it as no longer on the site', async () => {
    const user = userEvent.setup()
    render(wrap({ ...savedTask, target_group: 'legacy' }))
    await waitFor(() =>
      expect(getChannelPushSiteGroups).toHaveBeenCalledWith(1)
    )

    await user.click(
      await screen.findByRole('combobox', { name: 'Target group' })
    )

    const option = await within(await screen.findByRole('listbox')).findByRole(
      'option',
      { name: /^legacy/ }
    )
    expect(option).toHaveTextContent('No longer on the site')
  })

  it('marks a selected user who is no longer a channel supplier', async () => {
    render(
      wrap({
        ...savedTask,
        suppliers: [
          { id: 5, username: 'alice' },
          { id: 9, username: 'carol' },
        ],
      })
    )

    expect(
      await screen.findByText(/^carol .*No longer a channel supplier/)
    ).toBeInTheDocument()
    expect(screen.queryByText(/^alice .*No longer/)).toBeNull()
  })

  it('sends numeric ids and interval 0 for a once task', async () => {
    vi.mocked(createChannelPushTask).mockResolvedValue({
      success: true,
      data: savedTask,
    })
    const user = userEvent.setup()
    render(wrap())

    await fillCreateForm(user)
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(createChannelPushTask).toHaveBeenCalled())
    expect(createChannelPushTask).toHaveBeenCalledWith({
      site_id: 2,
      supplier_user_ids: [5, 6],
      task_type: 'once',
      interval_minutes: 0,
      target_group: 'vip',
    })
  })

  it('shows a server field error under the matching field', async () => {
    vi.mocked(createChannelPushTask).mockResolvedValue({
      success: false,
      message: 'User 6 is not a channel supplier',
      message_key: 'User 6 is not a channel supplier',
      field: 'supplier_user_ids',
    })
    const user = userEvent.setup()
    render(wrap())

    await fillCreateForm(user)
    await user.click(screen.getByRole('button', { name: 'Save' }))

    const message = await screen.findByText('User 6 is not a channel supplier')
    const item = message.closest('[data-slot="form-item"]')
    expect(item).not.toBeNull()
    expect(
      within(item as HTMLElement).getByText('Provider users')
    ).toBeInTheDocument()
    expect(onOpenChange).not.toHaveBeenCalledWith(false)
  })

  it('closes and shows the saved toast after success', async () => {
    vi.mocked(updateChannelPushTask).mockResolvedValue({
      success: true,
      data: savedTask,
    })
    const success = vi.spyOn(toast, 'success')
    const listKey = [...CHANNEL_PUSH_QUERY_KEYS.tasks, 1, 10]
    client.setQueryData(listKey, { items: [] })
    const user = userEvent.setup()
    render(wrap(savedTask))
    await waitFor(() =>
      expect(
        screen.getByRole('combobox', { name: 'Target group' })
      ).toBeEnabled()
    )

    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(updateChannelPushTask).toHaveBeenCalledWith(3, {
      site_id: 1,
      supplier_user_ids: [5],
      task_type: 'periodic',
      interval_minutes: 60,
      target_group: 'svip',
    })
    expect(success).toHaveBeenCalledWith(
      'Saved. The task will start within half a minute'
    )
    expect(client.getQueryState(listKey)?.isInvalidated).toBe(true)
  })

  it('shows a plain saved toast when the saved task is disabled', async () => {
    const disabledTask = { ...savedTask, enabled: false }
    vi.mocked(updateChannelPushTask).mockResolvedValue({
      success: true,
      data: disabledTask,
    })
    const success = vi.spyOn(toast, 'success')
    const user = userEvent.setup()
    render(wrap(disabledTask))
    await waitFor(() =>
      expect(
        screen.getByRole('combobox', { name: 'Target group' })
      ).toBeEnabled()
    )

    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(success).toHaveBeenCalledWith('Saved successfully')
    expect(success).not.toHaveBeenCalledWith(
      'Saved. The task will start within half a minute'
    )
  })
})
