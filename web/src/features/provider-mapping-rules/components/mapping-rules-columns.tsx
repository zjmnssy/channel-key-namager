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
import type { ColumnDef } from '@tanstack/react-table'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { TableId } from '@/components/table-id'
import { TruncatedText } from '@/components/truncated-text'
import { formatTimestampToDate } from '@/lib/format'

import { RULE_TYPE_LABEL_KEYS } from '../constants'
import type { ProviderMappingRule } from '../types'
import { MappingRuleRowActions } from './mapping-rule-row-actions'

const EMPTY = '—'

/** "<channel id> <name>"; the name is left out when the channel has none. */
function channelText(rule: ProviderMappingRule): string {
  if (rule.rule_type !== 'channel') return EMPTY
  return rule.remote_channel_name
    ? `${rule.remote_channel_id} ${rule.remote_channel_name}`
    : String(rule.remote_channel_id)
}

export function useMappingRulesColumns(): ColumnDef<ProviderMappingRule>[] {
  const { t } = useTranslation()

  return useMemo<ColumnDef<ProviderMappingRule>[]>(
    () => [
      {
        accessorKey: 'id',
        header: t('ID'),
        cell: ({ row }) => <TableId value={row.original.id} />,
      },
      {
        accessorKey: 'site_name',
        header: t('Third-party site'),
        meta: { mobileTitle: true },
        cell: ({ row }) => (
          <div className='flex items-center gap-2'>
            <TruncatedText
              text={row.original.site_name}
              maxWidth='max-w-[200px]'
            />
            {row.original.site_deleted && (
              <StatusBadge
                label={t('Site deleted')}
                variant='neutral'
                copyable={false}
              />
            )}
          </div>
        ),
      },
      {
        accessorKey: 'rule_type',
        header: t('Rule Type'),
        cell: ({ row }) => t(RULE_TYPE_LABEL_KEYS[row.original.rule_type]),
      },
      {
        id: 'channel',
        header: t('Site Channel'),
        cell: ({ row }) => (
          <TruncatedText
            text={channelText(row.original)}
            maxWidth='max-w-[260px]'
          />
        ),
      },
      {
        accessorKey: 'provider_name',
        header: t('Provider'),
        cell: ({ row }) => (
          <div className='flex items-center gap-2'>
            <TruncatedText
              text={row.original.provider_name}
              maxWidth='max-w-[160px]'
            />
            {!row.original.provider_valid && (
              <StatusBadge
                label={t('No longer valid')}
                variant='warning'
                copyable={false}
              />
            )}
          </div>
        ),
      },
      {
        accessorKey: 'updated_time',
        header: t('Updated At'),
        cell: ({ row }) => formatTimestampToDate(row.original.updated_time),
      },
      {
        id: 'actions',
        header: t('Actions'),
        cell: ({ row }) => <MappingRuleRowActions row={row} />,
      },
    ],
    [t]
  )
}
