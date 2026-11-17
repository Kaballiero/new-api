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
import { Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Field, FieldError } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import {
  OPENROUTER_RESERVATION_SOURCES,
  type OpenRouterPolicyEntryError,
  type OpenRouterReserveRow,
} from '../../../lib/openrouter-policy-entries'
import type {
  OpenRouterReserveItem,
  OpenRouterReserveProvider,
} from '../../../types'

// Example statuses of models the catalog cannot price: the gateway prices
// them from the live endpoints at request time.
const OPENROUTER_LIVE_EXAMPLE_STATUSES = new Set([
  'live_only',
  'needs_live_check',
])

// The gateway refuses the reserve for a reason live prices cannot fix.
const OPENROUTER_REFUSED_EXAMPLE_STATUS = 'refused'

const usdFormatter = new Intl.NumberFormat(undefined, {
  style: 'currency',
  currency: 'USD',
  maximumFractionDigits: 6,
})

type OpenRouterReserveTableProps = {
  rows: OpenRouterReserveRow[]
  sourceErrors: Map<string, OpenRouterPolicyEntryError | undefined>
  reserveErrors: Map<string, OpenRouterPolicyEntryError | undefined>
  previewLoaded: boolean
  unsaved: boolean
  disabled: boolean
  onSourceChange: (model: string, source: string) => void
  onManualUsdChange: (model: string, usd: string) => void
  onManualUsdBlur: () => void
  onRemove: (model: string) => void
}

export function OpenRouterReserveTable(props: OpenRouterReserveTableProps) {
  const { t } = useTranslation()
  const sourceItems = OPENROUTER_RESERVATION_SOURCES.map((source) => ({
    value: source,
    label: source,
  }))
  // A row shows the saved example only while nothing that prices it changed.
  const currentPreview = (row: OpenRouterReserveRow) =>
    props.unsaved ? undefined : row.preview
  const noPreview = <span className='text-muted-foreground'>-</span>

  return (
    <StaticDataTable
      data={props.rows}
      getRowKey={(row) => row.model}
      columns={[
        {
          id: 'model',
          header: t('Upstream model ID'),
          cell: (row) => {
            const modelError =
              props.sourceErrors.get(row.model)?.model ??
              props.reserveErrors.get(row.model)?.model
            return (
              <div className='flex flex-col items-start gap-1'>
                <span className='font-mono text-xs break-all'>{row.model}</span>
                {!row.inChannel && (
                  <Badge variant='outline'>{t('Not in channel')}</Badge>
                )}
                {modelError && <FieldError>{t(modelError)}</FieldError>}
              </div>
            )
          },
        },
        {
          id: 'source',
          header: t('Reservation source'),
          className: 'w-44',
          cell: (row) => {
            const error = props.sourceErrors.get(row.model)
            return (
              <Field data-invalid={Boolean(error?.value)}>
                <Select
                  items={sourceItems}
                  value={row.source}
                  onValueChange={(value) =>
                    props.onSourceChange(row.model, String(value))
                  }
                  disabled={props.disabled}
                >
                  <SelectTrigger
                    className='w-full'
                    aria-label={t('Reservation source for {{model}}', {
                      model: row.model,
                    })}
                    aria-invalid={Boolean(error?.model || error?.value)}
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      {OPENROUTER_RESERVATION_SOURCES.map((source) => (
                        <SelectItem key={source} value={source}>
                          {source}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
                {error?.value && <FieldError>{t(error.value)}</FieldError>}
              </Field>
            )
          },
        },
        {
          id: 'manual',
          header: t('Manual reserve, USD'),
          className: 'w-36',
          cell: (row) => {
            const error = props.reserveErrors.get(row.model)
            return (
              <Field data-invalid={Boolean(error?.value)}>
                <Input
                  type='number'
                  inputMode='decimal'
                  min='0'
                  step='any'
                  value={row.manualUsd}
                  placeholder={t('None')}
                  onChange={(event) =>
                    props.onManualUsdChange(row.model, event.target.value)
                  }
                  onBlur={props.onManualUsdBlur}
                  disabled={props.disabled}
                  aria-label={t('Manual reserve, USD for {{model}}', {
                    model: row.model,
                  })}
                  aria-invalid={Boolean(error?.model || error?.value)}
                />
                {error?.value && <FieldError>{t(error.value)}</FieldError>}
              </Field>
            )
          },
        },
        {
          id: 'effective',
          header: t('Effective source'),
          cell: (row) => {
            const preview = currentPreview(row)
            if (!preview?.effective_source) return noPreview
            return <Badge variant='secondary'>{preview.effective_source}</Badge>
          },
        },
        {
          id: 'example',
          header: t('Example'),
          cell: (row) => {
            const preview = currentPreview(row)
            if (preview) return <OpenRouterReserveExample item={preview} />
            if (props.unsaved || props.previewLoaded) {
              return (
                <span className='text-muted-foreground text-xs'>
                  {t('Save to update the example')}
                </span>
              )
            }
            return noPreview
          },
        },
        {
          id: 'providers',
          header: t('Providers'),
          cell: (row) => {
            const preview = currentPreview(row)
            if (!preview || preview.providers.length === 0) return noPreview
            return (
              <OpenRouterReserveProviders
                model={row.model}
                providers={preview.providers}
              />
            )
          },
        },
        {
          id: 'actions',
          header: <span className='sr-only'>{t('Actions')}</span>,
          className: 'w-10',
          cell: (row) =>
            !row.inChannel && (
              <Button
                type='button'
                variant='ghost'
                size='icon-sm'
                onClick={() => props.onRemove(row.model)}
                disabled={props.disabled}
                aria-label={t('Remove {{value}}', { value: row.model })}
              >
                <Trash2 className='text-destructive' aria-hidden='true' />
              </Button>
            ),
        },
      ]}
    />
  )
}

function OpenRouterReserveExample(props: { item: OpenRouterReserveItem }) {
  const { t } = useTranslation()
  const isLive = OPENROUTER_LIVE_EXAMPLE_STATUSES.has(props.item.example_status)
  const isRefused =
    props.item.example_status === OPENROUTER_REFUSED_EXAMPLE_STATUS
  if (!isLive && !isRefused) {
    if (props.item.example_usd == null) {
      return <span className='text-muted-foreground'>-</span>
    }
    return (
      <span className='font-mono text-xs'>
        {usdFormatter.format(props.item.example_usd)}
      </span>
    )
  }
  return (
    <div className='flex flex-col gap-0.5 text-xs whitespace-normal'>
      {isLive ? (
        <span>{t('Priced live at request time')}</span>
      ) : (
        <span className='text-destructive'>{props.item.example_error}</span>
      )}
      {props.item.configured_usd != null && (
        <span className='text-muted-foreground'>
          {t('At least {{usd}} by the configured price', {
            usd: usdFormatter.format(props.item.configured_usd),
          })}
        </span>
      )}
      {props.item.manual_usd != null && (
        <span className='text-muted-foreground'>
          {t('At least {{usd}} by the manual reserve', {
            usd: usdFormatter.format(props.item.manual_usd),
          })}
        </span>
      )}
    </div>
  )
}

function OpenRouterReserveProviders(props: {
  model: string
  providers: OpenRouterReserveProvider[]
}) {
  const { t } = useTranslation()
  return (
    <Popover>
      <PopoverTrigger
        render={
          <Button
            type='button'
            variant='outline'
            size='xs'
            aria-label={t('Providers for {{model}}', { model: props.model })}
          />
        }
      >
        {props.providers.length}
      </PopoverTrigger>
      <PopoverContent className='w-72'>
        <ul className='flex flex-col gap-1 text-xs'>
          {props.providers.map((provider) => (
            <li
              key={provider.tag}
              className='flex items-center justify-between gap-2'
            >
              <span className='flex items-center gap-1 font-mono break-all'>
                {provider.tag}
                {provider.tier && (
                  <Badge variant='outline'>{provider.tier}</Badge>
                )}
              </span>
              <span className='font-mono'>
                {usdFormatter.format(provider.usd)}
              </span>
            </li>
          ))}
        </ul>
      </PopoverContent>
    </Popover>
  )
}
