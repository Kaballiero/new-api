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
import type { Control } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'

import type { ChannelFormValues } from '../../../lib/channel-form'
import { OpenRouterPolicyTable } from './openrouter-policy-table'

type OpenRouterReserveFieldProps = {
  control: Control<ChannelFormValues>
  disabled: boolean
  modelOptions: string[]
}

export function OpenRouterReserveField(props: OpenRouterReserveFieldProps) {
  const { t } = useTranslation()
  return (
    <>
      <FormField
        control={props.control}
        name='openrouter_reservation_source'
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('OpenRouter reservation sources')}</FormLabel>
            <FormDescription>
              {t(
                'Map exact upstream model IDs to upstream or configured_price. Default: upstream. configured_price uses the configured NewAPI price or expression only for supported operations. Endpoint and provider checks still apply. Final charges use OpenRouter actual cost.'
              )}
            </FormDescription>
            <FormControl>
              <OpenRouterPolicyTable
                kind='source'
                label={t('OpenRouter reservation sources')}
                value={field.value || ''}
                onChange={field.onChange}
                onBlur={field.onBlur}
                disabled={props.disabled}
                modelOptions={props.modelOptions}
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={props.control}
        name='openrouter_model_reserve_usd'
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('OpenRouter model reserves (USD)')}</FormLabel>
            <FormDescription>
              {t(
                'Map exact upstream model IDs to positive minimum holds in USD. Supported contracts include dedicated Audio, Rerank, Chat image output, streaming Chat audio output and hosted web search (web_search_options). Holds never reduce a higher upstream estimate and are not a spending limit. Full actual cost is charged, including excess. An empty table disables these holds. Legacy Sonar settings migrate on save.'
              )}
            </FormDescription>
            <FormControl>
              <OpenRouterPolicyTable
                kind='reserve'
                label={t('OpenRouter model reserves (USD)')}
                value={field.value || ''}
                onChange={field.onChange}
                onBlur={field.onBlur}
                disabled={props.disabled}
                modelOptions={props.modelOptions}
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  )
}
