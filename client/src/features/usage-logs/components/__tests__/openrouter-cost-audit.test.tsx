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
import { render, screen } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

import type { LogOtherData } from '../../types'
import { OpenRouterCostAudit } from '../dialogs/openrouter-cost-audit'

const settledInfo: NonNullable<LogOtherData['admin_info']> = {
  openrouter_generation_id: 'gen-123',
  openrouter_cost_source: 'generation_lookup',
  openrouter_cost_basis: {
    version: 'openrouter_cost_v2',
    effective_group_ratio: 1,
    quota_per_unit: 500000,
    estimated_usd: 0.02,
  },
  upstream_cost_usd: 0.015,
  reserved_quota: 10000,
}

describe('OpenRouter cost audit', () => {
  test('settled request shows generation, cost source and upstream cost', () => {
    render(<OpenRouterCostAudit adminInfo={settledInfo} />)

    expect(screen.getByText('gen-123')).toBeVisible()
    expect(screen.getByText('generation_lookup')).toBeVisible()
    expect(screen.getByText('0.015 USD')).toBeVisible()
    expect(screen.getByText(/openrouter_cost_v2/)).toBeVisible()
    expect(
      screen.queryByText('OpenRouter charge unknown')
    ).not.toBeInTheDocument()
  })

  test('request with an unknown financial outcome shows a warning with its reason', () => {
    render(
      <OpenRouterCostAudit
        adminInfo={{
          ...settledInfo,
          upstream_cost_usd: undefined,
          openrouter_financial_outcome: 'unknown',
          openrouter_unknown_reason: 'stream_interrupted',
        }}
      />
    )

    expect(screen.getByText('OpenRouter charge unknown')).toBeVisible()
    expect(screen.getByText('stream_interrupted')).toBeVisible()
  })

  test('request without an OpenRouter cost basis renders nothing', () => {
    const { container } = render(
      <OpenRouterCostAudit adminInfo={{ use_channel: [1] }} />
    )

    expect(container).toBeEmptyDOMElement()
  })
})
