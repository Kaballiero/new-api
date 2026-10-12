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
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import type {
  EffectiveGroupPricing,
  EffectivePricingModel,
  EffectivePricingTier,
} from '../effective-pricing'
import { parseTaskTiersFromExpr, parseTiersFromExpr } from '../lib/billing-expr'

export function EffectivePrice(props: {
  model: EffectivePricingModel | null
  selectedGroup?: string
  compact?: boolean
  components?: string[]
  indicative?: boolean
}) {
  const { t, i18n } = useTranslation()
  const labels: Record<string, string> = {
    input: t('Input'),
    output: t('Output'),
    cache_read: t('Cache Read'),
    cache_write: t('Cache write'),
    cache_write_1h: t('Cache write (1h)'),
    image_input: t('Image input'),
    image_output: t('Image output'),
    audio_input: t('Audio input'),
    audio_output: t('Audio output'),
    base: t('Per-request'),
  }
  const groups = (props.model?.group_prices ?? []).filter(
    (group) =>
      !props.selectedGroup ||
      props.selectedGroup === 'all' ||
      group.using_group === props.selectedGroup
  )
  if (!groups.length) {
    return (
      <span className='text-muted-foreground text-xs'>
        {t('Tariff unavailable')}
      </span>
    )
  }

  return (
    <span className='block min-w-0 space-y-3 text-xs'>
      {groups.map((group) => {
        const sources = Object.values(group.billing_sources_by_endpoint ?? {})
        if (
          sources.some((values) => values.includes('openrouter_actual_cost'))
        ) {
          const direct = sources.some((values) =>
            values.includes('configured_tariff')
          )
          const tariffs = new Map<
            string,
            { tariff: EffectiveGroupPricing; operations: string[] }
          >()
          for (const [endpoint, tariff] of Object.entries(
            group.openrouter_indicative_tariffs ?? {}
          )) {
            const identity = JSON.stringify(tariff)
            const display = tariffs.get(identity) ?? { tariff, operations: [] }
            display.operations.push(endpoint)
            tariffs.set(identity, display)
          }
          const directGroup = {
            ...group,
            billing_sources_by_endpoint: undefined,
            openrouter_indicative_tariffs: undefined,
          }
          return (
            <span className='block space-y-2' key={group.using_group}>
              {direct && (
                <EffectivePrice
                  {...props}
                  model={{
                    model_name: props.model?.model_name ?? '',
                    group_prices: [directGroup],
                  }}
                />
              )}
              {[...tariffs.entries()].map(
                ([identity, { tariff, operations }]) => (
                  <span className='block space-y-1' key={identity}>
                    <span className='text-muted-foreground block'>
                      OpenRouter · {operations.join(', ')}
                    </span>
                    <EffectivePrice
                      {...props}
                      model={{
                        model_name: props.model?.model_name ?? '',
                        group_prices: [tariff],
                      }}
                      indicative
                    />
                  </span>
                )
              )}
              <span className='text-muted-foreground block'>
                {t(
                  'OpenRouter prices are indicative. Final charge: OpenRouter cost × effective group and exchange-rate factor.'
                )}
              </span>
            </span>
          )
        }
        const unavailable =
          group.status === 'unavailable' ||
          !['unit_prices', 'formula'].includes(group.status)
        let tiers = group.tiers.length
          ? group.tiers
          : [{ unit_prices: group.unit_prices ?? [] }]
        if (
          props.indicative &&
          !tiers.some((tier) => tier.unit_prices.length)
        ) {
          tiers = indicativeFormulaTiers(group)
        }
        const hasPrices = tiers.some((tier) => tier.unit_prices.length > 0)
        const occurrences = new Map<string, number>()
        let pricesContent: ReactNode
        if (unavailable) pricesContent = <span>{t('Tariff unavailable')}</span>
        else if (!hasPrices) {
          pricesContent = (
            <span>
              {group.status === 'formula'
                ? t('Price depends on the request')
                : t('Tariff unavailable')}
            </span>
          )
        } else {
          pricesContent = tiers.map((tier) => {
            const identity = JSON.stringify(tier)
            const occurrence = occurrences.get(identity) ?? 0
            occurrences.set(identity, occurrence + 1)
            const prices = tier.unit_prices.filter((price) =>
              props.components
                ? props.components.includes(price.component)
                : !props.compact ||
                  props.indicative ||
                  ['input', 'output', 'base'].includes(price.component)
            )
            return (
              <span
                className='block space-y-1'
                key={`${identity}:${occurrence}`}
              >
                {tier.label && (
                  <span className='block font-medium'>{tier.label}</span>
                )}
                {tier.condition && <EffectiveTierCondition tier={tier} />}
                {prices.map((price) => {
                  let unit = price.unit
                  if (unit === 'million_tokens') unit = t('1M tokens')
                  if (unit === 'call') unit = t('request')
                  if (unit === 'second') unit = t('s')
                  if (unit === 'count') unit = t('unit')
                  if (unit === 'credit') unit = t('credit')
                  if (unit === 'token') unit = t('1M token')
                  const amount = price.amount_rub
                  const valid = Number.isFinite(amount) && amount >= 0
                  let value = t('Tariff unavailable')
                  if (valid) {
                    value = `${amount.toLocaleString(i18n.language, { minimumFractionDigits: 2, maximumFractionDigits: 2 })} ₽ / ${unit}`
                    if (amount > 0 && amount < 0.005) {
                      value = `< 0.01 ₽ / ${unit}`
                    }
                  }
                  return (
                    <span
                      className='block'
                      key={`${price.component}:${price.unit}`}
                    >
                      <span className='text-muted-foreground'>
                        {labels[price.component] ?? price.component}:{' '}
                      </span>
                      <span className='font-mono tabular-nums'>
                        {props.indicative && valid
                          ? t('From {{price}}', {
                              price: value,
                              interpolation: { escapeValue: false },
                            })
                          : value}
                      </span>
                    </span>
                  )
                })}
                {!prices.length && <span>{t('See tariff details')}</span>}
              </span>
            )
          })
        }
        return (
          <span key={group.using_group} className='block min-w-0 space-y-1'>
            <span className='text-muted-foreground block'>
              {t('Using group: {{group}}', { group: group.using_group })}
            </span>
            {pricesContent}
            {!props.compact &&
              !unavailable &&
              group.limitations?.map((limitation) => (
                <span className='text-muted-foreground block' key={limitation}>
                  {limitation ===
                  'provider-specific multipliers may apply for some request parameters'
                    ? t('Price may depend on provider request parameters')
                    : limitation}
                </span>
              ))}
          </span>
        )
      })}
    </span>
  )
}

function EffectiveTierCondition(props: { tier: EffectivePricingTier }) {
  const { t } = useTranslation()
  const condition = props.tier.condition
  if (!condition) return null
  if (condition.kind === 'time_of_day') {
    return (
      <span className='text-muted-foreground block'>
        {condition.time_windows
          ?.map(
            (window) =>
              `${String(window.start_hour).padStart(2, '0')}:00 ≤ ${t('Time')} < ${String(window.end_hour).padStart(2, '0')}:00`
          )
          .join('; ')}{' '}
        {condition.time_zone}
      </span>
    )
  }
  const labels: Record<string, string> = {
    context_length: t('Context length'),
    input_tokens: t('Input tokens'),
    output_tokens: t('Output tokens'),
  }
  if (!labels[condition.kind]) {
    return (
      <span className='text-muted-foreground block'>
        {t('Price depends on the request')}
      </span>
    )
  }
  return (
    <span className='text-muted-foreground block'>
      {condition.min_value !== undefined && `${condition.min_value} < `}
      {labels[condition.kind]}
      {condition.max_value !== undefined && ` ≤ ${condition.max_value}`}{' '}
      {condition.unit === 'token' || condition.unit === 'tokens'
        ? t('tokens')
        : condition.unit}
    </span>
  )
}

// Reuse the pricing parser for canonical task/fixed shapes the token-only API
// projection cannot express. Custom formulas remain request dependent.
function indicativeFormulaTiers(
  group: EffectiveGroupPricing
): EffectivePricingTier[] {
  const formula = group.formula
  if (!formula) return []
  if (formula.kind === 'task_usage_expression' && group.usage_schema) {
    return parseTaskTiersFromExpr(formula.expression, group.usage_schema).map(
      (tier) => ({
        label: [
          tier.label,
          ...tier.conditions.map(
            (condition) => `${condition.field}=${condition.value}`
          ),
        ].join(' · '),
        unit_prices: [
          ...Object.entries(tier.unitPrices).map(([field, amount]) => ({
            component: field,
            unit: group.usage_schema?.[field]?.unit ?? 'count',
            amount_rub: amount * formula.output_to_rub_factor,
          })),
          ...(tier.constant > 0
            ? [
                {
                  component: 'base',
                  unit: 'call',
                  amount_rub: tier.constant * formula.output_to_rub_factor,
                },
              ]
            : []),
        ],
      })
    )
  }
  const tiers = parseTiersFromExpr(formula.expression)
  if (
    !tiers.length ||
    tiers.some(
      (tier) => tier.billingUnit !== 'request' || tier.fixedPrice === undefined
    )
  ) {
    return []
  }
  return tiers.map((tier) => ({
    label: tier.label,
    unit_prices: [
      {
        component: 'base',
        unit: 'call',
        amount_rub:
          (tier.fixedPrice ?? 0) * 1_000_000 * formula.output_to_rub_factor,
      },
    ],
  }))
}
