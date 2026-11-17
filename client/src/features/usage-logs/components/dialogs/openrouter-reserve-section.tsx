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

import {
  OPENROUTER_RESERVATION_QUANTITY,
  OPENROUTER_RESERVATION_SOURCE,
  type OpenRouterReservationSource,
  type OpenRouterReserveAdminInfo,
} from '../../types'
import { DetailRow, DetailSection } from './log-detail-layout'

/** Sources an admin should review: a stale catalog or a manual reserve. */
const RESERVATION_SOURCES_TO_REVIEW = new Set<OpenRouterReservationSource>([
  OPENROUTER_RESERVATION_SOURCE.CATALOG_STALE,
  OPENROUTER_RESERVATION_SOURCE.MANUAL,
])

function formatReserve(usd: number, model?: string): string {
  return model ? `${usd} USD · ${model}` : `${usd} USD`
}

/**
 * Admin-only OpenRouter reserve of a request or task order: what it was
 * priced from and the reserves that raised it. Renders nothing without one.
 */
export function OpenRouterReserveSection(props: {
  adminInfo: OpenRouterReserveAdminInfo
}) {
  const { t } = useTranslation()
  const info = props.adminInfo
  const orderFacts = info.openrouter_order_facts
  const source =
    info.openrouter_reservation_source ?? orderFacts?.reservation_source
  const orderReservation = orderFacts?.reservation
  const configuredReservation = orderFacts?.configured_reservation
  const quantityLabels: Record<string, string> = {
    [OPENROUTER_RESERVATION_QUANTITY.SEARCHES]: t('Reserved web searches'),
  }
  const quantities = Object.entries(
    info.openrouter_reservation_quantities ?? {}
  )
  const sourceLabels: Record<OpenRouterReservationSource, string> = {
    catalog: t('Catalog'),
    catalog_stale: t('Stale catalog'),
    live: t('Live pricing'),
    configured_price: t('Configured price'),
    manual: t('Manual reserve'),
  }

  const rows = [
    source && (
      <DetailRow
        key='source'
        label={t('Reserve source')}
        value={
          <StatusBadge
            label={sourceLabels[source] ?? source}
            variant={
              RESERVATION_SOURCES_TO_REVIEW.has(source) ? 'warning' : 'neutral'
            }
            copyable={false}
          />
        }
      />
    ),
    ...quantities.map(([quantity, value]) => (
      <DetailRow
        key={`quantity-${quantity}`}
        label={quantityLabels[quantity] ?? quantity}
        value={String(value)}
        mono
      />
    )),
    info.openrouter_admission_policy && (
      <DetailRow
        key='admission-policy'
        label={t('Admission policy')}
        value={info.openrouter_admission_policy}
        mono
      />
    ),
    orderReservation && (
      <DetailRow
        key='order-reservation'
        label={t('Order reserve')}
        value={[
          formatReserve(orderReservation.usd, orderReservation.model),
          orderReservation.source,
        ]
          .filter(Boolean)
          .join(' · ')}
        mono
      />
    ),
    info.openrouter_configured_reserve_usd != null && (
      <DetailRow
        key='configured-reserve'
        label={t('Configured reserve')}
        value={formatReserve(
          info.openrouter_configured_reserve_usd,
          info.openrouter_configured_reserve_model
        )}
        mono
      />
    ),
    configuredReservation && (
      <DetailRow
        key='configured-order-reserve'
        label={t('Configured reserve')}
        value={formatReserve(
          configuredReservation.usd,
          configuredReservation.model
        )}
        mono
      />
    ),
    info.openrouter_configured_reserve_expr_hash && (
      <DetailRow
        key='configured-reserve-expr-hash'
        label={t('Expression hash')}
        value={info.openrouter_configured_reserve_expr_hash}
        mono
      />
    ),
    info.openrouter_approximate_reserve_usd != null && (
      <DetailRow
        key='approximate-reserve'
        label={t('Approximate reserve')}
        value={formatReserve(
          info.openrouter_approximate_reserve_usd,
          info.openrouter_approximate_reserve_model
        )}
        mono
      />
    ),
    info.openrouter_add_on_reserve_usd != null && (
      <DetailRow
        key='add-on-reserve'
        label={t('Add-on reserve')}
        value={formatReserve(
          info.openrouter_add_on_reserve_usd,
          info.openrouter_add_on_reserve_model
        )}
        mono
      />
    ),
    info.openrouter_approximate_research_reserve_usd != null && (
      <DetailRow
        key='approximate-research-reserve'
        label={t('Approximate research reserve')}
        value={formatReserve(info.openrouter_approximate_research_reserve_usd)}
        mono
      />
    ),
  ].filter(Boolean)

  if (rows.length === 0) return null
  return <DetailSection label={t('OpenRouter reserve')}>{rows}</DetailSection>
}
