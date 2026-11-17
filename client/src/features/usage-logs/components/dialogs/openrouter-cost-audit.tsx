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
import { AlertTriangle, Receipt } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { formatLogQuota } from '@/lib/format'

import type { LogOtherData } from '../../types'
import { DetailRow, DetailSection } from './log-detail-layout'

interface OpenRouterCostAuditProps {
  adminInfo: LogOtherData['admin_info']
}

/** Admin view of how an OpenRouter request cost was confirmed and settled. */
export function OpenRouterCostAudit(props: OpenRouterCostAuditProps) {
  const { t } = useTranslation()
  const info = props.adminInfo
  if (!info?.openrouter_cost_basis) return null
  const basis = info.openrouter_cost_basis
  const outcomeUnknown = info.openrouter_financial_outcome === 'unknown'

  return (
    <>
      <DetailSection
        icon={<Receipt className='size-3.5' aria-hidden='true' />}
        label={t('OpenRouter Cost')}
      >
        <DetailRow
          label={t('Generation ID')}
          value={info.openrouter_generation_id || '-'}
          mono
        />
        <DetailRow
          label={t('Cost source')}
          value={info.openrouter_cost_source || '-'}
          mono
        />
        <DetailRow label={t('Cost basis')} value={basis.version} mono />
        <DetailRow
          label={t('Estimated cost')}
          value={`${basis.estimated_usd} USD`}
          mono
        />
        <DetailRow
          label={t('Upstream cost')}
          value={
            info.upstream_cost_usd === undefined
              ? '-'
              : `${info.upstream_cost_usd} USD`
          }
          mono
        />
        <DetailRow
          label={t('Held quota')}
          value={
            info.reserved_quota === undefined
              ? '-'
              : formatLogQuota(info.reserved_quota)
          }
          mono
        />
      </DetailSection>
      {outcomeUnknown ? (
        <DetailSection
          icon={<AlertTriangle className='size-3.5' aria-hidden='true' />}
          label={t('OpenRouter charge unknown')}
          variant='danger'
        >
          <p className='text-xs wrap-break-word'>
            {t(
              'The request reached OpenRouter but its cost was not confirmed. Check the charge in OpenRouter before resolving it.'
            )}
          </p>
          <DetailRow
            label={t('Reason')}
            value={info.openrouter_unknown_reason || '-'}
            mono
          />
        </DetailSection>
      ) : null}
    </>
  )
}
