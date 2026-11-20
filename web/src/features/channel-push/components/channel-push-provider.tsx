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
import React, { useState } from 'react'

import useDialogState from '@/hooks/use-dialog'

import type { ChannelPushDialogType, ChannelPushTask } from '../types'

type ChannelPushContextType = {
  open: ChannelPushDialogType | null
  setOpen: (value: ChannelPushDialogType | null) => void
  currentTask: ChannelPushTask | null
  setCurrentTask: React.Dispatch<React.SetStateAction<ChannelPushTask | null>>
}

const ChannelPushContext = React.createContext<ChannelPushContextType | null>(
  null
)

export function ChannelPushProvider({
  children,
}: {
  children: React.ReactNode
}) {
  const [open, setOpen] = useDialogState<ChannelPushDialogType>(null)
  const [currentTask, setCurrentTask] = useState<ChannelPushTask | null>(null)

  return (
    <ChannelPushContext value={{ open, setOpen, currentTask, setCurrentTask }}>
      {children}
    </ChannelPushContext>
  )
}

// eslint-disable-next-line react-refresh/only-export-components
export const useChannelPush = () => {
  const context = React.useContext(ChannelPushContext)
  if (!context) {
    throw new Error(
      'useChannelPush has to be used within <ChannelPushProvider>'
    )
  }
  return context
}
