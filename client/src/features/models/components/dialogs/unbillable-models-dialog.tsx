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

import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { StatusBadge } from '@/components/status-badge'

import { useUnbillableModels } from '../../hooks/use-unbillable-models'
import type { UnbillableModel } from '../../types'

type UnbillableModelsDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

const REASON_LABELS: Record<UnbillableModel['reason'], string> = {
  no_billing_config: 'No tariff or reserve',
  no_catalog_price: 'No catalog price',
}

function reasonHint(model: UnbillableModel): string {
  if (model.reason === 'no_catalog_price') {
    return 'The catalog has no price for this model: disable it or check the provider.'
  }
  if (model.model_name.endsWith(':batch')) {
    return 'Set a tiered billing expression for the batch model.'
  }
  return 'Set a price, ratio or tiered expression, or an OpenRouter reserve on the channel.'
}

export function UnbillableModelsDialog(props: UnbillableModelsDialogProps) {
  const { t } = useTranslation()
  const query = useUnbillableModels()
  const models = query.data || []

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Models without tariff')}
      description={t(
        'Enabled models hidden from the public price list and /v1/models until they are configured'
      )}
      contentClassName='flex max-h-[85vh] max-w-2xl flex-col gap-3 p-4'
      headerClassName='flex-shrink-0 text-start'
      contentHeight='min(74vh, 760px)'
      bodyClassName='space-y-4'
    >
      {query.isLoading && <LoadingState />}
      {query.isError && (
        <ErrorState
          description={query.error.message}
          onRetry={() => void query.refetch()}
        />
      )}
      {!query.isLoading && !query.isError && models.length === 0 && (
        <div className='text-muted-foreground py-12 text-center'>
          <p>{t('All enabled models have a tariff.')}</p>
        </div>
      )}
      {!query.isLoading && !query.isError && models.length > 0 && (
        <div className='min-h-0 flex-1 divide-y overflow-y-auto rounded-lg border'>
          {models.map((model) => (
            <div
              key={`${model.reason}:${model.model_name}`}
              className='flex flex-col gap-1 p-3'
            >
              <div className='flex flex-wrap items-center gap-2'>
                <StatusBadge
                  label={model.model_name}
                  variant='neutral'
                  copyable
                />
                <StatusBadge
                  label={t(REASON_LABELS[model.reason])}
                  variant={
                    model.reason === 'no_billing_config' ? 'danger' : 'warning'
                  }
                />
                {model.catalog_type && (
                  <span className='text-muted-foreground text-xs'>
                    {model.catalog_type}
                  </span>
                )}
              </div>
              <p className='text-muted-foreground text-sm'>
                {t(reasonHint(model))}
                {model.suggested_reserve_usd != null &&
                  ` ${t('Suggested reserve: {{amount}} USD', { amount: model.suggested_reserve_usd })}`}
              </p>
            </div>
          ))}
        </div>
      )}
    </Dialog>
  )
}
