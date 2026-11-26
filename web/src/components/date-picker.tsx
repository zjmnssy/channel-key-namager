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
import { Calendar as CalendarIcon } from 'lucide-react'
import { enUS, fr, ja, ru, vi, zhCN, zhTW } from 'react-day-picker/locale'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Calendar } from '@/components/ui/calendar'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import dayjs from '@/lib/dayjs'

const calendarLocales = {
  en: enUS,
  zhCN,
  zhTW,
  fr,
  ru,
  ja,
  vi,
} as const

// eslint-disable-next-line react-refresh/only-export-components
export function getCalendarLocale(language: string) {
  return calendarLocales[language as keyof typeof calendarLocales] ?? enUS
}

type DatePickerProps = {
  selected: Date | undefined
  onSelect: (date: Date | undefined) => void
  placeholder?: string
  /** Earliest selectable date; defaults to 1900-01-01 */
  minDate?: Date
  /** Latest selectable date; defaults to now */
  maxDate?: Date
  /** Set by FormControl, so the field's label and messages describe the trigger */
  id?: string
  'aria-describedby'?: string
  'aria-invalid'?: boolean
}

export function DatePicker({
  selected,
  onSelect,
  placeholder,
  minDate,
  maxDate,
  id,
  'aria-describedby': ariaDescribedBy,
  'aria-invalid': ariaInvalid,
}: DatePickerProps) {
  const { t, i18n } = useTranslation()
  const placeholderText = placeholder ?? t('Pick a date')
  const calendarLocale = getCalendarLocale(i18n.language)
  return (
    <Popover>
      <PopoverTrigger
        render={
          <Button
            id={id}
            aria-describedby={ariaDescribedBy}
            aria-invalid={ariaInvalid}
            variant='outline'
            data-empty={!selected}
            className='data-[empty=true]:text-muted-foreground w-[240px] justify-start text-start font-normal'
          />
        }
      >
        {selected ? (
          dayjs(selected).format('YYYY-MM-DD')
        ) : (
          <span>{placeholderText}</span>
        )}
        <CalendarIcon className='ms-auto h-4 w-4 opacity-50' />
      </PopoverTrigger>
      <PopoverContent className='w-auto p-0'>
        <Calendar
          mode='single'
          captionLayout='dropdown'
          selected={selected}
          onSelect={onSelect}
          locale={calendarLocale}
          disabled={(date: Date) =>
            date > (maxDate ?? new Date()) ||
            date < (minDate ?? new Date('1900-01-01'))
          }
        />
      </PopoverContent>
    </Popover>
  )
}
