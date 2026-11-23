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
/** Tooltip text from the backend's decimal string; a missing point shows nothing, never 0. */
export function formatTrendTooltip(
  point: { raw: string | null; provisional: boolean },
  t: (key: string) => string
): string {
  if (point.raw == null) return ''
  const amount = `$${point.raw}`
  return point.provisional ? `${amount} (${t('Provisional data')})` : amount
}
