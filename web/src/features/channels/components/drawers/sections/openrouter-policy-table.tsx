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
import { Plus, Trash2 } from 'lucide-react'
import { useEffect, useId, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
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
  parseOpenRouterPolicyEntries,
  serializeOpenRouterPolicyEntries,
  validateOpenRouterPolicyEntries,
  type OpenRouterPolicyEntry,
  type OpenRouterPolicyKind,
} from '../../../lib/openrouter-policy-entries'

type OpenRouterPolicyTableProps = {
  kind: OpenRouterPolicyKind
  label: string
  value: string
  onChange: (value: string) => void
  onBlur: () => void
  disabled: boolean
  modelOptions: string[]
  id?: string
  'aria-describedby'?: string
  'aria-invalid'?: boolean
}

type PolicyRow = OpenRouterPolicyEntry & { id: number }

let nextPolicyRowId = 0

function toPolicyRows(value: string): PolicyRow[] {
  return (parseOpenRouterPolicyEntries(value) ?? []).map((entry) => {
    nextPolicyRowId += 1
    return { ...entry, id: nextPolicyRowId }
  })
}

export function OpenRouterPolicyTable(props: OpenRouterPolicyTableProps) {
  const { t } = useTranslation()
  const modelListId = useId()
  const emittedValue = useRef<string | null>(null)
  const [rows, setRows] = useState<PolicyRow[]>(() => toPolicyRows(props.value))
  const errors = validateOpenRouterPolicyEntries(rows, props.kind)
  const valueHeader =
    props.kind === 'reserve' ? t('Reserve, USD') : t('Reservation source')

  useEffect(() => {
    if (props.value === emittedValue.current) return
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setRows(toPolicyRows(props.value))
  }, [props.value])

  const updateRows = (nextRows: PolicyRow[]) => {
    const serialized = serializeOpenRouterPolicyEntries(nextRows, props.kind)
    emittedValue.current = serialized
    setRows(nextRows)
    props.onChange(serialized)
  }

  const updateRow = (id: number, field: 'model' | 'value', value: string) => {
    updateRows(
      rows.map((row) => (row.id === id ? { ...row, [field]: value } : row))
    )
  }

  const addRow = () => {
    nextPolicyRowId += 1
    updateRows([
      ...rows,
      {
        id: nextPolicyRowId,
        model: '',
        value: props.kind === 'reserve' ? '' : 'configured_price',
      },
    ])
  }

  return (
    <div
      role='group'
      id={props.id}
      aria-label={props.label}
      aria-describedby={props['aria-describedby']}
      aria-invalid={props['aria-invalid']}
      className='space-y-2'
    >
      {rows.length > 0 ? (
        <div className='space-y-2'>
          <div className='grid grid-cols-[minmax(0,1fr)_10rem_auto] gap-2 text-sm font-medium'>
            <div>{t('Upstream model ID')}</div>
            <div>{valueHeader}</div>
            <div className='w-8' />
          </div>
          {rows.map((row, index) => {
            const rowNumber = index + 1
            const modelError = errors[index]?.model
            const valueError = errors[index]?.value
            const modelErrorId = `${modelListId}-${row.id}-model-error`
            const valueErrorId = `${modelListId}-${row.id}-value-error`
            return (
              <div
                key={row.id}
                className='grid grid-cols-[minmax(0,1fr)_10rem_auto] items-start gap-2'
              >
                <div className='space-y-1'>
                  <Input
                    value={row.model}
                    onChange={(event) =>
                      updateRow(row.id, 'model', event.target.value)
                    }
                    onBlur={props.onBlur}
                    placeholder='openai/gpt-audio'
                    disabled={props.disabled}
                    list={modelListId}
                    aria-label={t('Upstream model ID {{row}}', {
                      row: rowNumber,
                    })}
                    aria-invalid={Boolean(modelError)}
                    aria-describedby={modelError ? modelErrorId : undefined}
                  />
                  {modelError && (
                    <p id={modelErrorId} className='text-destructive text-xs'>
                      {t(modelError)}
                    </p>
                  )}
                </div>
                <div className='space-y-1'>
                  {props.kind === 'reserve' ? (
                    <Input
                      type='number'
                      inputMode='decimal'
                      min='0'
                      step='any'
                      value={row.value}
                      onChange={(event) =>
                        updateRow(row.id, 'value', event.target.value)
                      }
                      onBlur={props.onBlur}
                      placeholder='0.1'
                      disabled={props.disabled}
                      aria-label={t('Reserve, USD {{row}}', { row: rowNumber })}
                      aria-invalid={Boolean(valueError)}
                      aria-describedby={valueError ? valueErrorId : undefined}
                    />
                  ) : (
                    <Select
                      items={OPENROUTER_RESERVATION_SOURCES.map((source) => ({
                        value: source,
                        label: source,
                      }))}
                      value={row.value}
                      onValueChange={(value) =>
                        updateRow(row.id, 'value', String(value))
                      }
                      disabled={props.disabled}
                    >
                      <SelectTrigger
                        className='w-full'
                        aria-label={t('Reservation source {{row}}', {
                          row: rowNumber,
                        })}
                        aria-invalid={Boolean(valueError)}
                        aria-describedby={valueError ? valueErrorId : undefined}
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
                  )}
                  {valueError && (
                    <p id={valueErrorId} className='text-destructive text-xs'>
                      {t(valueError)}
                    </p>
                  )}
                </div>
                <Button
                  type='button'
                  variant='ghost'
                  size='icon'
                  onClick={() =>
                    updateRows(rows.filter((item) => item.id !== row.id))
                  }
                  disabled={props.disabled}
                  aria-label={t('Remove row {{row}}', { row: rowNumber })}
                >
                  <Trash2 aria-hidden='true' />
                </Button>
              </div>
            )
          })}
        </div>
      ) : (
        <div className='text-muted-foreground flex h-16 items-center justify-center rounded-md border border-dashed text-sm'>
          {t('No rows. Click "Add Row" to configure a model.')}
        </div>
      )}
      <Button
        type='button'
        variant='outline'
        size='sm'
        onClick={addRow}
        disabled={props.disabled}
        className='w-full'
      >
        <Plus aria-hidden='true' />
        {t('Add Row')}
      </Button>
      {props.modelOptions.length > 0 && (
        <datalist id={modelListId}>
          {props.modelOptions.map((model) => (
            <option key={model} value={model} />
          ))}
        </datalist>
      )}
    </div>
  )
}
