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
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { PAYABLE_BILL_QUERY_KEYS } from '@/features/payable-bills/constants'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

import { deleteProviderMappingRule } from '../api'
import { PROVIDER_MAPPING_QUERY_KEYS } from '../constants'
import type { ProviderMappingRule } from '../types'

type MappingRuleDeleteDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  rule: ProviderMappingRule
}

export function MappingRuleDeleteDialog(props: MappingRuleDeleteDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const mutation = useMutation({
    mutationFn: async (id: number) =>
      requireServerSuccess(await deleteProviderMappingRule(id)),
    onSuccess: () => {
      toast.success(t('Mapping rule deleted'))
      props.onOpenChange(false)
    },
    onError: (error) => handleServerError(error),
    // The payable details mark the channels that have a rule of their own.
    // A failure may hide a delete that went through, so refresh then too.
    onSettled: () => {
      void queryClient.invalidateQueries({
        queryKey: PROVIDER_MAPPING_QUERY_KEYS.all,
      })
      void queryClient.invalidateQueries({
        queryKey: PAYABLE_BILL_QUERY_KEYS.all,
      })
    },
  })

  return (
    <ConfirmDialog
      destructive
      open={props.open}
      onOpenChange={(open) => {
        if (!mutation.isPending) props.onOpenChange(open)
      }}
      title={t('Delete Mapping Rule')}
      desc={
        <>
          <p>
            {t('Delete the mapping rule of {{site}}?', {
              site: props.rule.site_name,
            })}
          </p>
          <p>
            {t(
              'The rule is no longer used from the next pull. Details already pulled stay unchanged.'
            )}
          </p>
        </>
      }
      confirmText={mutation.isPending ? t('Deleting...') : t('Delete')}
      isLoading={mutation.isPending}
      handleConfirm={() => mutation.mutate(props.rule.id)}
    />
  )
}
