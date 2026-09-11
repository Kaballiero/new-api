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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, renderHook, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { CachedPriceCell } from '../components/cached-price-cell'
import { ModelCard } from '../components/model-card'
import { ModelDetailsContent } from '../components/model-details'
import { ModelPriceCell } from '../components/model-price-cell'
import type { EffectivePricingModel } from '../effective-pricing'
import { usePricingData } from '../hooks/use-pricing-data'
import { filterByGroup } from '../lib/filters'
import type { PricingModel, PricingData } from '../types'

vi.mock('@visactor/react-vchart', () => ({ VChart: () => null }))

const tariff: EffectivePricingModel = {
  model_name: 'square-example',
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
const model: PricingModel = {
  id: 1,
  model_name: 'square-example',
  quota_type: 0,
  model_ratio: 99,
  completion_ratio: 5,
  enable_groups: ['default'],
  group_ratio: { default: 10 },
  billing_mode: 'tiered_expr',
  billing_expr: 'tier("legacy", p * 99 + c * 200)',
}
let client: QueryClient
function wrapper(props: { children: ReactNode }) {
  return (
    <QueryClientProvider client={client}>{props.children}</QueryClientProvider>
  )
}
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  client.setQueryData(['pricing'], {
    success: true,
    data: [model],
    vendors: [],
    group_ratio: { default: 10 },
    usable_group: { default: { desc: '', ratio: 10 } },
    supported_endpoint: {},
    auto_groups: [],
  })
  useAuthStore.getState().auth.setUser({
    id: 17,
    role: 1,
    username: 'square-user',
    group: 'default',
  })
  useSystemConfigStore.getState().setConfig({
    currency: { ...DEFAULT_CURRENCY_CONFIG, quotaDisplayType: 'USD' },
  })
})
afterEach(() => {
  client.clear()
  useAuthStore.getState().auth.setUser(null)
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
  vi.restoreAllMocks()
})

it('marks square models unavailable while effective tariffs load without exposing raw fallback', () => {
  vi.spyOn(api, 'get').mockImplementation(() => new Promise(() => {}))
  const { result } = renderHook(() => usePricingData(true, true), { wrapper })
  expect(result.current.models[0].effective_pricing).toBeNull()
})

it('keeps legacy editor models opt-out while a guest square fails closed', () => {
  useAuthStore.getState().auth.setUser(null)
  const get = vi
    .spyOn(api, 'get')
    .mockImplementation(() => new Promise(() => {}))
  const legacy = renderHook(() => usePricingData(), { wrapper })
  const square = renderHook(() => usePricingData(true, true), { wrapper })
  expect(legacy.result.current.models[0].effective_pricing).toBeUndefined()
  expect(square.result.current.models[0].effective_pricing).toBeNull()
  expect(
    get.mock.calls.some(([url]) => String(url).includes('effective-pricing'))
  ).toBe(false)
})

it('joins authenticated server tariffs to metadata and fails closed after a refresh error', async () => {
  const get = vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        user_group: 'default',
        currency: 'RUB',
        fx: { usd_rate: 85.4594, updated_at: 1 },
        data: [tariff],
      },
    },
  })
  const { result } = renderHook(() => usePricingData(true, true), { wrapper })
  await waitFor(() =>
    expect(result.current.models[0].effective_pricing).toEqual(tariff)
  )
  get.mockRejectedValue(new Error('refresh failed'))
  await client.invalidateQueries({
    predicate: (query) => query.queryKey[0] !== 'pricing',
  })
  await waitFor(() =>
    expect(result.current.models[0].effective_pricing).toBeNull()
  )
})

it.each(['card', 'price', 'cached', 'details'])(
  'renders %s using only server RUB, then unavailable instead of legacy prices',
  (surface) => {
    vi.spyOn(api, 'get').mockImplementation(() => new Promise(() => {}))
    const options = {
      selectedGroup: 'default',
      priceRate: 800,
      usdExchangeRate: 99,
      showRechargePrice: true,
    }
    const effectiveModel = { ...model, effective_pricing: tariff }
    const unavailableModel = { ...model, effective_pricing: null }
    function view(value: PricingModel) {
      if (surface === 'card') {
        return <ModelCard model={value} onClick={vi.fn()} {...options} />
      }
      if (surface === 'price') {
        return <ModelPriceCell model={value} options={options} />
      }
      if (surface === 'cached') {
        return <CachedPriceCell model={value} options={options} />
      }
      return (
        <ModelDetailsContent
          model={value}
          groupRatio={{ default: 99 }}
          usableGroup={{ default: { desc: '', ratio: 99 } }}
          endpointMap={{}}
          autoGroups={[]}
          tokenUnit='K'
          {...options}
        />
      )
    }
    const { rerender, container } = render(view(effectiveModel), { wrapper })
    const expected =
      surface === 'cached' ? '17.09 ₽ / 1M tokens' : '170.92 ₽ / 1M tokens'
    expect(screen.getByText(expected)).toBeInTheDocument()
    expect(container.textContent).not.toContain('$')
    rerender(view(unavailableModel))
    expect(screen.getByText('Tariff unavailable')).toBeInTheDocument()
    expect(screen.queryByText(expected)).not.toBeInTheDocument()
    expect(container.textContent).not.toContain('$')
  }
)

it('filters all-group catalog models using the concrete groups in the server tariff', async () => {
  client.setQueryData<PricingData>(['pricing'], (previous) => {
    if (!previous) throw new Error('Pricing fixture is required')
    return { ...previous, data: [{ ...model, enable_groups: ['all'] }] }
  })
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: { currency: 'RUB', data: [tariff] } },
  })
  const { result } = renderHook(() => usePricingData(true, true), { wrapper })
  await waitFor(() =>
    expect(result.current.models[0].effective_pricing).toEqual(tariff)
  )
  expect(filterByGroup(result.current.models, 'default')).toHaveLength(1)
  expect(filterByGroup(result.current.models, 'unavailable')).toHaveLength(0)
})
