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
import { downloadBlob } from '@/features/receivable-bills/lib/export'
import { handleServerError } from '@/lib/handle-server-error'

import { exportPayableProviderBills, exportPayableUsages } from '../api'
import { channelUsageFileName, payableBillFileName } from '../lib/export'
import type { PayableBillFilter } from '../types'

// Each tab exports its own list under the same filter.
const EXPORTS = {
  usages: { request: exportPayableUsages, fileName: channelUsageFileName },
  providers: {
    request: exportPayableProviderBills,
    fileName: payableBillFileName,
  },
} as const

type ExportPayableButtonProps = {
  kind: 'usages' | 'providers'
  filter: PayableBillFilter
}

export function ExportPayableButton(props: ExportPayableButtonProps) {
  const { t } = useTranslation()
  const target = EXPORTS[props.kind]

  const mutation = useMutation({
    mutationFn: () => target.request(props.filter),
    onSuccess: (blob) =>
      downloadBlob(
        blob,
        target.fileName(props.filter.start_date, props.filter.end_date, t)
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
