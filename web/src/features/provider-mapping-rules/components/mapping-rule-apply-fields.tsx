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
import { TriangleAlert } from 'lucide-react'
import { useId } from 'react'
import { useFormContext, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Checkbox } from '@/components/ui/checkbox'
import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Label } from '@/components/ui/label'
import { usePayableBillOptions } from '@/features/payable-bills/hooks/use-payable-bill-options'
import { BillDateRangePicker } from '@/features/receivable-bills/components/bill-date-range-picker'
import { beijingYesterday } from '@/features/receivable-bills/lib/bill-date'

import type { RuleFormValues } from '../lib/rule-form'

/** Whether saving also changes the details already pulled, and on which bill dates. */
export function MappingRuleApplyFields() {
  const { t } = useTranslation()
  const datesLabelId = useId()
  const form = useFormContext<RuleFormValues>()
  const options = usePayableBillOptions()
  const applyEnabled = useWatch({ control: form.control, name: 'applyEnabled' })
  const applyEnd = useWatch({ control: form.control, name: 'applyEnd' })
  const providerId = useWatch({ control: form.control, name: 'providerId' })
  // The changed details take the payable discount of the chosen provider.
  const discountMissing =
    options.providers.find((provider) => String(provider.id) === providerId)
      ?.payable_discount === null

  return (
    <>
      <FormField
        control={form.control}
        name='applyEnabled'
        render={({ field }) => (
          <FormItem className='flex items-center gap-2'>
            <FormControl>
              <Checkbox
                checked={field.value}
                onCheckedChange={(checked) => field.onChange(checked === true)}
              />
            </FormControl>
            <FormLabel className='font-normal'>
              {t('Also apply to details already pulled')}
            </FormLabel>
          </FormItem>
        )}
      />
      {applyEnabled && (
        <FormField
          control={form.control}
          name='applyStart'
          render={({ field }) => (
            <FormItem role='group' aria-labelledby={datesLabelId}>
              <Label id={datesLabelId}>{t('Bill Date')}</Label>
              <BillDateRangePicker
                start={field.value}
                end={applyEnd}
                max={beijingYesterday()}
                onChange={(start, end) => {
                  form.setValue('applyEnd', end)
                  // Checked at once, so a range that is too long shows before saving.
                  form.setValue('applyStart', start, { shouldValidate: true })
                }}
              />
              <FormMessage />
            </FormItem>
          )}
        />
      )}
      {applyEnabled && discountMissing && (
        <Alert>
          <TriangleAlert aria-hidden='true' />
          <AlertDescription>
            {t(
              'This provider has no payable discount, so the changed details will have no payable amount.'
            )}
          </AlertDescription>
        </Alert>
      )}
    </>
  )
}
