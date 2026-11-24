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
import { useIsFetching, useQueryClient } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import { RefreshCw } from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'

import { ChannelUsagesTable } from './components/channel-usages-table'
import { PullTasksTable } from './components/pull-tasks-table'
import { PAYABLE_BILL_QUERY_KEYS } from './constants'

const route = getRouteApi('/_authenticated/payable-bills/')

export function PayableBills() {
  const { t } = useTranslation()
  const navigate = route.useNavigate()
  const queryClient = useQueryClient()
  const fetching = useIsFetching({ queryKey: PAYABLE_BILL_QUERY_KEYS.all }) > 0
  const tab = route.useSearch().tab ?? 'pulls'

  // The provider bills tab is filled in by a later change.
  let content: ReactNode = <EmptyState title={t('Coming Soon!')} />
  if (tab === 'pulls') {
    content = <PullTasksTable />
  } else if (tab === 'usages') {
    content = <ChannelUsagesTable />
  }

  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>{t('Payable Bills')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        {/* The page does not poll; pull progress is reloaded on demand. */}
        <Button
          variant='outline'
          size='sm'
          disabled={fetching}
          onClick={() =>
            void queryClient.invalidateQueries({
              queryKey: PAYABLE_BILL_QUERY_KEYS.all,
            })
          }
        >
          <RefreshCw
            data-icon='inline-start'
            className={cn('size-3.5', fetching && 'animate-spin')}
            aria-hidden='true'
          />
          {t('Refresh')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='flex h-full min-h-0 flex-col gap-4'>
          <Tabs
            value={tab}
            onValueChange={(value) => {
              if (
                value === 'pulls' ||
                value === 'usages' ||
                value === 'providers'
              ) {
                // Each tab pages on its own; the date, site and provider
                // filters stay shared between details and provider bills.
                void navigate({
                  search: (prev) => ({ ...prev, tab: value, page: undefined }),
                })
              }
            }}
          >
            <TabsList className='max-w-full flex-wrap justify-start group-data-horizontal/tabs:h-auto'>
              <TabsTrigger value='pulls'>
                {t('Channel Usage Pulls')}
              </TabsTrigger>
              <TabsTrigger value='usages'>
                {t('Channel Usage Details')}
              </TabsTrigger>
              <TabsTrigger value='providers'>{t('Provider Bills')}</TabsTrigger>
            </TabsList>
          </Tabs>
          <div className='min-h-0 flex-1'>{content}</div>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
