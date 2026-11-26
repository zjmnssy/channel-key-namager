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
import { useQuery } from '@tanstack/react-query'
import { useEffect, useId } from 'react'
import { useFormContext, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { usePayableBillOptions } from '@/features/payable-bills/hooks/use-payable-bill-options'
import { requireServerSuccess } from '@/lib/server-error-message'

import { listProviderMappingRules } from '../api'
import {
  PROVIDER_MAPPING_QUERY_KEYS,
  RULE_TYPE_LABEL_KEYS,
  RULE_TYPES,
} from '../constants'
import type { RuleFormValues } from '../lib/rule-form'
import { ChannelSearchField } from './channel-search-field'

type MappingRuleTargetFieldsProps = {
  disabled: boolean
}

/** The site, rule type and site channel of a new rule. */
export function MappingRuleTargetFields(props: MappingRuleTargetFieldsProps) {
  const { t } = useTranslation()
  const id = useId()
  const form = useFormContext<RuleFormValues>()
  const options = usePayableBillOptions()
  const siteId = useWatch({ control: form.control, name: 'siteId' })
  const ruleType = useWatch({ control: form.control, name: 'ruleType' })
  const siteItems = options.sites
    .filter((site) => !site.deleted)
    .map((site) => ({ value: String(site.id), label: site.name }))

  // A site has at most one site-level rule. Until it is known, or when the
  // check fails, every type stays open: the server refuses a conflict anyway.
  const siteRuleQuery = useQuery({
    queryKey: PROVIDER_MAPPING_QUERY_KEYS.siteRules(Number(siteId)),
    queryFn: async () =>
      requireServerSuccess(
        await listProviderMappingRules({
          p: 1,
          page_size: 1,
          site_ids: [Number(siteId)],
          types: ['all', 'unresolved'],
        })
      ).data?.items?.[0] ?? null,
    enabled: siteId !== '',
    meta: { errorToast: false },
  })
  const siteRule = siteRuleQuery.data ?? null

  useEffect(() => {
    if (siteRule && form.getValues('ruleType') !== 'channel') {
      form.setValue('ruleType', 'channel')
    }
  }, [siteRule, form])

  return (
    <>
      <FormField
        control={form.control}
        name='siteId'
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('Third-party site')}</FormLabel>
            <Select
              value={field.value || null}
              onValueChange={(value) => {
                const next = value ?? ''
                if (next === field.value) return
                field.onChange(next)
                // A channel belongs to the site it was chosen from.
                form.setValue('channelId', '')
                form.setValue('channelName', '')
              }}
              items={siteItems}
              disabled={props.disabled}
            >
              <FormControl>
                <SelectTrigger className='w-full'>
                  <SelectValue placeholder={t('Select a site')} />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                {siteItems.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name='ruleType'
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('Rule Type')}</FormLabel>
            <RadioGroup
              value={field.value}
              onValueChange={(value) => {
                const type = RULE_TYPES.find((item) => item === value)
                if (type) field.onChange(type)
              }}
              aria-label={t('Rule Type')}
              disabled={props.disabled}
              className='flex flex-wrap gap-x-5 gap-y-3'
            >
              {RULE_TYPES.map((type) => (
                <div key={type} className='flex items-center gap-2'>
                  <RadioGroupItem
                    value={type}
                    id={`${id}-${type}`}
                    disabled={siteRule !== null && type !== 'channel'}
                  />
                  <Label htmlFor={`${id}-${type}`}>
                    {t(RULE_TYPE_LABEL_KEYS[type])}
                  </Label>
                </div>
              ))}
            </RadioGroup>
            {siteRule && (
              <p className='text-muted-foreground text-sm'>
                {t(
                  'This site already has a "{{type}}" rule. Edit the existing rule instead.',
                  { type: t(RULE_TYPE_LABEL_KEYS[siteRule.rule_type]) }
                )}
              </p>
            )}
            <FormMessage />
          </FormItem>
        )}
      />
      {ruleType === 'channel' && (
        <FormField
          control={form.control}
          name='channelId'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Site Channel')}</FormLabel>
              <FormControl>
                <ChannelSearchField
                  siteId={Number(siteId)}
                  disabled={props.disabled || siteId === ''}
                  value={field.value}
                  onValueChange={(value, channel) => {
                    field.onChange(value)
                    form.setValue(
                      'channelName',
                      channel?.remote_channel_name ?? ''
                    )
                  }}
                />
              </FormControl>
              <FormDescription>
                {t(
                  'By name, only channels of the latest pull are found. Enter the full channel ID to find earlier ones.'
                )}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      )}
    </>
  )
}
