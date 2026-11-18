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
import { useQuery } from '@tanstack/react-query'
import { useEffect, useId, useRef, useState } from 'react'
import { useController, type Control } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Badge } from '@/components/ui/badge'
import {
  FieldDescription,
  FieldError,
  FieldLegend,
  FieldSet,
} from '@/components/ui/field'
import { formatDateTimeStr } from '@/lib/format'
import {
  getServerErrorMessage,
  requireServerSuccess,
} from '@/lib/server-error-message'

import { getChannelOpenRouterReserves } from '../../../api'
import { channelsQueryKeys } from '../../../lib/channel-actions'
import type { ChannelFormValues } from '../../../lib/channel-form'
import {
  OPENROUTER_DEFAULT_RESERVATION_SOURCE,
  mergeOpenRouterReserveRows,
  parseOpenRouterPolicyEntries,
  serializeOpenRouterPolicyEntries,
  validateOpenRouterPolicyEntries,
  type OpenRouterPolicyEntry,
  type OpenRouterPolicyKind,
} from '../../../lib/openrouter-policy-entries'
import { OpenRouterReserveTable } from './openrouter-reserve-table'

type OpenRouterReserveFieldProps = {
  control: Control<ChannelFormValues>
  disabled: boolean
  channelId: number | null
  upstreamModels: string[]
}

export function OpenRouterReserveField(props: OpenRouterReserveFieldProps) {
  const { t } = useTranslation()
  const descriptionId = useId()
  const sourceHintId = useId()
  const errorId = useId()
  const sourceField = useController({
    control: props.control,
    name: 'openrouter_reservation_source',
  })
  const reserveField = useController({
    control: props.control,
    name: 'openrouter_model_reserve_usd',
  })
  const sources = useOpenRouterPolicyEntries(sourceField.field, 'source')
  const reserves = useOpenRouterPolicyEntries(reserveField.field, 'reserve')
  const reservesQuery = useQuery({
    queryKey: channelsQueryKeys.openRouterReserves(props.channelId ?? 0),
    queryFn: async () =>
      requireServerSuccess(
        await getChannelOpenRouterReserves(props.channelId ?? 0)
      ).data,
    enabled: props.channelId != null,
    meta: { errorToast: false },
  })
  const preview = reservesQuery.data
  const unsaved =
    props.channelId == null ||
    sourceField.fieldState.isDirty ||
    reserveField.fieldState.isDirty
  const rows = mergeOpenRouterReserveRows(
    props.upstreamModels,
    sources.entries,
    reserves.entries,
    preview?.items ?? []
  )
  const fieldErrors = [
    sourceField.fieldState.error?.message,
    reserveField.fieldState.error?.message,
  ]
    .filter((message) => message !== undefined)
    .map((message) => ({ message: t(message) }))

  return (
    <FieldSet
      className='gap-2'
      aria-describedby={[
        descriptionId,
        sourceHintId,
        ...(fieldErrors.length > 0 ? [errorId] : []),
      ].join(' ')}
    >
      <FieldLegend variant='label'>{t('OpenRouter reserves')}</FieldLegend>
      <FieldDescription id={descriptionId}>
        {t(
          'Reservation source of each upstream model. Default: upstream. configured_price uses the configured NewAPI price or expression only for supported operations. Endpoint and provider checks still apply. Final charges use OpenRouter actual cost.'
        )}{' '}
        {t(
          'Manual reserve: a minimum hold in USD for the model. A text request priced from upstream tariffs holds the larger of the estimate and this amount; when the estimate is unavailable, this amount alone is held only for one non-streaming Chat response (n = 1) without tools, provider routing, fallback models, image input or Batch. Hosted web search on another engine or without a published search price adds this amount on top. Dedicated Audio, Rerank, Decisions, Chat image output and streaming Chat audio output hold at least this amount. It is not a spending limit: full actual cost is charged, including excess. An empty manual amount disables the hold for that model. Legacy Sonar settings migrate on save.'
        )}
      </FieldDescription>
      <FieldDescription id={sourceHintId}>
        {t(
          'upstream: OpenRouter prices from the catalog, then live endpoints, then a stale catalog document if live fails. configured_price: the channel price, taking the maximum with the estimate and the manual amount.'
        )}
      </FieldDescription>
      {reservesQuery.isLoading && (
        <LoadingState
          inline
          size='sm'
          message={t('Loading reserve examples...')}
        />
      )}
      {reservesQuery.isError && (
        <ErrorState
          className='min-h-0 py-4'
          title={t('Failed to load reserve examples')}
          description={getServerErrorMessage(reservesQuery.error)}
          onRetry={() => void reservesQuery.refetch()}
        />
      )}
      {preview && (
        <div className='text-muted-foreground flex flex-wrap items-center gap-2 text-xs'>
          {preview.fetched_at && (
            <span>
              {t('Catalog fetched at {{time}}', {
                time: formatDateTimeStr(new Date(preview.fetched_at)),
              })}
            </span>
          )}
          {preview.catalog_stale && (
            <Badge variant='warning'>{t('Catalog is stale')}</Badge>
          )}
          <span>
            {t(
              'Example request: {{operation}}, {{promptTokens}} prompt and {{outputTokens}} output tokens',
              {
                operation: preview.reference.operation,
                promptTokens: preview.reference.prompt_tokens,
                outputTokens: preview.reference.output_tokens,
              }
            )}
          </span>
        </div>
      )}
      {rows.length > 0 ? (
        <OpenRouterReserveTable
          rows={rows}
          sourceErrors={sources.errors}
          reserveErrors={reserves.errors}
          previewLoaded={preview != null}
          referenceOperation={preview?.reference.operation}
          unsaved={unsaved}
          disabled={props.disabled}
          onSourceChange={(model, source) => {
            if (source === OPENROUTER_DEFAULT_RESERVATION_SOURCE) {
              sources.remove(model)
              return
            }
            sources.setValue(model, source)
          }}
          onManualUsdChange={reserves.setValue}
          onManualUsdBlur={reserveField.field.onBlur}
          onRemove={(model) => {
            sources.remove(model)
            reserves.remove(model)
          }}
        />
      ) : (
        <EmptyState
          bordered
          className='min-h-0 py-6'
          title={t('No upstream models')}
          description={t(
            'Add models to the channel to configure their reserves.'
          )}
        />
      )}
      <FieldError id={errorId} errors={fieldErrors} />
    </FieldSet>
  )
}

// Keeps the edited rows of one setting map. An empty value keeps its row and
// drops the model from the setting. An invalid value is stored as non-map JSON,
// so the form schema blocks saving while the row keeps the typed text.
function useOpenRouterPolicyEntries(
  field: { value?: string; onChange: (value: string) => void },
  kind: OpenRouterPolicyKind
) {
  const emittedValue = useRef<string | null>(null)
  const [entries, setEntries] = useState<OpenRouterPolicyEntry[]>(
    () => parseOpenRouterPolicyEntries(field.value ?? '') ?? []
  )

  useEffect(() => {
    if (field.value === emittedValue.current) return
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setEntries(parseOpenRouterPolicyEntries(field.value ?? '') ?? [])
  }, [field.value])

  const storedEntries = entries.filter((entry) => entry.value !== '')
  const storedErrors = validateOpenRouterPolicyEntries(storedEntries, kind)
  const errors = new Map(
    storedEntries.map((entry, index) => [entry.model, storedErrors[index]])
  )

  const update = (nextEntries: OpenRouterPolicyEntry[]) => {
    const serialized = serializeOpenRouterPolicyEntries(
      nextEntries.filter((entry) => entry.value !== ''),
      kind
    )
    emittedValue.current = serialized
    setEntries(nextEntries)
    field.onChange(serialized)
  }

  const setValue = (model: string, value: string) => {
    update(
      entries.some((entry) => entry.model === model)
        ? entries.map((entry) =>
            entry.model === model ? { model, value } : entry
          )
        : [...entries, { model, value }]
    )
  }

  const remove = (model: string) => {
    update(entries.filter((entry) => entry.model !== model))
  }

  return { entries, errors, setValue, remove }
}
