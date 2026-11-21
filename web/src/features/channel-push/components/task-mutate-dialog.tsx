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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useId, useMemo } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { LoadingState } from '@/components/loading-state'
import { MultiSelect, type Option } from '@/components/multi-select'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { handleServerError } from '@/lib/handle-server-error'
import {
  createServerError,
  getServerErrorMessage,
  requireServerSuccess,
} from '@/lib/server-error-message'

import {
  createChannelPushTask,
  getChannelPushOptions,
  getChannelPushSiteGroups,
  updateChannelPushTask,
} from '../api'
import { CHANNEL_PUSH_QUERY_KEYS } from '../constants'
import {
  TASK_FORM_DEFAULTS,
  taskFormSchema,
  taskToFormValues,
  toTaskPayload,
  type TaskFormValues,
} from '../lib/task-form'
import type { ChannelPushTask } from '../types'

type TaskMutateDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentTask?: ChannelPushTask
}

const FORM_ID = 'channel-push-task-form'

// Backend validation fields (model.ChannelPushFieldError) and the form field
// each one is shown under.
const SERVER_FIELDS: Record<string, keyof TaskFormValues> = {
  site_id: 'siteId',
  supplier_user_ids: 'supplierIds',
  task_type: 'taskType',
  interval_minutes: 'intervalMinutes',
  target_group: 'targetGroup',
}

export function TaskMutateDialog(props: TaskMutateDialogProps) {
  const { t } = useTranslation()
  const id = useId()
  const queryClient = useQueryClient()
  const isUpdate = props.currentTask != null

  const form = useForm<TaskFormValues>({
    resolver: zodResolver(taskFormSchema),
    defaultValues: TASK_FORM_DEFAULTS,
  })

  useEffect(() => {
    form.reset(
      props.open && props.currentTask
        ? taskToFormValues(props.currentTask)
        : TASK_FORM_DEFAULTS
    )
  }, [props.open, props.currentTask, form])

  const siteId = useWatch({ control: form.control, name: 'siteId' })
  const taskType = useWatch({ control: form.control, name: 'taskType' })
  const targetGroup = useWatch({ control: form.control, name: 'targetGroup' })

  const optionsQuery = useQuery({
    queryKey: CHANNEL_PUSH_QUERY_KEYS.options,
    queryFn: async () =>
      requireServerSuccess(await getChannelPushOptions()).data ?? {
        sites: [],
        suppliers: [],
      },
    enabled: props.open,
  })

  // The backend already retries this read, so a failure is shown at once.
  const groupsQuery = useQuery({
    queryKey: ['channel-push', 'site-groups', siteId],
    queryFn: async () =>
      requireServerSuccess(await getChannelPushSiteGroups(Number(siteId)))
        .data ?? [],
    enabled: props.open && siteId !== '',
    retry: false,
    meta: { errorToast: false },
  })

  const siteItems = useMemo(() => {
    const items = (optionsQuery.data?.sites ?? []).map((site) => ({
      value: String(site.id),
      label: site.name,
    }))
    const task = props.currentTask
    if (task && !items.some((item) => item.value === String(task.site_id))) {
      items.push({
        value: String(task.site_id),
        label: `${task.site_name} · ${t('Site deleted')}`,
      })
    }
    return items
  }, [optionsQuery.data, props.currentTask, t])

  // A saved supplier who has since lost the role stays selected and marked,
  // so the administrator sees why saving is refused.
  const supplierOptions = useMemo(() => {
    const options: Option[] = (optionsQuery.data?.suppliers ?? []).map(
      (supplier) => ({
        value: String(supplier.id),
        label: `${supplier.username} (#${supplier.id})`,
      })
    )
    if (!optionsQuery.data) return options
    for (const supplier of props.currentTask?.suppliers ?? []) {
      if (options.some((option) => option.value === String(supplier.id))) {
        continue
      }
      options.push({
        value: String(supplier.id),
        label: `${supplier.username} (#${supplier.id}) · ${t('No longer a channel supplier')}`,
      })
    }
    return options
  }, [optionsQuery.data, props.currentTask, t])

  // A selected group the site no longer lists stays an option, and is marked
  // once the list has actually been read.
  const groupItems = useMemo(() => {
    const items = (groupsQuery.data ?? []).map((group) => ({
      value: group,
      label: group,
    }))
    if (targetGroup && !items.some((item) => item.value === targetGroup)) {
      items.push({
        value: targetGroup,
        label: groupsQuery.isSuccess
          ? `${targetGroup} · ${t('No longer on the site')}`
          : targetGroup,
      })
    }
    return items
  }, [groupsQuery.data, groupsQuery.isSuccess, targetGroup, t])

  const mutation = useMutation({
    mutationFn: async (values: TaskFormValues) => {
      const payload = toTaskPayload(values)
      const res = props.currentTask
        ? await updateChannelPushTask(props.currentTask.id, payload)
        : await createChannelPushTask(payload)
      if (!res.success) throw createServerError(res)
      return res
    },
    onSuccess: (res) => {
      // Only an enabled task is queued by saving.
      toast.success(
        res.data?.enabled
          ? t('Saved. The task will start within half a minute')
          : t('Saved successfully')
      )
      void queryClient.invalidateQueries({
        queryKey: CHANNEL_PUSH_QUERY_KEYS.tasks,
      })
      props.onOpenChange(false)
    },
    onError: (error: Error) => {
      const field = (error.cause as { field?: unknown } | undefined)?.field
      if (typeof field === 'string' && Object.hasOwn(SERVER_FIELDS, field)) {
        form.setError(SERVER_FIELDS[field], { message: error.message })
        return
      }
      handleServerError(error)
    },
  })

  const groupsLoading = siteId !== '' && groupsQuery.isFetching
  const groupsFailed = groupsQuery.isError && !groupsQuery.isFetching

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={isUpdate ? t('Edit push task') : t('New push task')}
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button type='submit' form={FORM_ID} disabled={mutation.isPending}>
            {mutation.isPending ? t('Saving...') : t('Save')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <form
          id={FORM_ID}
          onSubmit={form.handleSubmit((values) => mutation.mutate(values))}
          className='space-y-4'
        >
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
                    form.setValue('targetGroup', '')
                  }}
                  items={siteItems}
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
            name='supplierIds'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Provider users')}</FormLabel>
                <FormControl>
                  <MultiSelect
                    options={supplierOptions}
                    selected={field.value}
                    onChange={field.onChange}
                    placeholder={t('Select provider users')}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='taskType'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Task type')}</FormLabel>
                <RadioGroup
                  value={field.value}
                  onValueChange={(value) => {
                    if (value === 'once' || value === 'periodic') {
                      field.onChange(value)
                    }
                  }}
                  aria-label={t('Task type')}
                  className='flex flex-wrap gap-x-5 gap-y-3'
                >
                  {[
                    { value: 'once', label: t('Once') },
                    { value: 'periodic', label: t('Periodic') },
                  ].map((option) => (
                    <div key={option.value} className='flex items-center gap-2'>
                      <RadioGroupItem
                        value={option.value}
                        id={`${id}-${option.value}`}
                      />
                      <Label htmlFor={`${id}-${option.value}`}>
                        {option.label}
                      </Label>
                    </div>
                  ))}
                </RadioGroup>
                <FormMessage />
              </FormItem>
            )}
          />
          {taskType === 'periodic' && (
            <FormField
              control={form.control}
              name='intervalMinutes'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Interval (minutes)')}</FormLabel>
                  <FormControl>
                    <Input {...field} inputMode='numeric' autoComplete='off' />
                  </FormControl>
                  <FormDescription>
                    {t('Runs every N minutes, from 5 to 1440')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          )}
          <FormField
            control={form.control}
            name='targetGroup'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Target group')}</FormLabel>
                <Select
                  value={field.value || null}
                  onValueChange={(value) => field.onChange(value ?? '')}
                  items={groupItems}
                  disabled={siteId === '' || !groupsQuery.isSuccess}
                >
                  <FormControl>
                    <SelectTrigger className='w-full'>
                      <SelectValue placeholder={t('Select a target group')} />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    {groupItems.map((item) => (
                      <SelectItem key={item.value} value={item.value}>
                        {item.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                {groupsLoading && (
                  <LoadingState
                    inline
                    size='sm'
                    message={t('Loading groups...')}
                  />
                )}
                {groupsFailed && (
                  <div className='flex flex-wrap items-center gap-2'>
                    <p role='alert' className='text-destructive text-sm'>
                      {getServerErrorMessage(groupsQuery.error)}
                    </p>
                    <Button
                      type='button'
                      variant='outline'
                      size='sm'
                      onClick={() => void groupsQuery.refetch()}
                    >
                      {t('Retry')}
                    </Button>
                  </div>
                )}
                <FormMessage />
              </FormItem>
            )}
          />
        </form>
      </Form>
    </Dialog>
  )
}
