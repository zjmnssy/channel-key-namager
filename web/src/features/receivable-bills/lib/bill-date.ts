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
const BEIJING_OFFSET_MS = 8 * 60 * 60 * 1000

function pad(n: number, width: number): string {
  return String(n).padStart(width, '0')
}

/** Today's date in Beijing time (UTC+8) as YYYYMMDD, whatever the browser zone. */
export function beijingToday(now: Date = new Date()): number {
  const beijing = new Date(now.getTime() + BEIJING_OFFSET_MS)
  return (
    beijing.getUTCFullYear() * 10000 +
    (beijing.getUTCMonth() + 1) * 100 +
    beijing.getUTCDate()
  )
}

export function monthStart(date: number): number {
  return Math.floor(date / 100) * 100 + 1
}

/** Midnight of the bill date in the browser's zone, for the calendar. */
export function billDateToLocalDate(date: number): Date {
  return new Date(
    Math.floor(date / 10000),
    (Math.floor(date / 100) % 100) - 1,
    date % 100
  )
}

export function localDateToBillDate(date: Date): number {
  return (
    date.getFullYear() * 10000 + (date.getMonth() + 1) * 100 + date.getDate()
  )
}

export function formatBillDate(date: number): string {
  return `${pad(Math.floor(date / 10000), 4)}-${pad(Math.floor(date / 100) % 100, 2)}-${pad(date % 100, 2)}`
}
