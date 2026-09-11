import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
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
import { useState } from 'react'
import { afterEach, expect, it } from 'vitest'

import { PricingPreviewControls } from '@/features/model-pricing/pricing-preview-controls'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { EffectivePrice } from '../components/effective-price'
import {
  useEffectivePricing,
  type EffectivePricingModel,
} from '../effective-pricing'

const model: EffectivePricingModel = {
  model_name: 'example',
  group_prices: [
    {
      using_group: 'default',
      billing_mode: 'tiered_expr',
      billing_surface: 'token',
      status: 'formula',
      is_free: false,
      tiers: [
        {
          label: 'standard',
          condition: {
            kind: 'context_length',
            max_value: 32000,
            unit: 'token',
          },
          unit_prices: [
            {
              component: 'input',
              unit: 'million_tokens',
              amount_rub: 170.9188,
            },
            {
              component: 'cache_read',
              unit: 'million_tokens',
              amount_rub: 17.09188,
            },
          ],
        },
      ],
    },
  ],
}

afterEach(() =>
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
)

it('renders server RUB without applying site currency, FX or group ratios again', () => {
  useSystemConfigStore.getState().setConfig({
    currency: {
      ...DEFAULT_CURRENCY_CONFIG,
      quotaDisplayType: 'CUSTOM',
      customCurrencyExchangeRate: 100,
      customCurrencySymbol: 'WRONG',
    },
  })
  render(<EffectivePrice model={model} />)
  expect(screen.getByText('170.92 ₽ / 1M tokens')).toBeInTheDocument()
  expect(screen.getByText('17.09 ₽ / 1M tokens')).toBeInTheDocument()
  expect(screen.getByText(/≤ 32000/)).toBeInTheDocument()
  expect(screen.queryByText(/WRONG/)).not.toBeInTheDocument()
})

it('does not fall back to another group when the selected group is unavailable', () => {
  render(<EffectivePrice model={model} selectedGroup='premium' />)
  expect(screen.getByText('Tariff unavailable')).toBeInTheDocument()
  expect(screen.queryByText(/170.92/)).not.toBeInTheDocument()
})

it('does not display stale amounts supplied with unavailable status', () => {
  render(
    <EffectivePrice
      model={{
        ...model,
        group_prices: [{ ...model.group_prices[0], status: 'unavailable' }],
      }}
    />
  )
  expect(screen.getByText('Tariff unavailable')).toBeInTheDocument()
  expect(screen.queryByText(/170.92/)).not.toBeInTheDocument()
})

it('renders formula-only tariffs as request dependent, not zero or an invented price', () => {
  render(
    <EffectivePrice
      model={{
        ...model,
        group_prices: [{ ...model.group_prices[0], tiers: [] }],
      }}
    />
  )
  expect(screen.getByText('Price depends on the request')).toBeInTheDocument()
  expect(screen.queryByText('Free')).not.toBeInTheDocument()
})

it('keeps explicit zero, per-call units and time windows', () => {
  render(
    <EffectivePrice
      model={{
        ...model,
        group_prices: [
          {
            ...model.group_prices[0],
            is_free: true,
            tiers: [
              {
                condition: {
                  kind: 'time_of_day',
                  time_zone: 'UTC',
                  time_windows: [{ start_hour: 1, end_hour: 4 }],
                },
                unit_prices: [
                  { component: 'base', unit: 'call', amount_rub: 0 },
                ],
              },
            ],
          },
        ],
      }}
    />
  )
  expect(screen.getByText('0.00 ₽ / request')).toBeInTheDocument()
  expect(screen.getByText(/01:00 ≤.*< 04:00.*UTC/)).toBeInTheDocument()
})

it('changes the preview user group without showing the previous group tariff', async () => {
  useAuthStore
    .getState()
    .auth.setUser({ id: 91, role: 100, username: 'root', group: 'default' })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const adapter = api.defaults.adapter
  const seen: string[] = []
  api.defaults.adapter = async (config) => {
    expect(config.url).toBe('/api/option/effective_pricing')
    const group = config.params?.user_group ?? 'default'
    seen.push(group)
    return {
      config,
      status: 200,
      statusText: 'OK',
      headers: {},
      data: {
        success: true,
        data: {
          currency: 'RUB',
          user_group: group,
          available_user_groups: ['default', 'vip'],
          fx: { usd_rate: 85.4594, publication_version: 1, fetched_at: 1 },
          data: [
            {
              model_name: 'example',
              group_prices: [
                {
                  ...model.group_prices[0],
                  using_group: group,
                  tiers: [
                    {
                      unit_prices: [
                        {
                          component: 'input',
                          unit: 'million_tokens',
                          amount_rub: group === 'vip' ? 85.46 : 170.92,
                        },
                      ],
                    },
                  ],
                },
              ],
            },
          ],
        },
      },
    }
  }
  try {
    render(
      <QueryClientProvider client={client}>
        <Preview />
      </QueryClientProvider>
    )
    expect(await screen.findByText('170.92 ₽ / 1M tokens')).toBeInTheDocument()
    const user = userEvent.setup()
    await user.click(
      screen.getByRole('combobox', { name: 'Customer user group' })
    )
    await user.click(await screen.findByRole('option', { name: 'vip' }))
    expect(await screen.findByText('85.46 ₽ / 1M tokens')).toBeInTheDocument()
    expect(screen.queryByText('170.92 ₽ / 1M tokens')).not.toBeInTheDocument()
    expect(seen).toEqual(['default', 'vip'])
    expect(useAuthStore.getState().auth.user?.group).toBe('default')
  } finally {
    client.clear()
    api.defaults.adapter = adapter
    useAuthStore.getState().auth.setUser(null)
  }
})

function Preview() {
  const [group, setGroup] = useState<string>()
  const query = useEffectivePricing(group, true)
  return (
    <>
      <PricingPreviewControls
        query={query}
        userGroup={group}
        onGroupChange={setGroup}
      />
      <EffectivePrice
        model={query.isError ? null : (query.data?.data[0] ?? null)}
      />
    </>
  )
}
