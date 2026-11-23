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
import { useMutation } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { handleServerError } from '@/lib/handle-server-error'

import { exportReceivableBills } from '../api'
import { downloadBlob, receivableBillFileName } from '../lib/export'
import type { DailyBillFilter } from '../types'

type ExportBillsButtonProps = {
  filter: DailyBillFilter
  /** Names of the selected sites, used for the file name. */
  siteNames: string[]
}

export function ExportBillsButton(props: ExportBillsButtonProps) {
  const { t } = useTranslation()

  const mutation = useMutation({
    mutationFn: () => exportReceivableBills(props.filter),
    onSuccess: (blob) =>
      downloadBlob(
        blob,
        receivableBillFileName(
          props.siteNames,
          props.filter.start_date,
          props.filter.end_date,
          t
        )
      ),
    onError: (error) => handleServerError(error),
  })

  return (
    <Button
      variant='outline'
      size='sm'
      disabled={mutation.isPending}
      onClick={() => mutation.mutate()}
    >
      {t('Export Excel')}
    </Button>
  )
}
