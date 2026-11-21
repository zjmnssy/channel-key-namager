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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import type { Row } from '@tanstack/react-table'
import { Edit, Power, PowerOff } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DataTableRowActionMenu } from '@/components/data-table/core/row-action-menu'
import {
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
} from '@/components/ui/dropdown-menu'
import { handleServerError } from '@/lib/handle-server-error'
import { createServerError } from '@/lib/server-error-message'

import { setChannelPushTaskEnabled } from '../api'
import { CHANNEL_PUSH_QUERY_KEYS } from '../constants'
import type { ChannelPushTask } from '../types'
import { useChannelPush } from './channel-push-provider'

type TaskRowActionsProps = {
  row: Row<ChannelPushTask>
}

export function TaskRowActions(props: TaskRowActionsProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { setOpen, setCurrentTask } = useChannelPush()
  const task = props.row.original
  const nextEnabled = !task.enabled

  const toggle = useMutation({
    mutationFn: async () => {
      const res = await setChannelPushTaskEnabled(task.id, nextEnabled)
      if (!res.success) throw createServerError(res)
      return res
    },
    onSuccess: () => {
      // Enabling queues the task, so say when it starts.
      toast.success(
        nextEnabled
          ? t('Saved. The task will start within half a minute')
          : t('Task disabled')
      )
      void queryClient.invalidateQueries({
        queryKey: CHANNEL_PUSH_QUERY_KEYS.tasks,
      })
    },
    onError: (error) => handleServerError(error),
  })

  return (
    <DataTableRowActionMenu ariaLabel={t('Open menu')} modal={false}>
      <DropdownMenuItem
        onClick={() => {
          setCurrentTask(task)
          setOpen('update')
        }}
      >
        {t('Modify')}
        <DropdownMenuShortcut>
          <Edit size={16} />
        </DropdownMenuShortcut>
      </DropdownMenuItem>
      <DropdownMenuSeparator />
      <DropdownMenuItem
        disabled={toggle.isPending}
        onClick={() => toggle.mutate()}
      >
        {task.enabled ? t('Disable') : t('Enable')}
        <DropdownMenuShortcut>
          {task.enabled ? <PowerOff size={16} /> : <Power size={16} />}
        </DropdownMenuShortcut>
      </DropdownMenuItem>
    </DataTableRowActionMenu>
  )
}
