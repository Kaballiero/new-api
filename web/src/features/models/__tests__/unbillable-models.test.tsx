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
import {
  createRootRoute,
  createRoute,
  createRouter,
  createMemoryHistory,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen, cleanup, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { ModelsDialogs } from '../components/models-dialogs'
import { ModelsPrimaryButtons } from '../components/models-primary-buttons'
import { ModelsProvider } from '../components/models-provider'
import type { UnbillableModelsResponse } from '../types'

const clients: QueryClient[] = []

function Page() {
  return (
    <ModelsProvider>
      <ModelsPrimaryButtons />
      <ModelsDialogs />
    </ModelsProvider>
  )
}

async function renderButtons(response: UnbillableModelsResponse) {
  useAuthStore.getState().auth.setUser({ id: 1, username: 'admin', role: 100 })
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/models/unbillable') return { data: response }
    return { data: { success: true, data: { items: [] } } }
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  const root = createRootRoute()
  const models = createRoute({
    getParentRoute: () => root,
    path: 'models/$section',
    component: Page,
  })
  const router = createRouter({
    routeTree: root.addChildren([models]),
    history: createMemoryHistory({ initialEntries: ['/models/metadata'] }),
  })
  await router.load()
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  await screen.findByRole('button', { name: 'Add Model' })
  return { get }
}

afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  useAuthStore.getState().auth.reset()
  vi.restoreAllMocks()
})

it('shows a counter for models without tariff and explains what to configure', async () => {
  await renderButtons({
    success: true,
    data: [
      {
        model_name: 'eleven-v4',
        catalog_type: 'speech',
        reason: 'no_billing_config',
        suggested_reserve_usd: 0.1,
      },
      { model_name: 'argon:batch', reason: 'no_billing_config' },
      { model_name: 'deepseek-r1', reason: 'no_catalog_price' },
    ],
  })

  await userEvent.click(
    await screen.findByRole('button', { name: 'Without tariff: 3' })
  )

  const dialog = await screen.findByRole('dialog')
  expect(within(dialog).getByText('Models without tariff')).toBeTruthy()
  expect(within(dialog).getByText('eleven-v4')).toBeTruthy()
  expect(within(dialog).getAllByText('No tariff or reserve')).toHaveLength(2)
  expect(within(dialog).getByText('No catalog price')).toBeTruthy()
  expect(
    within(dialog).getByText(
      'Set a price, ratio or tiered expression, or an OpenRouter reserve on the channel. Suggested reserve: 0.1 USD'
    )
  ).toBeTruthy()
  expect(
    within(dialog).getByText(
      'Set a tiered billing expression for the batch model.'
    )
  ).toBeTruthy()
  expect(
    within(dialog).getByText(
      'The catalog has no price for this model: disable it or check the provider.'
    )
  ).toBeTruthy()
})

it('hides the counter when every enabled model has a tariff', async () => {
  const { get } = await renderButtons({ success: true, data: [] })

  expect(get).toHaveBeenCalledWith('/api/models/unbillable')
  expect(screen.queryByRole('button', { name: /Without tariff/ })).toBeNull()

  await userEvent.click(screen.getByRole('button', { name: 'Open menu' }))
  await userEvent.click(
    await screen.findByRole('menuitem', { name: /Models without tariff/ })
  )
  expect(
    await screen.findByText('All enabled models have a tariff.')
  ).toBeTruthy()
})

it('keeps the page quiet when the list fails and shows the error in the dialog', async () => {
  await renderButtons({ success: false, message: 'pricing unavailable' })

  expect(screen.queryByRole('button', { name: /Without tariff/ })).toBeNull()
  await userEvent.click(screen.getByRole('button', { name: 'Open menu' }))
  await userEvent.click(
    await screen.findByRole('menuitem', { name: /Models without tariff/ })
  )
  expect(await screen.findByText('pricing unavailable')).toBeTruthy()
})
