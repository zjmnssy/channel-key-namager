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

import { ChannelPushDialogs } from './components/channel-push-dialogs'
import { ChannelPushPrimaryButtons } from './components/channel-push-primary-buttons'
import { ChannelPushProvider } from './components/channel-push-provider'
import { RunsTable } from './components/runs-table'
import { TasksTable } from './components/tasks-table'

const route = getRouteApi('/_authenticated/channel-push/')

export function ChannelPush() {
  const { t } = useTranslation()
  const navigate = route.useNavigate()
  const tab = route.useSearch().tab === 'runs' ? 'runs' : 'tasks'

  return (
    <ChannelPushProvider>
      <SectionPageLayout fixedContent>
        <SectionPageLayout.Title>{t('Channel Push')}</SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          {tab === 'tasks' && <ChannelPushPrimaryButtons />}
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='flex h-full min-h-0 flex-col gap-4'>
            <Tabs
              value={tab}
              onValueChange={(value) => {
                if (value === 'tasks' || value === 'runs') {
                  void navigate({ search: (prev) => ({ ...prev, tab: value }) })
                }
              }}
            >
              <TabsList className='max-w-full flex-wrap justify-start group-data-horizontal/tabs:h-auto'>
                <TabsTrigger value='tasks'>{t('Task Management')}</TabsTrigger>
                <TabsTrigger value='runs'>{t('Task Results')}</TabsTrigger>
              </TabsList>
            </Tabs>
            <div className='min-h-0 flex-1'>
              {tab === 'tasks' ? <TasksTable /> : <RunsTable />}
            </div>
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <ChannelPushDialogs />
    </ChannelPushProvider>
  )
}
