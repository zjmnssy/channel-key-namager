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
import type { TFunction } from 'i18next'

const REVOKE_DELAY_MS = 1000
const INVALID_FILE_NAME_CHARS = /[/\\:*?"<>|]/g

function siteLabel(siteNames: string[], t: TFunction): string {
  if (siteNames.length === 0) return t('All Sites')
  if (siteNames.length > 1) return t('Multiple Sites')
  return siteNames[0].replace(INVALID_FILE_NAME_CHARS, '_')
}

export function receivableBillFileName(
  siteNames: string[],
  startDate: number,
  endDate: number,
  t: TFunction
): string {
  return `${siteLabel(siteNames, t)}-${t('Receivable Bills')}-${startDate}-${endDate}.xlsx`
}

export function downloadBlob(blob: Blob, fileName: string): void {
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = fileName
  // Some browsers only start the download for an anchor that is in the page,
  // and may read the object URL after click() returns.
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  setTimeout(() => URL.revokeObjectURL(url), REVOKE_DELAY_MS)
}
