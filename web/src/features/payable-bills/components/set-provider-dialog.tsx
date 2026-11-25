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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Label } from '@/components/ui/label'
import { PROVIDER_MAPPING_QUERY_KEYS } from '@/features/provider-mapping-rules/constants'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

import { setPayableUsageProvider } from '../api'
import { PAYABLE_BILL_QUERY_KEYS, UNKNOWN_PROVIDER_VALUE } from '../constants'
import { usePayableBillOptions } from '../hooks/use-payable-bill-options'
import type { PayableUsage } from '../types'

// Sent as the provider to remove the manual setting of the channel.
const CLEAR_PROVIDER_ID = 0

type SetProviderDialogProps = {
  usage: PayableUsage
  open: boolean
  onOpenChange: (open: boolean) => void
}

/** Mount it for one row; the choice starts from the row's provider. */
export function SetProviderDialog(props: SetProviderDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const options = usePayableBillOptions()
  const [providerId, setProviderId] = useState(String(props.usage.provider_id))
  // Only channel suppliers can be chosen; clearing has its own action.
  const supplierOptions = options.providerOptions.filter(
    (option) => option.value !== UNKNOWN_PROVIDER_VALUE
  )
  const selected = supplierOptions.find((option) => option.value === providerId)

  const mutation = useMutation({
    mutationFn: async (nextProviderId: number) =>
      requireServerSuccess(
        await setPayableUsageProvider(props.usage.id, nextProviderId)
      ),
    onSuccess: (_response, nextProviderId) => {
      void queryClient.invalidateQueries({
        queryKey: PAYABLE_BILL_QUERY_KEYS.all,
      })
      // The rules page caches for a while, so mark it stale too.
      void queryClient.invalidateQueries({
        queryKey: PROVIDER_MAPPING_QUERY_KEYS.all,
      })
      toast.success(
        nextProviderId === CLEAR_PROVIDER_ID
          ? t('Manual setting cleared')
          : t('Provider updated successfully')
      )
      props.onOpenChange(false)
    },
    onError: (error) => handleServerError(error),
  })

  return (
    <Dialog
      open={props.open}
      onOpenChange={(open) => {
        if (!mutation.isPending) props.onOpenChange(open)
      }}
      title={t('Set Provider')}
      description={t(
        'The setting applies to all later syncs of this site channel. Other dates already pulled stay unchanged.'
      )}
      contentClassName='sm:max-w-md'
      footer={
        <>
          {props.usage.manual && (
            <Button
              variant='outline'
              className='sm:mr-auto'
              disabled={mutation.isPending}
              onClick={() => mutation.mutate(CLEAR_PROVIDER_ID)}
            >
              {t('Clear Manual Setting')}
            </Button>
          )}
          <Button
            variant='outline'
            disabled={mutation.isPending}
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            disabled={!selected || mutation.isPending}
            onClick={() => {
              if (selected) mutation.mutate(Number(selected.value))
            }}
          >
            {t('Save')}
          </Button>
        </>
      }
    >
      <div className='space-y-4'>
        <p className='text-muted-foreground text-sm break-all'>
          {props.usage.site_name} / {props.usage.remote_channel_name}
        </p>
        <div className='space-y-2'>
          <Label htmlFor='payable-usage-provider'>{t('Provider')}</Label>
          <Combobox
            id='payable-usage-provider'
            disabled={mutation.isPending}
            options={supplierOptions}
            value={selected?.value ?? null}
            onValueChange={(value) => setProviderId(value ?? '')}
            emptyText={t('No providers available')}
          />
        </div>
      </div>
    </Dialog>
  )
}
