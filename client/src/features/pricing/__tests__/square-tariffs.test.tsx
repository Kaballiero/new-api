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

it.each(['card', 'price', 'cached', 'details'])(
  'renders OR-only %s tariffs unavailable when only legacy prices exist',
  (surface) => {
    const value: PricingModel = {
      ...model,
      billing_sources_by_group: {
        default: { openai: ['openrouter_actual_cost'] },
      },
      indicative_billing_expr_by_endpoint: { openai: '' },
    }
    let view: ReactNode
    if (surface === 'card') view = <ModelCard model={value} onClick={vi.fn()} />
    else if (surface === 'price') view = <ModelPriceCell model={value} />
    else if (surface === 'cached') {
      view = <CachedPriceCell model={value} options={{}} />
    } else {
      view = (
        <ModelDetailsContent
          model={value}
          groupRatio={{ default: 1 }}
          usableGroup={{ default: { desc: '', ratio: 1 } }}
          endpointMap={{}}
          autoGroups={[]}
          priceRate={1}
          usdExchangeRate={1}
          tokenUnit='M'
        />
      )
    }
    const { container } = render(view, { wrapper })
    expect(screen.getByText('Tariff unavailable')).toBeInTheDocument()
    expect(container.textContent).not.toContain('990')
    expect(
      screen.getByText(/OpenRouter prices are indicative/)
    ).toBeInTheDocument()
  }
)

it('shows direct legacy pricing alongside an unavailable OR tariff in a mixed scope', () => {
  render(
    <ModelPriceCell
      model={{
        ...model,
        billing_mode: undefined,
        billing_expr: undefined,
        billing_sources_by_group: {
          default: { openai: ['configured_tariff', 'openrouter_actual_cost'] },
        },
        indicative_billing_expr_by_endpoint: { openai: '' },
      }}
    />
  )
  expect(screen.getByText('Tariff unavailable')).toBeInTheDocument()
  expect(screen.getByText('Input')).toBeInTheDocument()
  expect(screen.getByText('Output')).toBeInTheDocument()
})

it('takes OR indicative amounts only from its operation expression and selected group', () => {
  const { container } = render(
    <ModelPriceCell
      model={{
        ...model,
        group_ratio: { default: 2, premium: 5 },
        enable_groups: ['default', 'premium'],
        billing_sources_by_group: {
          default: { openai: ['configured_tariff'] },
          premium: {
            openai: ['openrouter_actual_cost'],
            openai_batch: ['openrouter_actual_cost'],
          },
        },
        indicative_billing_expr_by_endpoint: {
          openai: 'tier("display", p * 2 + c * 4)',
          openai_batch: '',
        },
      }}
      options={{ selectedGroup: 'premium' }}
    />
  )
  expect(screen.getByText('From 10')).toBeInTheDocument()
  expect(screen.getByText('From 20')).toBeInTheDocument()
  expect(screen.getByText('Tariff unavailable')).toBeInTheDocument()
  expect(container.textContent).not.toContain('990')
})

it.each(['price', 'card', 'cached'])(
  'uses the cheaper later OR tier for the raw %s starting price',
  (surface) => {
    const value: PricingModel = {
      ...model,
      group_ratio: { default: 2 },
      billing_sources_by_group: {
        default: { openai: ['openrouter_actual_cost'] },
      },
      indicative_billing_expr_by_endpoint: {
        openai:
          'len <= 100 ? tier("short", p * 10 + c * 20 + cr * 1) : tier("long", p * 2 + c * 4 + cr * 0.5)',
      },
    }
    let view: ReactNode = (
      <ModelPriceCell model={value} options={{ selectedGroup: 'default' }} />
    )
    if (surface === 'card') {
      view = (
        <ModelCard model={value} onClick={vi.fn()} selectedGroup='default' />
      )
    }
    if (surface === 'cached') {
      view = (
        <CachedPriceCell model={value} options={{ selectedGroup: 'default' }} />
      )
    }
    render(view)
    if (surface === 'cached') {
      expect(screen.getByText('From 1')).toBeInTheDocument()
    } else {
      expect(screen.getByText('From 4')).toBeInTheDocument()
      expect(screen.getByText('From 8')).toBeInTheDocument()
      expect(screen.queryByText('From 20')).not.toBeInTheDocument()
    }
  }
)

it('preserves full direct and OR raw details with conditions, cache and audio categories', () => {
  const value: PricingModel = {
    ...model,
    group_ratio: { default: 2 },
    billing_expr: 'tier("direct-standard", p * 3 + c * 6 + cr * 0.3 + ai * 8)',
    billing_sources_by_group: {
      default: { openai: ['configured_tariff', 'openrouter_actual_cost'] },
    },
    indicative_billing_expr_by_endpoint: {
      openai:
        'len <= 100 ? tier("short", p * 10 + c * 20 + cr * 1 + ai * 12) : tier("long", p * 2 + c * 4 + cr * 0.5 + ai * 5)',
    },
  }
  render(
    <ModelDetailsContent
      model={value}
      groupRatio={{ default: 2 }}
      usableGroup={{ default: { desc: '', ratio: 2 } }}
      endpointMap={{}}
      autoGroups={[]}
      priceRate={1}
      usdExchangeRate={1}
      tokenUnit='M'
    />,
    { wrapper }
  )
  expect(screen.getAllByText('direct-standard').length).toBeGreaterThan(0)
  expect(screen.getAllByText('short').length).toBeGreaterThan(0)
  expect(screen.getAllByText('long').length).toBeGreaterThan(0)
  expect(
    screen.getAllByRole('columnheader', { name: 'Cache Read' }).length
  ).toBeGreaterThan(1)
  expect(
    screen.getAllByRole('columnheader', { name: 'Audio In' }).length
  ).toBeGreaterThan(1)
  expect(screen.getAllByText(/≤ 100/).length).toBeGreaterThan(0)
  expect(screen.getAllByText(/\$24/).length).toBeGreaterThan(0)
})

it('shows all three OR task components and the fixed additional charge in raw details', () => {
  const value: PricingModel = {
    ...model,
    group_ratio: { default: 2 },
    billing_usage_schema: {
      seconds: {
        type: 'number',
        unit: 'second',
        description: { en: 'Duration charge' },
      },
      clips: {
        type: 'number',
        unit: 'count',
        description: { en: 'Clip charge' },
      },
      units: {
        type: 'number',
        unit: 'credit',
        description: { en: 'Credit charge' },
      },
    },
    billing_sources_by_group: {
      default: { 'openai-video': ['openrouter_actual_cost'] },
    },
    indicative_billing_expr_by_endpoint: {
      'openai-video':
        'tier("video", 0.1 + u("seconds") * 0.4 + u("clips") * 0.2 + u("units") * 0.3)',
    },
  }
  render(
    <ModelDetailsContent
      model={value}
      groupRatio={{ default: 2 }}
      usableGroup={{ default: { desc: '', ratio: 2 } }}
      endpointMap={{}}
      autoGroups={[]}
      priceRate={1}
      usdExchangeRate={1}
      tokenUnit='M'
    />,
    { wrapper }
  )
  for (const label of [
    'Duration charge',
    'Clip charge',
    'Credit charge',
    'Additional charge',
  ]) {
    expect(
      screen.getByRole('columnheader', { name: label })
    ).toBeInTheDocument()
  }
  expect(screen.getAllByText(/\$0.2\/request/).length).toBeGreaterThan(0)
})
