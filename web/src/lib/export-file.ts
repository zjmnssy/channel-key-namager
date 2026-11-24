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
import i18n from 'i18next'

import { toIntlLocale } from '@/i18n/languages'
import { api } from '@/lib/api'
import { createServerError } from '@/lib/server-error-message'

/** A list query parameter as comma-separated values; an empty list is left out. */
export function joinQueryList(
  values?: (string | number)[]
): string | undefined {
  return values && values.length > 0 ? values.join(',') : undefined
}

/**
 * Downloads a file the server generates, such as an xlsx export. Parameter
 * errors come back as a JSON body instead and are thrown as the server error;
 * `fallbackMessage` is used when that body cannot be read.
 */
export async function fetchExportFile(
  url: string,
  params: Record<string, unknown>,
  fallbackMessage: string
): Promise<Blob> {
  const res = await api.get<Blob>(url, {
    params,
    headers: {
      // Without a saved language preference the file follows the interface.
      'Accept-Language': toIntlLocale(i18n.resolvedLanguage || i18n.language),
    },
    responseType: 'blob',
    disableDuplicate: true,
    // Only silences the session-expired toast; the caller reports failures.
    skipErrorHandler: true,
  })
  if (res.data.type.includes('json')) {
    let body: unknown
    try {
      body = JSON.parse(await res.data.text())
    } catch {
      throw new Error(fallbackMessage)
    }
    throw createServerError(body)
  }
  return res.data
}
