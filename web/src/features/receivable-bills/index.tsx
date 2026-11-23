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
import { getRouteApi } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'

import { DailyBillsTable } from './components/daily-bills-table'

const route = getRouteApi('/_authenticated/receivable-bills/')

export function ReceivableBills() {
  const { t } = useTranslation()
  const navigate = route.useNavigate()
  const tab = route.useSearch().tab === 'daily' ? 'daily' : 'trend'

  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>{t('Receivable Bills')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='flex h-full min-h-0 flex-col gap-4'>
          <Tabs
            value={tab}
            onValueChange={(value) => {
              if (value === 'trend' || value === 'daily') {
                void navigate({ search: (prev) => ({ ...prev, tab: value }) })
              }
            }}
          >
            <TabsList className='max-w-full flex-wrap justify-start group-data-horizontal/tabs:h-auto'>
              <TabsTrigger value='trend'>{t('Consumption Trend')}</TabsTrigger>
              <TabsTrigger value='daily'>{t('Daily Bills')}</TabsTrigger>
            </TabsList>
          </Tabs>
          <div className='min-h-0 flex-1'>
            {tab === 'daily' ? <DailyBillsTable /> : null}
          </div>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
