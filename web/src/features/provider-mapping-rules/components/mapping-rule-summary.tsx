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
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'

import { RULE_TYPE_LABEL_KEYS } from '../constants'
import { siteChannelLabel } from '../lib/site-channel'
import type { ProviderMappingRule } from '../types'

type MappingRuleSummaryProps = {
  rule: ProviderMappingRule
}

/** The parts of a saved rule that cannot change; another target needs a new rule. */
export function MappingRuleSummary(props: MappingRuleSummaryProps) {
  const { t } = useTranslation()

  return (
    <dl className='grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm'>
      <dt className='text-muted-foreground'>{t('Third-party site')}</dt>
      <dd className='flex flex-wrap items-center gap-2 break-all'>
        {props.rule.site_name}
        {props.rule.site_deleted && (
          <StatusBadge
            label={t('Site deleted')}
            variant='neutral'
            copyable={false}
          />
        )}
      </dd>
      <dt className='text-muted-foreground'>{t('Rule Type')}</dt>
      <dd>{t(RULE_TYPE_LABEL_KEYS[props.rule.rule_type])}</dd>
      {props.rule.rule_type === 'channel' && (
        <>
          <dt className='text-muted-foreground'>{t('Site Channel')}</dt>
          <dd className='break-all'>
            {siteChannelLabel(
              props.rule.remote_channel_id,
              props.rule.remote_channel_name
            )}
          </dd>
        </>
      )}
    </dl>
  )
}
