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
import { useFormContext } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Combobox } from '@/components/ui/combobox'
import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { UNKNOWN_PROVIDER_VALUE } from '@/features/payable-bills/constants'
import { usePayableBillOptions } from '@/features/payable-bills/hooks/use-payable-bill-options'

import type { RuleFormValues } from '../lib/rule-form'

/** The channel supplier the rule assigns the channels to. */
export function MappingRuleProviderField() {
  const { t } = useTranslation()
  const form = useFormContext<RuleFormValues>()
  const options = usePayableBillOptions()
  // A rule always has a provider, so the unknown one is no choice here.
  const providerOptions = options.providerOptions.filter(
    (option) => option.value !== UNKNOWN_PROVIDER_VALUE
  )

  return (
    <FormField
      control={form.control}
      name='providerId'
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t('Provider')}</FormLabel>
          <FormControl>
            <Combobox
              options={providerOptions}
              // Shown once the providers are loaded, never as a bare ID.
              value={
                providerOptions.some((option) => option.value === field.value)
                  ? field.value
                  : null
              }
              onValueChange={(value) => field.onChange(value ?? '')}
              emptyText={t('No providers available')}
            />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}
