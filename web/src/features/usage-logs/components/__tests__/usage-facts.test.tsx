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
import { render, screen } from '@testing-library/react'
import i18next from 'i18next'
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  test,
} from 'vitest'

import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import type { UsageLog } from '../../data/schema'
import type { LogOtherData } from '../../types'
import { DetailsDialog } from '../dialogs/details-dialog'

const i18nKeys = {
  'Log Details': 'Log Details',
  Consume: 'Consume',
  'Billing Details': 'Billing Details',
  'Billing Mode': 'Billing Mode',
  'Per-token': 'Per-token',
  'Dynamic Pricing': 'Dynamic Pricing',
  'Matched Tier': 'Matched Tier',
  'Group Ratio': 'Group Ratio',
  'Total Cost': 'Total Cost',
  'Usage parameters': 'Usage parameters',
}

function makeLog(other: LogOtherData): UsageLog {
  return {
    id: 1,
    user_id: 1,
    created_at: 1,
    type: 2,
    content: '',
    username: 'user',
    token_name: 'token',
    model_name: 'wan2.5-i2v-preview',
    quota: 5000,
    prompt_tokens: 0,
    completion_tokens: 0,
    use_time: 0,
    is_stream: false,
    channel: 1,
    channel_name: '',
    token_id: 1,
    group: 'default',
    ip: '',
    other: JSON.stringify(other),
    request_id: 'req-1',
    upstream_request_id: '',
  }
}

function renderDetails(
  other: LogOtherData,
  isAdmin = false,
  quota = 5000
): QueryClient {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const freshAt = Date.now() + 60_000
  queryClient.setQueryData(['status'], {}, { updatedAt: freshAt })
  queryClient.setQueryData(
    ['pricing'],
    { data: [], vendors: [] },
    { updatedAt: freshAt }
  )

  render(
    <QueryClientProvider client={queryClient}>
      <DetailsDialog
        log={{ ...makeLog(other), quota }}
        isAdmin={isAdmin}
        isRoot={false}
        open
        onOpenChange={() => undefined}
      />
    </QueryClientProvider>
  )
  return queryClient
}

function rowValue(label: string): string | null {
  return screen.getByText(label).nextElementSibling?.textContent ?? null
}

describe('usage facts billing details', () => {
  const queryClients: QueryClient[] = []
  const previousConfig = useSystemConfigStore.getState().config
  beforeEach(() => {
    useSystemConfigStore
      .getState()
      .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
  })

  beforeAll(() => {
    i18next.addResourceBundle('en', 'translation', i18nKeys)
  })

  afterEach(() => {
    useSystemConfigStore.getState().setConfig(previousConfig)
    for (const queryClient of queryClients) {
      queryClient.clear()
    }
    queryClients.length = 0
  })

  test('renders one raw-key row per usage fact before total cost', () => {
    const expression = 'tier("720P", u("seconds") * 5)'
    queryClients.push(
      renderDetails({
        group_ratio: 1,
        billing_mode: 'tiered_expr',
        expr_b64: Buffer.from(expression, 'utf8').toString('base64'),
        matched_tier: '720P',
        usage_facts: {
          resolution: '720P',
          seconds: 5,
        },
      })
    )

    expect(screen.getByText('Usage parameters')).toBeInTheDocument()
    expect(rowValue('resolution')).toBe('720P')
    expect(rowValue('seconds')).toBe('5')
    expect(rowValue('Billing Mode')).toBe('Dynamic Pricing')
    expect(rowValue('Matched Tier')).toBe('720P')

    const usageHeader = screen.getByText('Usage parameters')
    const totalCost = screen.getByText('Total Cost')
    expect(
      usageHeader.compareDocumentPosition(totalCost) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
  })

  test('does not render usage parameter rows when usage_facts is absent', () => {
    queryClients.push(
      renderDetails({
        group_ratio: 1,
      })
    )

    expect(screen.queryByText('Usage parameters')).toBeNull()
    expect(screen.queryByText('resolution')).toBeNull()
    expect(screen.queryByText('seconds')).toBeNull()
    expect(screen.getByText('Total Cost')).toBeInTheDocument()
  })

  test('does not render usage parameter rows when usage_facts is empty', () => {
    queryClients.push(
      renderDetails({
        group_ratio: 1,
        usage_facts: {},
      })
    )

    expect(screen.queryByText('Usage parameters')).toBeNull()
    expect(screen.queryByText('resolution')).toBeNull()
    expect(screen.queryByText('seconds')).toBeNull()
    expect(screen.getByText('Total Cost')).toBeInTheDocument()
  })
  test.each([
    {
      pure: 1.45,
      effective: 1.305,
      special: undefined,
      label: 'Group Ratio',
      quota: 652500,
      total: '$1.305',
    },
    {
      pure: 0.8,
      effective: 0.72,
      special: 0.8,
      label: 'User Exclusive Ratio',
      quota: 360000,
      total: '$0.72',
    },
    {
      pure: 0,
      effective: 0,
      special: undefined,
      label: 'Group Ratio',
      quota: 0,
      total: '$0',
    },
  ])(
    'admin sees the captured FX calculation for pure=$pure',
    ({ pure, effective, special, label, quota, total }) => {
      queryClients.push(
        renderDetails(
          {
            model_price: 1,
            group_ratio: 9,
            admin_info: {
              billing_fx: {
                schema_version: 1,
                source: 'cbr',
                rate: 90,
                publication_version: 7,
                effective_at: 1789430400,
                fetched_at: 1789430401,
              },
              billing_applied_group: {
                pure_ratio: pure,
                effective_ratio: effective,
                special_ratio: special,
              },
              billing_stage: 'request',
            },
          },
          true,
          quota
        )
      )
      expect(rowValue('Billing Exchange Rate')).toBe('90 RUB/USD')
      expect(rowValue('Exchange Rate Source')).toBe('cbr')
      expect(rowValue('Exchange Rate Multiplier')).toBe('90 / 100 = 0.9x')
      expect(rowValue(label)).toBe(`${pure}x`)
      expect(rowValue('Applied Group Ratio')).toBe(
        `${pure} × 0.9 = ${effective}x`
      )
      expect(rowValue('Model Price')).toBe('$1')
      expect(rowValue('Total Cost')).toBe(total)
    }
  )

  test.each([true, false])(
    'legacy log keeps its ratio without inventing an FX rate (admin=%s)',
    (isAdmin) => {
      queryClients.push(renderDetails({ group_ratio: 1.45 }, isAdmin))
      expect(rowValue('Group Ratio')).toBe('1.4500x')
      expect(
        screen.queryByText('Billing Exchange Rate')
      ).not.toBeInTheDocument()
      expect(screen.queryByText('Applied Group Ratio')).not.toBeInTheDocument()
    }
  )

  test('user view does not expose an admin FX snapshot even when present in the response', () => {
    queryClients.push(
      renderDetails({
        group_ratio: 1.45,
        admin_info: {
          billing_fx: {
            schema_version: 1,
            source: 'cbr',
            rate: 90,
            publication_version: 7,
            effective_at: 1789430400,
            fetched_at: 1789430401,
          },
          billing_applied_group: { pure_ratio: 1.45, effective_ratio: 1.305 },
        },
      })
    )
    expect(rowValue('Group Ratio')).toBe('1.4500x')
    expect(screen.queryByText('Billing Exchange Rate')).not.toBeInTheDocument()
    expect(screen.queryByText('Applied Group Ratio')).not.toBeInTheDocument()
    expect(screen.queryByText('cbr')).not.toBeInTheDocument()
  })

  test.each([0, -90, null, '90'])(
    'invalid captured rate %s does not invent a multiplier',
    (rate) => {
      const other = {
        group_ratio: 1.45,
        admin_info: {
          billing_fx: {
            schema_version: 1,
            source: 'cbr',
            rate,
            publication_version: 7,
            effective_at: 1789430400,
            fetched_at: 1789430401,
          },
          billing_applied_group: { pure_ratio: 1.45, effective_ratio: 1.305 },
        },
      } as unknown as LogOtherData
      queryClients.push(renderDetails(other, true))
      expect(rowValue('Applied Group Ratio')).toBe('1.305x')
      expect(
        screen.queryByText('Exchange Rate Multiplier')
      ).not.toBeInTheDocument()
    }
  )

  test('unknown FX schema retains the recorded effective ratio without interpreting its rate', () => {
    queryClients.push(
      renderDetails(
        {
          admin_info: {
            billing_fx: {
              schema_version: 2,
              source: 'cbr',
              rate: 90,
              publication_version: 7,
              effective_at: 1789430400,
              fetched_at: 1789430401,
            },
            billing_applied_group: { pure_ratio: 1.45, effective_ratio: 1.305 },
          },
        },
        true
      )
    )
    expect(rowValue('Applied Group Ratio')).toBe('1.305x')
    expect(
      screen.queryByText('Exchange Rate Multiplier')
    ).not.toBeInTheDocument()
  })
})
