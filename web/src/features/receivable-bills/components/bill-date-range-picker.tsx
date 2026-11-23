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
import { useState } from 'react'
import type { DateRange } from 'react-day-picker'
import { useTranslation } from 'react-i18next'

import { getCalendarLocale } from '@/components/date-picker'
import { Button } from '@/components/ui/button'
import { Calendar } from '@/components/ui/calendar'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'

import {
  billDateToLocalDate,
  formatBillDate,
  localDateToBillDate,
} from '../lib/bill-date'

type BillDateRangePickerProps = {
  start: number // YYYYMMDD
  end: number
  max: number // latest selectable date
  onChange: (start: number, end: number) => void
}

export function BillDateRangePicker(props: BillDateRangePickerProps) {
  const { i18n } = useTranslation()
  // The range under construction: the first click sets only the start.
  const [draft, setDraft] = useState<DateRange | undefined>()
  const committed: DateRange = {
    from: billDateToLocalDate(props.start),
    to: billDateToLocalDate(props.end),
  }

  const handleSelect = (range: DateRange | undefined) => {
    setDraft(range)
    if (range?.from && range.to) {
      props.onChange(
        localDateToBillDate(range.from),
        localDateToBillDate(range.to)
      )
    }
  }

  return (
    <Popover onOpenChange={(open) => !open && setDraft(undefined)}>
      <PopoverTrigger
        render={
          <Button
            variant='outline'
            className='w-full justify-start text-start font-normal sm:w-[280px]'
          />
        }
      >
        {formatBillDate(props.start)} ~ {formatBillDate(props.end)}
        <CalendarIcon className='ms-auto h-4 w-4 opacity-50' />
      </PopoverTrigger>
      <PopoverContent className='w-auto p-0'>
        <Calendar
          mode='range'
          resetOnSelect
          captionLayout='dropdown'
          numberOfMonths={2}
          defaultMonth={committed.from}
          selected={draft ?? committed}
          onSelect={handleSelect}
          locale={getCalendarLocale(i18n.language)}
          disabled={{ after: billDateToLocalDate(props.max) }}
        />
      </PopoverContent>
    </Popover>
  )
}
