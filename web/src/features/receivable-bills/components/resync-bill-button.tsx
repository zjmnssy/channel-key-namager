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

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

import { resyncReceivableBill } from '../api'
import { RECEIVABLE_BILL_QUERY_KEYS } from '../constants'
import type { ReceivableBill } from '../types'

type ResyncBillButtonProps = {
  bill: ReceivableBill
}

export function ResyncBillButton(props: ResyncBillButtonProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [confirming, setConfirming] = useState(false)

  const mutation = useMutation({
    mutationFn: async () =>
      requireServerSuccess(await resyncReceivableBill(props.bill.id)),
    onSuccess: async (response) => {
      await queryClient.invalidateQueries({
        queryKey: RECEIVABLE_BILL_QUERY_KEYS.all,
      })
      const status = response.data?.status
      if (status === 'success') {
        toast.success(t('Bill synced'))
      } else if (status === 'retrying') {
        toast.warning(t('Sync failed, will retry automatically'))
      } else {
        toast.error(t('Sync failed'))
      }
    },
    onError: (error) => handleServerError(error),
    onSettled: () => setConfirming(false),
  })

  const handleClick = () => {
    if (props.bill.status === 'success') {
      setConfirming(true)
      return
    }
    mutation.mutate()
  }

  if (props.bill.site_deleted) {
    return (
      <Tooltip>
        <TooltipTrigger render={<span className='inline-flex' />}>
          <Button variant='ghost' size='sm' disabled>
            {t('Resync')}
          </Button>
        </TooltipTrigger>
        <TooltipContent>{t('The site has been deleted')}</TooltipContent>
      </Tooltip>
    )
  }

  return (
    <>
      <Button
        variant='ghost'
        size='sm'
        disabled={mutation.isPending}
        onClick={handleClick}
      >
        {t('Resync')}
      </Button>
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={t('Resync this bill?')}
        desc={t(
          "The current amount ${{amount}} will be overwritten with the site's current data.",
          { amount: props.bill.amount }
        )}
        confirmText={t('Resync')}
        isLoading={mutation.isPending}
        handleConfirm={() => mutation.mutate()}
      />
    </>
  )
}
