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

import { getCurrencyLabel } from '@/lib/currency'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { DEFAULT_TOKEN_UNIT } from '../constants'
import { useBillingTime } from '../hooks/use-billing-time'
import {
  getDynamicDisplayGroupRatio,
  getDynamicPriceUnitLabelKey,
  getDynamicPricingSummary,
  getIndicativePriceEntries,
  isUnconfiguredTaskUsageModel,
} from '../lib/dynamic-price'
import { isTokenBasedModel, hasOpenRouterPricing } from '../lib/model-helpers'
import { formatPrice, formatRequestPrice } from '../lib/price'
import type { PricingModel, PriceType, TokenUnit } from '../types'
import { DynamicPricingBreakdown } from './dynamic-pricing-breakdown'
import { EffectivePrice } from './effective-price'

export type ModelPriceCellOptions = {
  tokenUnit?: TokenUnit
  priceRate?: number
  usdExchangeRate?: number
  showRechargePrice?: boolean
  selectedGroup?: string
}

export function ModelPriceCell(props: {
  model: PricingModel
  options?: ModelPriceCellOptions
  showExpression?: boolean
  components?: string[]
  details?: boolean
}) {
  if (props.model.effective_pricing !== undefined) {
    return (
      <EffectivePrice
        model={props.model.effective_pricing}
        selectedGroup={props.options?.selectedGroup}
        compact
        components={props.components}
      />
    )
  }
  if (hasOpenRouterPricing(props.model, props.options?.selectedGroup)) {
    return (
      <OpenRouterPrice
        model={props.model}
        options={props.options}
        components={props.components}
        details={props.details}
      />
    )
  }
  return <LegacyModelPriceCell {...props} />
}

function OpenRouterPrice(props: {
  details?: boolean
  components?: string[]
  model: PricingModel
  options?: ModelPriceCellOptions
}) {
  const { t } = useTranslation()
  const groups = Object.entries(
    props.model.billing_sources_by_group ?? {}
  ).filter(
    ([group]) =>
      !props.options?.selectedGroup ||
      props.options.selectedGroup === 'all' ||
      group === 'all' ||
      group === props.options.selectedGroup
  )
  return (
    <span className='block min-w-0 space-y-3'>
      {groups.map(([group, endpoints]) => {
        const tariffs = new Map<string, string[]>()
        for (const [endpoint, sources] of Object.entries(endpoints)) {
          if (!sources.includes('openrouter_actual_cost')) continue
          const expression =
            props.model.indicative_billing_expr_by_endpoint?.[endpoint] ?? ''
          const operations = tariffs.get(expression) ?? []
          operations.push(endpoint)
          tariffs.set(expression, operations)
        }
        return (
          <span className='block space-y-1' key={group}>
            <span className='text-muted-foreground block text-xs'>
              {t('Using group: {{group}}', { group })}
            </span>
            {!props.details &&
              Object.values(endpoints).some((sources) =>
                sources.includes('configured_tariff')
              ) && (
                <LegacyModelPriceCell
                  components={props.components}
                  model={{ ...props.model, enable_groups: [group] }}
                  options={{ ...props.options, selectedGroup: group }}
                />
              )}
            {[...tariffs.entries()].map(([expression, operations]) => {
              const endpoint = operations.join(', ')
              return (
                <span className='block space-y-1' key={endpoint}>
                  <span className='text-muted-foreground block text-xs'>
                    OpenRouter · {endpoint}
                  </span>
                  {expression?.trim() ? (
                    <span className='block space-y-3'>
                      <LegacyModelPriceCell
                        components={props.components}
                        model={{
                          ...props.model,
                          enable_groups: [group],
                          billing_mode: 'tiered_expr',
                          billing_expr: expression,
                        }}
                        options={{ ...props.options, selectedGroup: group }}
                        indicative
                      />
                      {props.details && (
                        <DynamicPricingBreakdown
                          billingExpr={expression}
                          usageSchema={props.model.billing_usage_schema}
                          taskPriceOptions={{
                            ...props.options,
                            groupRatioMultiplier: getDynamicDisplayGroupRatio(
                              { ...props.model, enable_groups: [group] },
                              group
                            ),
                          }}
                        />
                      )}
                    </span>
                  ) : (
                    <span className='text-muted-foreground text-xs'>
                      {t('Tariff unavailable')}
                    </span>
                  )}
                </span>
              )
            })}
          </span>
        )
      })}
      <span className='text-muted-foreground block text-xs'>
        {t(
          'OpenRouter prices are indicative. Final charge: OpenRouter cost × effective group and exchange-rate factor.'
        )}
      </span>
    </span>
  )
}

function LegacyModelPriceCell(props: {
  indicative?: boolean
  model: PricingModel
  options?: ModelPriceCellOptions
  showExpression?: boolean
  components?: string[]
}) {
  const { t } = useTranslation()
  const currency = useSystemConfigStore((state) => state.config.currency)
  const currencyLabel =
    currency.quotaDisplayType === 'TOKENS' ? 'USD' : getCurrencyLabel()
  const options = props.options ?? {}
  const tokenUnit = options.tokenUnit ?? DEFAULT_TOKEN_UNIT
  const tokenUnitLabel = tokenUnit === 'K' ? '1K' : '1M'
  const billingTime = useBillingTime(props.model.billing_expr)
  const dynamic = getDynamicPricingSummary(props.model, {
    ...options,
    now: billingTime === undefined ? undefined : new Date(billingTime),
    tokenUnit,
    showCurrencySymbol: false,
    groupRatioMultiplier: getDynamicDisplayGroupRatio(
      props.model,
      options.selectedGroup
    ),
  })
  let metrics: Array<{ label: string; value: string }>
  let caption = t('{{currency}} / {{unit}} tokens', {
    currency: currencyLabel,
    unit: tokenUnitLabel,
  })

  if (dynamic) {
    if (dynamic.isSpecialExpression) {
      return (
        <span className='block max-w-full min-w-0'>
          <span className='text-muted-foreground block truncate text-sm'>
            {t('Special billing expression')}
          </span>
          {props.showExpression !== false && (
            <code className='text-muted-foreground mt-1 line-clamp-2 block text-xs break-all whitespace-normal'>
              {dynamic.rawExpression}
            </code>
          )}
        </span>
      )
    }
    const hasRequestPrice = dynamic.primaryEntries.some(
      (entry) => entry.unit === 'request'
    )
    const components: Record<string, string> = {
      p: 'input',
      c: 'output',
      cr: 'cache_read',
      cc: 'cache_write',
      cc1h: 'cache_write_1h',
      img: 'image_input',
      img_o: 'image_output',
      ai: 'audio_input',
      ao: 'audio_output',
      fixed: 'base',
    }
    let entries = dynamic.entries
    if (props.indicative) {
      entries = getIndicativePriceEntries(props.model, {
        ...options,
        tokenUnit,
        showCurrencySymbol: false,
        groupRatioMultiplier: getDynamicDisplayGroupRatio(
          props.model,
          options.selectedGroup
        ),
      })
    }
    if (props.components) {
      entries = entries.filter((entry) =>
        props.components?.includes(components[entry.key] ?? entry.key)
      )
    } else if (props.indicative) {
      entries = entries.filter((entry) =>
        dynamic.isTaskUsage
          ? entry.unit !== 'request'
          : entry.unit === 'request' ||
            entry.field === 'inputPrice' ||
            entry.field === 'outputPrice'
      )
    } else {
      entries = dynamic.primaryEntries
    }
    metrics = entries.slice(0, hasRequestPrice ? 3 : 2).map((entry) => {
      const unit = getDynamicPriceUnitLabelKey(entry)
      let suffix = unit ? `/${t(unit)}` : ''
      if (hasRequestPrice && entry.unit === 'token') {
        suffix = `/${t('{{unit}} tokens', { unit: tokenUnitLabel })}`
      }
      return {
        label:
          entry.labelKind === 'schema' ? entry.shortLabel : t(entry.shortLabel),
        value: `${entry.formattedRange ?? entry.formatted}${suffix}`,
      }
    })
    if (metrics.length === 0) {
      let emptyLabel = t('Dynamic Pricing')
      if (props.indicative) emptyLabel = t('Price depends on the request')
      if (props.components) emptyLabel = t('See tariff details')
      return <span className='text-muted-foreground text-sm'>{emptyLabel}</span>
    }
    if (dynamic.isTaskUsage || hasRequestPrice) caption = currencyLabel
    if (dynamic.isTimePricing && !props.indicative) {
      caption += ` · ${t('Current period price')}`
    }
    if (dynamic.isMixedBilling) {
      caption += ` · ${t('Token or per-call pricing')}`
    }
    if (dynamic.tierCount > 1) {
      caption += ` · ${t('{{count}} tiers', { count: dynamic.tierCount })}`
    }
  } else {
    if (isUnconfiguredTaskUsageModel(props.model)) {
      return (
        <span className='text-muted-foreground text-sm'>
          {t('Not configured')}
        </span>
      )
    }
    const tokenBased = isTokenBasedModel(props.model)
    if (
      !Number.isFinite(
        tokenBased ? props.model.model_ratio : props.model.model_price
      )
    ) {
      return (
        <span className='text-muted-foreground text-sm'>
          {t('Unset price')}
        </span>
      )
    }
    if (tokenBased) {
      metrics = [
        {
          label: t('Input'),
          value: formatPrice(
            props.model,
            'input',
            tokenUnit,
            options.showRechargePrice,
            options.priceRate,
            options.usdExchangeRate,
            options.selectedGroup,
            false
          ),
        },
        {
          label: t('Output'),
          value: formatPrice(
            props.model,
            'output',
            tokenUnit,
            options.showRechargePrice,
            options.priceRate,
            options.usdExchangeRate,
            options.selectedGroup,
            false
          ),
        },
      ]
    } else {
      metrics = [
        {
          label: t('Per-request'),
          value: formatRequestPrice(
            props.model,
            options.showRechargePrice,
            options.priceRate,
            options.usdExchangeRate,
            options.selectedGroup,
            false
          ),
        },
      ]
      caption = `${currencyLabel} / ${t('request')}`
    }
  }
  if (props.components && !dynamic) {
    const componentTypes: Array<{
      component: string
      type: PriceType
      label: string
      ratio?: number | null
    }> = [
      {
        component: 'cache_read',
        type: 'cache',
        label: t('Cache Read'),
        ratio: props.model.cache_ratio,
      },
      {
        component: 'cache_write',
        type: 'create_cache',
        label: t('Cache write'),
        ratio: props.model.create_cache_ratio,
      },
    ]
    metrics = componentTypes
      .filter(
        (entry) =>
          props.components?.includes(entry.component) && entry.ratio != null
      )
      .map((entry) => ({
        label: entry.label,
        value: formatPrice(
          props.model,
          entry.type,
          tokenUnit,
          options.showRechargePrice,
          options.priceRate,
          options.usdExchangeRate,
          options.selectedGroup,
          false
        ),
      }))
    if (!metrics.length) {
      return (
        <span className='text-muted-foreground text-xs'>
          {t('See tariff details')}
        </span>
      )
    }
  }
  return (
    <span className='block w-full max-w-full min-w-0 space-y-1.5'>
      <span
        className={metrics.length > 1 ? 'grid grid-cols-2 gap-x-4' : 'grid'}
      >
        {metrics.map((metric) => (
          <span
            key={metric.label}
            className='flex min-w-0 flex-col items-start gap-y-0.5 sm:flex-row sm:flex-wrap sm:items-baseline sm:gap-x-1.5'
          >
            <span
              className='text-muted-foreground min-w-0 truncate text-xs font-normal'
              title={metric.label}
            >
              {metric.label}
            </span>
            <span
              className='min-w-0 font-mono text-sm break-words whitespace-normal tabular-nums'
              title={metric.value}
            >
              {props.indicative
                ? t('From {{price}}', {
                    price: metric.value,
                    interpolation: { escapeValue: false },
                  })
                : metric.value}
            </span>
          </span>
        ))}
      </span>
      <span
        className='text-muted-foreground block text-xs font-normal break-words whitespace-normal sm:truncate'
        title={caption}
      >
        {caption}
      </span>
    </span>
  )
}
