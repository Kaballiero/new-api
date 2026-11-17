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
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { Combobox } from '@/components/ui/combobox'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import {
  OPENROUTER_SYNC_BILLING_STATE_FILTERS,
  OPENROUTER_SYNC_HOLD_REASONS,
} from '../constants'
import { LogsFilterField } from './logs-filter-toolbar'

const ALL_BILLING_STATES = 'all'

const holdReasonOptions = OPENROUTER_SYNC_HOLD_REASONS.map((reason) => ({
  label: reason,
  value: reason,
}))

interface OpenRouterSyncTaskFiltersProps {
  billingState?: string
  holdReason?: string
  onBillingStateChange: (value: string) => void
  onHoldReasonChange: (value: string) => void
  onKeyDown: (event: React.KeyboardEvent) => void
}

/** Admin filters of the manual-hold queue of OpenRouter synchronous tasks. */
export function OpenRouterSyncTaskFilters(
  props: OpenRouterSyncTaskFiltersProps
) {
  const { t } = useTranslation()
  const billingStateItems = useMemo(
    () => [
      { value: ALL_BILLING_STATES, label: t('All billing states') },
      ...OPENROUTER_SYNC_BILLING_STATE_FILTERS.map((state) => ({
        value: state.value,
        label: t(state.label),
      })),
    ],
    [t]
  )

  return (
    <>
      <LogsFilterField>
        <Select
          items={billingStateItems}
          value={props.billingState || ALL_BILLING_STATES}
          onValueChange={(value) =>
            props.onBillingStateChange(
              value === null || value === ALL_BILLING_STATES ? '' : value
            )
          }
        >
          <SelectTrigger aria-label={t('OpenRouter billing state')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent alignItemWithTrigger={false}>
            <SelectGroup>
              {billingStateItems.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
      </LogsFilterField>
      <LogsFilterField>
        <Combobox
          options={holdReasonOptions}
          allowCustomValue
          aria-label={t('Hold reason')}
          placeholder={t('Hold reason')}
          emptyText={t('No results found')}
          className='h-8 min-w-0 font-mono text-sm leading-5'
          value={props.holdReason || ''}
          onValueChange={(value) => props.onHoldReasonChange(value ?? '')}
          onKeyDown={props.onKeyDown}
        />
      </LogsFilterField>
    </>
  )
}
