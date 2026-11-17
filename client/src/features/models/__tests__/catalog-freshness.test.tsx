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
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { CatalogFreshnessBadge } from '../components/catalog-freshness-badge'
import type { CatalogFreshnessResponse } from '../types'

const clients: QueryClient[] = []

function renderBadge(response: CatalogFreshnessResponse) {
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/models/catalog_freshness') return { data: response }
    throw new Error(`unexpected request ${url}`)
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  return render(
    <QueryClientProvider client={client}>
      <CatalogFreshnessBadge />
    </QueryClientProvider>
  )
}

afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  vi.restoreAllMocks()
})

it('shows the catalog fetch time with the max age when the catalog is fresh', async () => {
  renderBadge({
    success: true,
    data: { fetched_at: 1795435200, max_age_hours: 24, stale: false },
  })

  const badge = await screen.findByText(/^Catalog: \d{4}-\d{2}-\d{2} /)
  expect(badge.closest('[data-slot="status-badge"]')).toHaveAttribute(
    'title',
    'OpenRouter reserves are priced from the catalog while it is at most 24 h old'
  )
})

it('marks the catalog stale when it is older than the max age', async () => {
  renderBadge({
    success: true,
    data: { fetched_at: 1795435200, max_age_hours: 24, stale: true },
  })

  expect(
    await screen.findByText(/^Catalog stale: \d{4}-\d{2}-\d{2} /)
  ).toBeVisible()
})

it('hides the badge without a published catalog', async () => {
  const { container } = renderBadge({
    success: true,
    data: { fetched_at: 0, max_age_hours: 24, stale: false },
  })

  await vi.waitFor(() => expect(api.get).toHaveBeenCalled())
  expect(container).toBeEmptyDOMElement()
})
