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
import * as z from 'zod'

import { getServerErrorSources } from '@/lib/server-error-message'

/** Matches the server's 191-character bound on upstream_id. */
export const MAX_BATCH_UPSTREAM_ID_LENGTH = 191

/** Provider Batch IDs the server adapters accept; Gemini names keep `batches/`. */
const BATCH_UPSTREAM_ID_PATTERN = /^(batches\/)?[A-Za-z0-9_-]+$/

export const batchRecoverSchema = z.object({
  upstreamId: z
    .string()
    .trim()
    .min(1, 'Enter the provider Batch ID')
    .max(
      MAX_BATCH_UPSTREAM_ID_LENGTH,
      'Batch ID must be at most 191 characters'
    )
    .regex(
      BATCH_UPSTREAM_ID_PATTERN,
      'Use letters, digits, _ and - only, optionally prefixed with batches/'
    ),
})

export type BatchRecoverFormValues = z.infer<typeof batchRecoverSchema>

const RECOVER_ERROR_MESSAGES: Record<string, string> = {
  recovery_unverified:
    'Provider ownership could not be verified; the Batch is unchanged.',
  recovery_ineligible:
    'This Batch can no longer be recovered by ID; the Batch is unchanged.',
}

/** i18n key for a recovery outcome the operator must act on, if recognized. */
export function getBatchRecoverErrorMessage(error: unknown): string | null {
  const source = getServerErrorSources(error).find(
    (candidate) => candidate.type === 'batch_error'
  )
  if (typeof source?.code !== 'string') return null
  return RECOVER_ERROR_MESSAGES[source.code] ?? null
}
