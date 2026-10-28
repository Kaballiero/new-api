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
import { z } from 'zod'

import type { OpenRouterSyncOperatorAction } from '../types'

/** Mirrors the server bound for a single operator cost settlement. */
export const OPENROUTER_SYNC_MAX_SETTLE_COST_USD = 50

const evidenceSchema = z
  .string()
  .trim()
  .min(1, 'Evidence is required')
  .max(4096, 'Evidence must be at most 4096 characters')

const settleCostSchema = z
  .string()
  .trim()
  .regex(
    /^\d{1,12}(\.\d{1,12})?$/,
    'Enter a USD amount with at most 12 decimal places'
  )
  .refine((value) => Number(value) > 0, 'Cost must be greater than 0')
  .refine(
    (value) => Number(value) <= OPENROUTER_SYNC_MAX_SETTLE_COST_USD,
    'Cost must be at most 50 USD'
  )

/** A release never carries a cost; a settle requires a bounded positive one. */
export function createOpenRouterSyncDecisionSchema(
  action: OpenRouterSyncOperatorAction
) {
  return z.object({
    cost_usd: action === 'settle' ? settleCostSchema : z.string(),
    evidence: evidenceSchema,
  })
}

export type OpenRouterSyncDecisionValues = z.infer<
  ReturnType<typeof createOpenRouterSyncDecisionSchema>
>
