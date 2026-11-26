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
import { MappingRuleDeleteDialog } from './mapping-rule-delete-dialog'
import { MappingRuleMutateDialog } from './mapping-rule-mutate-dialog'
import { useMappingRules } from './mapping-rules-provider'

export function MappingRulesDialogs() {
  const { open, setOpen, currentRow } = useMappingRules()
  const close = (isOpen: boolean) => {
    if (!isOpen) setOpen(null)
  }

  return (
    <>
      <MappingRuleMutateDialog open={open === 'create'} onOpenChange={close} />
      {currentRow && (
        <>
          <MappingRuleMutateDialog
            key={currentRow.id}
            open={open === 'update'}
            onOpenChange={close}
            currentRow={currentRow}
          />
          <MappingRuleDeleteDialog
            open={open === 'delete'}
            onOpenChange={close}
            rule={currentRow}
          />
        </>
      )}
    </>
  )
}
