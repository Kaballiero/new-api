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
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
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

function ReserveForm(props: {
  disabled: boolean
  reserves?: string
  sources?: string
}) {
  const form = useForm<ChannelFormValues>({
    resolver: zodResolver(channelFormSchema),
    defaultValues: {
      ...CHANNEL_FORM_DEFAULT_VALUES,
      type: 20,
      name: 'OpenRouter',
      key: 'local',
      models: 'other/reasoner',
      openrouter_model_reserve_usd: props.reserves ?? '{"other/reasoner":2}',
      openrouter_reservation_source:
        props.sources ?? '{"other/reasoner":"configured_price"}',
    },
    mode: 'onBlur',
  })
  return (
    <Form {...form}>
      <OpenRouterReserveField
        control={form.control}
        disabled={props.disabled}
        modelOptions={['openai/gpt-audio', 'openai/gpt-5-mini']}
      />
      <output data-testid='reserve-value'>
        {form.watch('openrouter_model_reserve_usd')}
      </output>
      <output data-testid='source-value'>
        {form.watch('openrouter_reservation_source')}
      </output>
      <button
        type='button'
        onClick={() =>
          form.reset({
            ...form.getValues(),
            openrouter_model_reserve_usd: '{"openai/gpt-audio":0.1}',
          })
        }
      >
        Load another channel
      </button>
    </Form>
  )
}

function reserveTable() {
  return within(
    screen.getByRole('group', { name: 'OpenRouter model reserves (USD)' })
  )
}

function sourceTable() {
  return within(
    screen.getByRole('group', { name: 'OpenRouter reservation sources' })
  )
}

function storedValue(testId: string): unknown {
  return JSON.parse(screen.getByTestId(testId).textContent || '')
}

describe('OpenRouter reserve table editor', () => {
  test('saved policies render as model and value rows', () => {
    render(<ReserveForm disabled={false} />)
    expect(
      reserveTable().getByRole('combobox', { name: 'Upstream model ID 1' })
    ).toHaveValue('other/reasoner')
    expect(
      reserveTable().getByRole('spinbutton', { name: 'Reserve, USD 1' })
    ).toHaveValue(2)
    expect(
      sourceTable().getByRole('combobox', { name: 'Upstream model ID 1' })
    ).toHaveValue('other/reasoner')
    expect(
      sourceTable().getByRole('combobox', { name: 'Reservation source 1' })
    ).toHaveTextContent('configured_price')
  })

  test('adding a reserve row with a positive amount stores the same JSON map', () => {
    render(<ReserveForm disabled={false} />)
    fireEvent.click(reserveTable().getByRole('button', { name: 'Add Row' }))
    fireEvent.change(
      reserveTable().getByRole('combobox', { name: 'Upstream model ID 2' }),
      { target: { value: 'openai/gpt-audio' } }
    )
    fireEvent.change(
      reserveTable().getByRole('spinbutton', { name: 'Reserve, USD 2' }),
      { target: { value: '0.1' } }
    )
    expect(storedValue('reserve-value')).toEqual({
      'other/reasoner': 2,
      'openai/gpt-audio': 0.1,
    })
  })

  test('removing the only reserve row stores an empty object', () => {
    render(<ReserveForm disabled={false} />)
    fireEvent.click(
      reserveTable().getByRole('button', { name: 'Remove row 1' })
    )
    expect(storedValue('reserve-value')).toEqual({})
    expect(
      reserveTable().getByText('No rows. Click "Add Row" to configure a model.')
    ).toBeVisible()
  })

  test('adding a source row defaults to configured_price', () => {
    render(<ReserveForm disabled={false} sources='' />)
    fireEvent.click(sourceTable().getByRole('button', { name: 'Add Row' }))
    fireEvent.change(
      sourceTable().getByRole('combobox', { name: 'Upstream model ID 1' }),
      { target: { value: 'openai/gpt-5-mini' } }
    )
    expect(storedValue('source-value')).toEqual({
      'openai/gpt-5-mini': 'configured_price',
    })
  })

  test('non-positive amount shows inline and field errors', async () => {
    render(<ReserveForm disabled={false} />)
    const amount = reserveTable().getByRole('spinbutton', {
      name: 'Reserve, USD 1',
    })
    fireEvent.change(amount, { target: { value: '0' } })
    fireEvent.blur(amount)
    expect(amount).toHaveAttribute('aria-invalid', 'true')
    expect(
      reserveTable().getByText('Enter a positive USD amount')
    ).toBeVisible()
    await waitFor(() =>
      expect(
        screen.getByText('Use exact model IDs and finite positive USD amounts')
      ).toBeVisible()
    )
  })

  test('duplicate model IDs are flagged on both rows', () => {
    render(<ReserveForm disabled={false} />)
    fireEvent.click(reserveTable().getByRole('button', { name: 'Add Row' }))
    fireEvent.change(
      reserveTable().getByRole('combobox', { name: 'Upstream model ID 2' }),
      { target: { value: 'other/reasoner' } }
    )
    fireEvent.change(
      reserveTable().getByRole('spinbutton', { name: 'Reserve, USD 2' }),
      { target: { value: '1' } }
    )
    expect(reserveTable().getAllByText('Duplicate model ID')).toHaveLength(2)
    expect(Array.isArray(storedValue('reserve-value'))).toBe(true)
  })

  test('model ID with whitespace shows an inline error', () => {
    render(<ReserveForm disabled={false} />)
    const model = sourceTable().getByRole('combobox', {
      name: 'Upstream model ID 1',
    })
    fireEvent.change(model, { target: { value: 'other/reasoner ' } })
    expect(model).toHaveAttribute('aria-invalid', 'true')
    expect(
      sourceTable().getByText('Model IDs cannot contain spaces or *')
    ).toBeVisible()
  })

  test('model ID inputs suggest the channel upstream models', () => {
    render(<ReserveForm disabled={false} />)
    const model = reserveTable().getByRole('combobox', {
      name: 'Upstream model ID 1',
    })
    const listId = model.getAttribute('list')
    expect(listId).toBeTruthy()
    const options = [
      ...document.querySelectorAll(`datalist[id="${listId}"] option`),
    ].map((option) => option.getAttribute('value'))
    expect(options).toEqual(['openai/gpt-audio', 'openai/gpt-5-mini'])
  })

  test('reserve table is labelled and announces its description and field error', async () => {
    render(<ReserveForm disabled={false} />)
    const group = screen.getByRole('group', {
      name: 'OpenRouter model reserves (USD)',
    })
    expect(
      screen.getByText('OpenRouter model reserves (USD)', {
        selector: 'label',
      })
    ).toHaveAttribute('for', group.id)
    expect(group).toHaveAccessibleDescription(
      expect.stringContaining('hosted web search (web_search_options)')
    )
    const amount = reserveTable().getByRole('spinbutton', {
      name: 'Reserve, USD 1',
    })
    fireEvent.change(amount, { target: { value: '0' } })
    fireEvent.blur(amount)
    await waitFor(() =>
      expect(group).toHaveAccessibleDescription(
        expect.stringContaining(
          'Use exact model IDs and finite positive USD amounts'
        )
      )
    )
    expect(group).toHaveAttribute('aria-invalid', 'true')
  })

  test('external form reset rebuilds the table rows', () => {
    render(<ReserveForm disabled={false} />)
    fireEvent.click(
      screen.getByRole('button', { name: 'Load another channel' })
    )
    expect(
      reserveTable().getByRole('combobox', { name: 'Upstream model ID 1' })
    ).toHaveValue('openai/gpt-audio')
    expect(
      reserveTable().getByRole('spinbutton', { name: 'Reserve, USD 1' })
    ).toHaveValue(0.1)
    expect(
      reserveTable().queryByRole('combobox', { name: 'Upstream model ID 2' })
    ).not.toBeInTheDocument()
  })

  test('disables editing when sensitive settings are locked or submission is pending', () => {
    render(<ReserveForm disabled />)
    expect(
      reserveTable().getByRole('combobox', { name: 'Upstream model ID 1' })
    ).toBeDisabled()
    expect(
      reserveTable().getByRole('button', { name: 'Add Row' })
    ).toBeDisabled()
    expect(
      sourceTable().getByRole('button', { name: 'Remove row 1' })
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
      const reserve = screen.queryByRole('group', {
        name: 'OpenRouter model reserves (USD)',
      })
      const source = screen.queryByRole('group', {
        name: 'OpenRouter reservation sources',
      })
      if (type === 20) {
        expect(reserve).toBeVisible()
        expect(source).toBeVisible()
        expect(
          within(reserve as HTMLElement).getByRole('combobox', {
            name: 'Upstream model ID 1',
          })
        ).toHaveValue('qwen/qwen3-reranker-8b')
      } else {
        expect(reserve).not.toBeInTheDocument()
        expect(source).not.toBeInTheDocument()
      }
    }
  )
})
