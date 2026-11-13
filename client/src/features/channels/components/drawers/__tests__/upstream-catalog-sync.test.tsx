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
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { TooltipProvider } from '@/components/ui/tooltip'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { channelSchema } from '../../../types'
import { ChannelsProvider } from '../../channels-provider'
import { ChannelMutateDrawer } from '../channel-mutate-drawer'

const CATALOG_SYNC_LABEL = 'Use the GetAPI catalog as the model source'

async function renderChannelDrawer(type: number) {
  const channel = channelSchema.parse({
    id: 71,
    type,
    name: 'Upstream checked channel',
    key: '',
    status: 1,
    created_time: 0,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
    models: 'openai/gpt-5-mini',
    group: 'default',
    settings: JSON.stringify({
      upstream_model_update_check_enabled: true,
      upstream_model_update_catalog_sync_enabled: type === 20,
    }),
  })
  useAuthStore.getState().auth.setUser({ id: 1, username: 'admin', role: 100 })
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/channel/71') {
      return { data: { success: true, data: channel } }
    }
    return { data: { success: true, data: [] } }
  })
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <ChannelsProvider>
          <ChannelMutateDrawer
            open
            onOpenChange={() => undefined}
            currentRow={channel}
          />
        </ChannelsProvider>
      </TooltipProvider>
    </QueryClientProvider>
  )
  await waitFor(() =>
    expect(screen.getByDisplayValue('Upstream checked channel')).toBeVisible()
  )
}

afterEach(() => {
  useAuthStore.getState().auth.setUser(null)
})

describe('channel drawer upstream catalog sync', () => {
  test('OpenRouter switch is available only while upstream checks are on', async () => {
    await renderChannelDrawer(20)
    const catalogSync = screen.getByRole('switch', { name: CATALOG_SYNC_LABEL })
    expect(catalogSync).toBeChecked()
    expect(catalogSync).not.toHaveAttribute('aria-disabled', 'true')

    fireEvent.click(
      screen.getByRole('switch', { name: 'Upstream Model Update Check' })
    )

    expect(catalogSync).toHaveAttribute('aria-disabled', 'true')
  })

  test('other channel types do not show the switch', async () => {
    await renderChannelDrawer(1)
    expect(
      screen.getByRole('switch', { name: 'Upstream Model Update Check' })
    ).toBeVisible()
    expect(
      screen.queryByRole('switch', { name: CATALOG_SYNC_LABEL })
    ).not.toBeInTheDocument()
  })
})
