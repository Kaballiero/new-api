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
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { formatTimestampToDate } from '@/lib/format'

import { useCatalogFreshness } from '../hooks/use-catalog-freshness'

/**
 * Age of the published upstream catalog for admins; stale once it is older
 * than PRICING_CATALOG_MAX_AGE_HOURS. Hidden without a published catalog.
 */
export function CatalogFreshnessBadge() {
  const { t } = useTranslation()
  const freshness = useCatalogFreshness()
  if (!freshness.data?.fetched_at) return null

  const fetchedAt = formatTimestampToDate(freshness.data.fetched_at)
  return (
    <StatusBadge
      label={
        freshness.data.stale
          ? t('Catalog stale: {{date}}', { date: fetchedAt })
          : t('Catalog: {{date}}', { date: fetchedAt })
      }
      variant={freshness.data.stale ? 'warning' : 'neutral'}
      copyable={false}
      title={t(
        'OpenRouter reserves are priced from the catalog while it is at most {{hours}} h old',
        { hours: freshness.data.max_age_hours }
      )}
    />
  )
}
