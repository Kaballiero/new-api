import { zodResolver } from '@hookform/resolvers/zod'
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
import { useForm } from 'react-hook-form'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { Form } from '@/components/ui/form'
import { TooltipProvider } from '@/components/ui/tooltip'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  type ChannelFormValues,
} from '../../../lib/channel-form'
import { channelSchema } from '../../../types'
import { ChannelsProvider } from '../../channels-provider'
import { ChannelMutateDrawer } from '../channel-mutate-drawer'
import { OpenRouterReserveField } from '../sections/openrouter-reserve-field'

function ReserveForm(props: { disabled: boolean }) {
  const form = useForm<ChannelFormValues>({
    resolver: zodResolver(channelFormSchema),
    defaultValues: {
      ...CHANNEL_FORM_DEFAULT_VALUES,
      type: 20,
      name: 'OpenRouter',
      key: 'local',
      models: 'other/reasoner',
      openrouter_model_reserve_usd: '{"other/reasoner":2}',
      openrouter_reservation_source: '{"other/reasoner":"configured_price"}',
    },
    mode: 'onBlur',
  })
  return (
    <Form {...form}>
      <OpenRouterReserveField
        control={form.control}
        disabled={props.disabled}
      />
    </Form>
  )
}

describe('OpenRouter reserve editor', () => {
  test('edits and validates the reservation source', async () => {
    render(<ReserveForm disabled={false} />)
    const editor = screen.getByRole('textbox', {
      name: 'OpenRouter reservation sources',
    })
    expect(editor).toHaveValue('{"other/reasoner":"configured_price"}')
    fireEvent.input(editor, {
      target: { value: '{"other/reasoner":"invalid"}' },
    })
    fireEvent.blur(editor)
    await waitFor(() =>
      expect(
        screen.getByText(
          'Use exact model IDs with upstream or configured_price'
        )
      ).toBeVisible()
    )
  })
  test('shows current policy and reports invalid amounts at the field', async () => {
    render(<ReserveForm disabled={false} />)
    const editor = screen.getByRole('textbox', {
      name: 'OpenRouter model reserves (USD)',
    })
    expect(editor).toHaveValue('{"other/reasoner":2}')
    fireEvent.input(editor, { target: { value: '{"other/reasoner":0}' } })
    fireEvent.blur(editor)
    await waitFor(() =>
      expect(
        screen.getByText('Use exact model IDs and finite positive USD amounts')
      ).toBeVisible()
    )
    fireEvent.input(editor, { target: { value: '{}' } })
    fireEvent.blur(editor)
    await waitFor(() =>
      expect(
        screen.queryByText(
          'Use exact model IDs and finite positive USD amounts'
        )
      ).not.toBeInTheDocument()
    )
  })
  test('disables editing when sensitive settings are locked or submission is pending', () => {
    render(<ReserveForm disabled />)
    expect(
      screen.getByRole('textbox', { name: 'OpenRouter model reserves (USD)' })
    ).toBeDisabled()
    expect(
      screen.getByRole('textbox', { name: 'OpenRouter reservation sources' })
    ).toBeDisabled()
  })
})

afterEach(() => {
  useAuthStore.getState().auth.setUser(null)
  vi.restoreAllMocks()
})

describe('channel drawer reserve settings', () => {
  test.each([20, 28, 1])(
    'shows reserve editors only for OpenRouter type 20 (channel type %s)',
    async (type) => {
      const channel = channelSchema.parse({
        id: 69,
        type,
        name: 'Reserve channel',
        key: '',
        status: 1,
        created_time: 0,
        test_time: 0,
        response_time: 0,
        balance_updated_time: 0,
        models: 'qwen/qwen3-reranker-8b',
        group: 'default',
        setting:
          '{"openrouter_model_reserve_usd":{"qwen/qwen3-reranker-8b":0.02}}',
      })
      useAuthStore
        .getState()
        .auth.setUser({ id: 1, username: 'admin', role: 100 })
      const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
        if (url === '/api/channel/69') {
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
        expect(screen.getByDisplayValue('Reserve channel')).toBeVisible()
      )
      expect(get).toHaveBeenCalledWith('/api/channel/69')
      const reserve = screen.queryByRole('textbox', {
        name: 'OpenRouter model reserves (USD)',
      })
      const source = screen.queryByRole('textbox', {
        name: 'OpenRouter reservation sources',
      })
      if (type === 20) {
        expect(reserve).toBeVisible()
        expect(source).toBeVisible()
      } else {
        expect(reserve).not.toBeInTheDocument()
        expect(source).not.toBeInTheDocument()
      }
    }
  )
})
