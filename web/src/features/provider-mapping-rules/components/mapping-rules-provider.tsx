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

import type { ProviderMappingDialogType, ProviderMappingRule } from '../types'

type MappingRulesContextType = {
  open: ProviderMappingDialogType | null
  setOpen: (value: ProviderMappingDialogType | null) => void
  currentRow: ProviderMappingRule | null
  setCurrentRow: React.Dispatch<
    React.SetStateAction<ProviderMappingRule | null>
  >
}

const MappingRulesContext = React.createContext<MappingRulesContextType | null>(
  null
)

export function MappingRulesProvider({
  children,
}: {
  children: React.ReactNode
}) {
  const [open, setOpen] = useDialogState<ProviderMappingDialogType>(null)
  const [currentRow, setCurrentRow] = useState<ProviderMappingRule | null>(null)

  return (
    <MappingRulesContext value={{ open, setOpen, currentRow, setCurrentRow }}>
      {children}
    </MappingRulesContext>
  )
}

// eslint-disable-next-line react-refresh/only-export-components
export const useMappingRules = () => {
  const context = React.useContext(MappingRulesContext)
  if (!context) {
    throw new Error(
      'useMappingRules has to be used within <MappingRulesProvider>'
    )
  }
  return context
}
