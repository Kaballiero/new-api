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
import type { OpenRouterReserveItem } from '../types'

export type OpenRouterPolicyKind = 'reserve' | 'source'

export type OpenRouterPolicyEntry = {
  model: string
  value: string
}

export type OpenRouterPolicyEntryError = {
  model?: string
  value?: string
}

export const OPENROUTER_RESERVATION_SOURCES = [
  'upstream',
  'configured_price',
] as const

export function parseOpenRouterPolicyEntries(
  value: string
): OpenRouterPolicyEntry[] | null {
  if (!value.trim()) return []
  try {
    const parsed: unknown = JSON.parse(value)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return null
    }
    return Object.entries(parsed).map(([model, entryValue]) => ({
      model,
      value: String(entryValue),
    }))
  } catch {
    return null
  }
}

function isPositiveAmount(value: string): boolean {
  if (!value.trim()) return false
  const amount = Number(value)
  return Number.isFinite(amount) && amount > 0
}

export function validateOpenRouterPolicyEntries(
  entries: OpenRouterPolicyEntry[],
  kind: OpenRouterPolicyKind
): OpenRouterPolicyEntryError[] {
  const counts = new Map<string, number>()
  for (const entry of entries) {
    counts.set(entry.model, (counts.get(entry.model) ?? 0) + 1)
  }
  return entries.map((entry) => {
    const error: OpenRouterPolicyEntryError = {}
    if (!entry.model) {
      error.model = 'Enter an upstream model ID'
    } else if (/[\s*]/.test(entry.model)) {
      error.model = 'Model IDs cannot contain spaces or *'
    } else if ((counts.get(entry.model) ?? 0) > 1) {
      error.model = 'Duplicate model ID'
    }
    if (kind === 'reserve' && !isPositiveAmount(entry.value)) {
      error.value = 'Enter a positive USD amount'
    }
    if (
      kind === 'source' &&
      !OPENROUTER_RESERVATION_SOURCES.some((source) => source === entry.value)
    ) {
      error.value = 'Select upstream or configured_price'
    }
    return error
  })
}

export function serializeOpenRouterPolicyEntries(
  entries: OpenRouterPolicyEntry[],
  kind: OpenRouterPolicyKind
): string {
  const errors = validateOpenRouterPolicyEntries(entries, kind)
  if (errors.some((error) => error.model || error.value)) {
    return JSON.stringify(entries.map((entry) => [entry.model, entry.value]))
  }
  const policy: Record<string, number | string> = {}
  for (const entry of entries) {
    policy[entry.model] = kind === 'reserve' ? Number(entry.value) : entry.value
  }
  return JSON.stringify(policy, null, 2)
}

export type OpenRouterReserveRow = {
  model: string
  inChannel: boolean
  source: string
  manualUsd: string
  preview: OpenRouterReserveItem | undefined
}

// Rows are the channel's upstream models, then the setting keys the channel
// no longer has. A model without a source entry is reserved from upstream.
export function mergeOpenRouterReserveRows(
  models: string[],
  sources: OpenRouterPolicyEntry[],
  reserves: OpenRouterPolicyEntry[],
  preview: OpenRouterReserveItem[]
): OpenRouterReserveRow[] {
  const channelModels = new Set(models)
  const settingModels = [...sources, ...reserves].map((entry) => entry.model)
  return [...new Set([...models, ...settingModels])].map((model) => ({
    model,
    inChannel: channelModels.has(model),
    source: sources.find((entry) => entry.model === model)?.value ?? 'upstream',
    manualUsd: reserves.find((entry) => entry.model === model)?.value ?? '',
    // A refused model may have no resolved upstream model.
    preview: preview.find(
      (item) => (item.upstream_model ?? item.model) === model
    ),
  }))
}
