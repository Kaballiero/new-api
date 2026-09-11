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

import { GroupSelector } from '@/components/model-group-selector'
import { Button } from '@/components/ui/button'
import type { useEffectivePricing } from '@/features/pricing/effective-pricing'

export function PricingPreviewControls(props: {
  query: ReturnType<typeof useEffectivePricing>
  userGroup?: string
  onGroupChange: (group: string | undefined) => void
}) {
  const { t } = useTranslation()
  const data = props.query.isError ? undefined : props.query.data
  return (
    <div className='space-y-2 text-xs'>
      <div className='flex flex-wrap items-center gap-2'>
        <div role='group' aria-label={t('Customer user group')}>
          <GroupSelector
            ariaLabel={t('Customer user group')}
            selectedGroup={
              props.userGroup === undefined
                ? 'self'
                : `group:${props.userGroup}`
            }
            groups={[
              { value: 'self', label: t('My user group') },
              ...(props.query.data?.available_user_groups ?? []).map(
                (group) => ({ value: `group:${group}`, label: group })
              ),
            ]}
            onGroupChange={(value) =>
              props.onGroupChange(
                value === 'self' ? undefined : value.slice('group:'.length)
              )
            }
          />
        </div>
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={props.query.isFetching}
          onClick={() => void props.query.refetch()}
        >
          {t('Refresh tariff')}
        </Button>
        {data && (
          <span>
            {t('User group: {{group}}', { group: data.user_group })} · USD/RUB{' '}
            {data.fx.usd_rate}
          </span>
        )}
      </div>
      <p className='text-muted-foreground'>
        {t(
          'Customer tariffs in RUB include billing FX and group pricing. Compare the same user group and using group.'
        )}
      </p>
      {props.query.isError && (
        <p role='alert' className='text-destructive'>
          {t('Tariff unavailable')}
        </p>
      )}
      {props.query.isLoading && <p role='status'>{t('Loading...')}</p>}
    </div>
  )
}
