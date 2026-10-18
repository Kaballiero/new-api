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

import { JsonCodeEditor } from '@/components/json-code-editor'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'

import type { ChannelFormValues } from '../../../lib/channel-form'

type OpenRouterReserveFieldProps = {
  control: Control<ChannelFormValues>
  disabled: boolean
}

export function OpenRouterReserveField(props: OpenRouterReserveFieldProps) {
  const { t } = useTranslation()
  return (
    <FormField
      control={props.control}
      name='openrouter_model_reserve_usd'
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t('OpenRouter model reserves (USD)')}</FormLabel>
          <FormDescription>
            {t(
              'Exact upstream model IDs mapped to minimum holds in USD. Empty object disables the policy. One nonstream text chat response only; tariffs and terminal actual cost are required. Full actual cost is charged, including excess. Legacy Sonar settings are migrated on save.'
            )}
          </FormDescription>
          <FormControl>
            <JsonCodeEditor
              value={field.value || ''}
              onChange={field.onChange}
              name={field.name}
              onBlur={field.onBlur}
              textareaRef={field.ref}
              ariaLabel={t('OpenRouter model reserves (USD)')}
              disabled={props.disabled}
              placeholder='{"perplexity/sonar-deep-research": 2}'
            />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}
