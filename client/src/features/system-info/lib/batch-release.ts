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

/** Matches the server limit of 4096 characters of trimmed evidence. */
export const MAX_BATCH_RELEASE_EVIDENCE_LENGTH = 4096

export const batchReleaseSchema = z.object({
  evidence: z
    .string()
    .trim()
    .min(1, 'Enter provider evidence')
    .max(
      MAX_BATCH_RELEASE_EVIDENCE_LENGTH,
      'Evidence must be at most 4096 characters'
    ),
})

export type BatchReleaseFormValues = z.infer<typeof batchReleaseSchema>
