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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Form } from '@/components/ui/form'
import { PAYABLE_BILL_QUERY_KEYS } from '@/features/payable-bills/constants'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

import {
  createProviderMappingRule,
  previewProviderMappingRule,
  updateProviderMappingRule,
} from '../api'
import { PROVIDER_MAPPING_QUERY_KEYS } from '../constants'
import {
  createRuleFormDefaults,
  ruleFormSchema,
  ruleToFormValues,
  toRuleCreatePayload,
  toRulePreviewQuery,
  toRuleUpdatePayload,
  type RuleFormValues,
} from '../lib/rule-form'
import type { ProviderMappingRule } from '../types'
import { MappingRuleApplyFields } from './mapping-rule-apply-fields'
import { MappingRuleProviderField } from './mapping-rule-provider-field'
import { MappingRuleSummary } from './mapping-rule-summary'
import { MappingRuleTargetFields } from './mapping-rule-target-fields'

type MappingRuleMutateDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentRow?: ProviderMappingRule
}

const FORM_ID = 'provider-mapping-rule-form'

export function MappingRuleMutateDialog(props: MappingRuleMutateDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [confirmOpen, setConfirmOpen] = useState(false)

  const form = useForm<RuleFormValues>({
    resolver: zodResolver(ruleFormSchema),
    defaultValues: props.currentRow
      ? ruleToFormValues(props.currentRow)
      : createRuleFormDefaults(),
  })

  useEffect(() => {
    form.reset(
      props.open && props.currentRow
        ? ruleToFormValues(props.currentRow)
        : createRuleFormDefaults()
    )
  }, [props.open, props.currentRow, form])

  // Counts the details a backfill would change, for the confirmation.
  const preview = useMutation({
    mutationFn: async (values: RuleFormValues) =>
      requireServerSuccess(
        await previewProviderMappingRule(toRulePreviewQuery(values))
      ).data?.rows ?? 0,
    onSuccess: () => setConfirmOpen(true),
    onError: (error) => handleServerError(error),
  })

  const save = useMutation({
    mutationFn: async (values: RuleFormValues) =>
      requireServerSuccess(
        props.currentRow
          ? await updateProviderMappingRule(
              props.currentRow.id,
              toRuleUpdatePayload(values)
            )
          : await createProviderMappingRule(toRuleCreatePayload(values))
      ),
    onSuccess: (res, values) => {
      toast.success(
        values.applyEnabled
          ? t('Mapping rule saved, {{count}} details changed', {
              count: res.data?.applied_rows ?? 0,
            })
          : t('Mapping rule saved')
      )
      setConfirmOpen(false)
      props.onOpenChange(false)
    },
    onError: (error) => {
      setConfirmOpen(false)
      handleServerError(error)
    },
    // A proxy timeout can hide a save that went through, so refresh then too.
    onSettled: () => {
      void queryClient.invalidateQueries({
        queryKey: PROVIDER_MAPPING_QUERY_KEYS.all,
      })
      void queryClient.invalidateQueries({
        queryKey: PAYABLE_BILL_QUERY_KEYS.all,
      })
    },
  })

  // The preview belongs to saving: the dialog must not close under it.
  const busy = preview.isPending || save.isPending
  const previewRows = preview.data ?? 0

  return (
    <Dialog
      open={props.open}
      onOpenChange={(open) => {
        if (!busy) props.onOpenChange(open)
      }}
      title={
        props.currentRow ? t('Edit Mapping Rule') : t('Create Mapping Rule')
      }
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            disabled={busy}
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button type='submit' form={FORM_ID} disabled={busy}>
            {busy ? t('Saving...') : t('Save')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <form
          id={FORM_ID}
          onSubmit={form.handleSubmit((values) => {
            if (values.applyEnabled) preview.mutate(values)
            else save.mutate(values)
          })}
          className='space-y-4'
        >
          {props.currentRow ? (
            <MappingRuleSummary rule={props.currentRow} />
          ) : (
            <MappingRuleTargetFields />
          )}
          <MappingRuleProviderField />
          <MappingRuleApplyFields />
        </form>
      </Form>
      <ConfirmDialog
        destructive
        open={confirmOpen}
        onOpenChange={(open) => {
          if (!save.isPending) setConfirmOpen(open)
        }}
        title={t('Apply to details already pulled?')}
        desc={
          previewRows > 0
            ? t(
                'This will change {{count}} details, and provider bills will change accordingly. This cannot be undone.',
                { count: previewRows }
              )
            : t(
                'No details in the selected dates need to change. Only the rule will be saved.'
              )
        }
        confirmText={save.isPending ? t('Saving...') : t('Save')}
        isLoading={save.isPending}
        handleConfirm={() => {
          if (preview.variables) save.mutate(preview.variables)
        }}
      />
    </Dialog>
  )
}
