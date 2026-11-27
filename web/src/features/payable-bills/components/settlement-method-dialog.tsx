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
import { TriangleAlert } from 'lucide-react'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

import { setPayableSettlementMethod } from '../api'
import {
  PAYABLE_BILL_QUERY_KEYS,
  SETTLEMENT_METHOD_LABEL_KEYS,
  SETTLEMENT_METHODS,
} from '../constants'
import type { PayableSettlement, PayableSettlementMethod } from '../types'

type SettlementMethodDialogProps = {
  settlement: PayableSettlement
  open: boolean
  onOpenChange: (open: boolean) => void
}

/** Mount it for one row; the choice starts from the row's method. */
export function SettlementMethodDialog(props: SettlementMethodDialogProps) {
  const { t } = useTranslation()
  const id = useId()
  const queryClient = useQueryClient()
  const [method, setMethod] = useState(props.settlement.method)
  const descriptions: Record<PayableSettlementMethod, string> = {
    channel: t('Calculated from the channel details pulled every day'),
    usage: t(
      'Fully assigned sites are calculated from the amount before discount of their daily bills; channels on other sites are still calculated from channel details'
    ),
  }
  // No fully assigned site, or each one has channels assigned to other
  // providers and stays on channel details: usage details settle nothing yet.
  const noEffectiveSite = props.settlement.whole_sites.every(
    (site) => site.conflict
  )

  const mutation = useMutation({
    mutationFn: async (nextMethod: PayableSettlementMethod) =>
      requireServerSuccess(
        await setPayableSettlementMethod(
          props.settlement.provider_id,
          nextMethod
        )
      ),
    onSuccess: () => {
      // The list, provider bills and channel usage details all show it.
      void queryClient.invalidateQueries({
        queryKey: PAYABLE_BILL_QUERY_KEYS.all,
      })
      toast.success(t('Settlement method updated'))
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
      title={t('Change settlement method: {{name}}', {
        name: props.settlement.username,
      })}
      contentClassName='sm:max-w-lg'
      footer={
        <>
          <Button
            variant='outline'
            disabled={mutation.isPending}
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            disabled={method === props.settlement.method || mutation.isPending}
            onClick={() => mutation.mutate(method)}
          >
            {mutation.isPending ? t('Saving...') : t('Save')}
          </Button>
        </>
      }
    >
      <div className='flex flex-col gap-4'>
        <RadioGroup
          value={method}
          onValueChange={(value) => {
            const next = SETTLEMENT_METHODS.find((item) => item === value)
            if (next) setMethod(next)
          }}
          aria-label={t('Settlement Method')}
          disabled={mutation.isPending}
          className='gap-3'
        >
          {SETTLEMENT_METHODS.map((item) => (
            <div key={item} className='flex items-start gap-2'>
              <RadioGroupItem
                value={item}
                id={`${id}-${item}`}
                aria-describedby={`${id}-${item}-description`}
                className='mt-0.5'
              />
              <div className='flex flex-col gap-1'>
                <Label htmlFor={`${id}-${item}`}>
                  {t(SETTLEMENT_METHOD_LABEL_KEYS[item])}
                </Label>
                <p
                  id={`${id}-${item}-description`}
                  className='text-muted-foreground text-sm'
                >
                  {descriptions[item]}
                </p>
              </div>
            </div>
          ))}
        </RadioGroup>
        {method === 'usage' && noEffectiveSite && (
          <Alert>
            <TriangleAlert aria-hidden='true' />
            <AlertDescription>
              {t(
                'This provider has no site that can be settled by usage details yet, so saving has no effect for now'
              )}
            </AlertDescription>
          </Alert>
        )}
        <p className='text-muted-foreground text-xs'>
          {t(
            'Only daily bills that sync successfully from now on are affected; bills already generated stay unchanged. To recalculate earlier dates, resync them in Daily Bills.'
          )}
        </p>
      </div>
    </Dialog>
  )
}
