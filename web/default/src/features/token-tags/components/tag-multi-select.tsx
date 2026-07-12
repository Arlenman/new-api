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
import { Check, ChevronsUpDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from '@/components/ui/command'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { cn } from '@/lib/utils'

export interface TagMultiSelectOption {
  value: string
  label: string
}

interface TagMultiSelectProps {
  label: string
  emptyLabel: string
  options: TagMultiSelectOption[]
  selected: string[]
  onChange: (selected: string[]) => void
}

export function TagMultiSelect({
  label,
  emptyLabel,
  options,
  selected,
  onChange,
}: TagMultiSelectProps) {
  const { t } = useTranslation()
  const selectedValues = new Set(selected)
  const optionLabels = new Map(
    options.map((option) => [option.value, option.label])
  )
  let valueLabel = emptyLabel
  if (selected.length === 1) {
    valueLabel = optionLabels.get(selected[0]) || selected[0]
  } else if (selected.length > 1) {
    valueLabel = t('{{count}} tags selected', { count: selected.length })
  }

  const toggle = (value: string) => {
    const next = new Set(selectedValues)
    if (next.has(value)) {
      next.delete(value)
    } else {
      next.add(value)
    }
    onChange([...next].sort((a, b) => a.localeCompare(b)))
  }

  return (
    <Popover>
      <PopoverTrigger
        render={
          <Button
            type='button'
            variant='outline'
            className='w-56 justify-between font-normal'
          />
        }
      >
        <span className='min-w-0 truncate'>
          <span className='text-muted-foreground'>{label}: </span>
          {valueLabel}
        </span>
        <ChevronsUpDown className='text-muted-foreground size-4 shrink-0' />
      </PopoverTrigger>
      <PopoverContent className='w-64 p-0' align='start'>
        <Command>
          <CommandInput placeholder={label} />
          <CommandList>
            <CommandEmpty>{t('No results found.')}</CommandEmpty>
            <CommandGroup>
              {options.map((option) => {
                const isSelected = selectedValues.has(option.value)
                return (
                  <CommandItem
                    key={option.value}
                    value={option.value}
                    keywords={[option.label]}
                    onSelect={() => toggle(option.value)}
                  >
                    <span
                      className={cn(
                        'border-primary flex size-4 items-center justify-center rounded-sm border',
                        isSelected
                          ? 'bg-primary text-primary-foreground'
                          : 'opacity-50 [&_svg]:invisible'
                      )}
                    >
                      <Check className='size-3.5' />
                    </span>
                    <span className='truncate'>{option.label}</span>
                  </CommandItem>
                )
              })}
            </CommandGroup>
            {selected.length > 0 && (
              <>
                <CommandSeparator />
                <CommandGroup>
                  <CommandItem
                    className='justify-center'
                    onSelect={() => onChange([])}
                  >
                    {t('Clear filters')}
                  </CommandItem>
                </CommandGroup>
              </>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
