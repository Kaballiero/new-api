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
import { useTranslation } from 'react-i18next'

import { createServerError } from '@/lib/server-error-message'

import { getCatalogFreshness } from '../api'
import { modelsQueryKeys } from '../lib'

/**
 * When the published upstream catalog last changed and whether it is too old
 * to price OpenRouter reserves first.
 */
export function useCatalogFreshness() {
  const { t } = useTranslation()
  return useQuery({
    queryKey: modelsQueryKeys.catalogFreshness(),
    queryFn: async () => {
      const response = await getCatalogFreshness()
      if (!response.success || !response.data) {
        throw createServerError(response, t('Operation failed'))
      }
      return response.data
    },
    // A page-level indicator must not toast on every visit.
    meta: { errorToast: false },
  })
}
