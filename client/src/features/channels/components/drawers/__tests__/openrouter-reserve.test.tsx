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
import { zodResolver } from '@hookform/resolvers/zod'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { useForm } from 'react-hook-form'
import { afterEach, beforeAll, describe, expect, test, vi } from 'vitest'

import { Form } from '@/components/ui/form'
import { TooltipProvider } from '@/components/ui/tooltip'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  type ChannelFormValues,
} from '../../../lib/channel-form'
import { channelSchema, type OpenRouterReserves } from '../../../types'
import { ChannelsProvider } from '../../channels-provider'
import { ChannelMutateDrawer } from '../channel-mutate-drawer'
import { OpenRouterReserveField } from '../sections/openrouter-reserve-field'

const reservesPreview: OpenRouterReserves = {
  fetched_at: '2026-11-17T15:42:00Z',
  catalog_stale: true,
  reference: {
    operation: 'chat/completions',
    prompt_tokens: 1000,
    output_tokens: 8192,
  },
  items: [
    {
      model: 'openai/gpt-5-mini',
      upstream_model: 'openai/gpt-5-mini',
      in_channel: true,
      configured_source: 'upstream',
      manual_usd: null,
      configured_usd: null,
      effective_source: 'catalog',
      admission_policy: 'upstream_estimate',
      reference_operation: 'chat/completions',
      example_usd: 0.0571,
      example_status: 'ok',
      example_error: null,
      providers: [
        { tag: 'openai', tier: 'default', usd: 0.0571 },
        { tag: 'azure', tier: 'flex', usd: 0.0312 },
        { tag: 'groq', tier: 'priority', usd: null },
      ],
    },
    {
      model: 'other/reasoner',
      upstream_model: 'other/reasoner',
      in_channel: true,
      configured_source: 'configured_price',
      manual_usd: 2,
      configured_usd: 0.4,
      effective_source: 'live',
      admission_policy: 'model_reserve_add_on',
      reference_operation: null,
      example_usd: null,
      example_status: 'live_only',
      example_error: null,
      providers: [],
    },
  ],
}

function mockReservesResponse(data: unknown) {
  return vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/channel/7/openrouter_reserves') return { data }
    throw new Error(`unexpected request ${url}`)
  })
}

function ReserveForm(props: {
  disabled?: boolean
  channelId?: number | null
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
      models: 'openai/gpt-5-mini,other/reasoner',
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
        disabled={props.disabled ?? false}
        channelId={props.channelId === undefined ? 7 : props.channelId}
        upstreamModels={['openai/gpt-5-mini', 'other/reasoner']}
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
            openrouter_model_reserve_usd: '{"openai/gpt-5-mini":0.1}',
            openrouter_reservation_source: '{}',
          })
        }
      >
        Load another channel
      </button>
    </Form>
  )
}

function renderReserveForm(props: Parameters<typeof ReserveForm>[0] = {}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <ReserveForm {...props} />
      </TooltipProvider>
    </QueryClientProvider>
  )
}

function reserveRow(model: string) {
  const row = screen
    .getByRole('combobox', { name: `Reservation source for ${model}` })
    .closest('tr')
  if (!row) throw new Error(`no table row for ${model}`)
  return within(row)
}

function manualInput(model: string) {
  return screen.getByRole('spinbutton', {
    name: `Manual reserve, USD for ${model}`,
  })
}

function storedValue(testId: string): unknown {
  return JSON.parse(screen.getByTestId(testId).textContent || '')
}

// Model IDs contain '/'; like the app config, leave escaping to React.
beforeAll(async () => {
  await i18next.init({
    lng: 'en',
    resources: { en: { translation: {} } },
    interpolation: { escapeValue: false },
  })
})

afterEach(() => {
  useAuthStore.getState().auth.setUser(null)
  vi.restoreAllMocks()
})

describe('OpenRouter reserve table', () => {
  test('rows list the channel models, then setting keys the channel lacks', () => {
    mockReservesResponse({ success: true, data: reservesPreview })
    renderReserveForm({
      sources:
        '{"other/reasoner":"configured_price","legacy/model":"upstream"}',
    })
    const group = screen.getByRole('group', { name: 'OpenRouter reserves' })
    expect(group).toHaveAccessibleDescription(
      expect.stringContaining(
        'holds the larger of the estimate and this amount'
      )
    )
    expect(
      within(group)
        .getAllByRole('combobox', { name: /^Reservation source for / })
        .map((select) => select.getAttribute('aria-label'))
    ).toEqual([
      'Reservation source for openai/gpt-5-mini',
      'Reservation source for other/reasoner',
      'Reservation source for legacy/model',
    ])
    expect(manualInput('other/reasoner')).toHaveValue(2)
    expect(manualInput('openai/gpt-5-mini')).toHaveValue(null)
    expect(
      reserveRow('other/reasoner').getByRole('combobox', {
        name: 'Reservation source for other/reasoner',
      })
    ).toHaveTextContent('configured_price')
    expect(
      reserveRow('openai/gpt-5-mini').getByRole('combobox', {
        name: 'Reservation source for openai/gpt-5-mini',
      })
    ).toHaveTextContent('upstream')
  })

  test('only rows of models the channel lacks can be removed', () => {
    mockReservesResponse({ success: true, data: reservesPreview })
    renderReserveForm({ reserves: '{"other/reasoner":2,"legacy/model":0.5}' })
    expect(
      reserveRow('legacy/model').getByRole('button', {
        name: 'Remove legacy/model',
      })
    ).toBeVisible()
    expect(reserveRow('legacy/model').getByText('Not in channel')).toBeVisible()
    expect(
      reserveRow('other/reasoner').queryByRole('button', {
        name: 'Remove other/reasoner',
      })
    ).not.toBeInTheDocument()
  })

  test('removing a row the channel lacks drops the model from both maps', () => {
    mockReservesResponse({ success: true, data: reservesPreview })
    renderReserveForm({
      reserves: '{"other/reasoner":2,"legacy/model":0.5}',
      sources: '{"legacy/model":"configured_price"}',
    })
    fireEvent.click(screen.getByRole('button', { name: 'Remove legacy/model' }))
    expect(storedValue('reserve-value')).toEqual({ 'other/reasoner': 2 })
    expect(storedValue('source-value')).toEqual({})
    expect(
      screen.queryByRole('combobox', {
        name: 'Reservation source for legacy/model',
      })
    ).not.toBeInTheDocument()
  })

  test('entering a manual amount adds the model to the reserve map', () => {
    mockReservesResponse({ success: true, data: reservesPreview })
    renderReserveForm()
    fireEvent.change(manualInput('openai/gpt-5-mini'), {
      target: { value: '0.1' },
    })
    expect(storedValue('reserve-value')).toEqual({
      'other/reasoner': 2,
      'openai/gpt-5-mini': 0.1,
    })
  })

  test('clearing a manual amount drops the model and keeps its row', () => {
    mockReservesResponse({ success: true, data: reservesPreview })
    renderReserveForm({ reserves: '{"legacy/model":0.5}' })
    fireEvent.change(manualInput('legacy/model'), { target: { value: '' } })
    expect(storedValue('reserve-value')).toEqual({})
    expect(manualInput('legacy/model')).toHaveValue(null)
  })

  test('choosing a reservation source stores it in the source map', async () => {
    mockReservesResponse({ success: true, data: reservesPreview })
    renderReserveForm()
    const user = userEvent.setup()
    await user.click(
      screen.getByRole('combobox', {
        name: 'Reservation source for openai/gpt-5-mini',
      })
    )
    await user.click(
      await screen.findByRole('option', { name: 'configured_price' })
    )
    expect(storedValue('source-value')).toEqual({
      'other/reasoner': 'configured_price',
      'openai/gpt-5-mini': 'configured_price',
    })
  })

  test('choosing upstream drops the model from the source map', async () => {
    mockReservesResponse({ success: true, data: reservesPreview })
    renderReserveForm()
    const user = userEvent.setup()
    await user.click(
      screen.getByRole('combobox', {
        name: 'Reservation source for other/reasoner',
      })
    )
    await user.click(await screen.findByRole('option', { name: 'upstream' }))
    expect(storedValue('source-value')).toEqual({})
    expect(
      screen.getByRole('combobox', {
        name: 'Reservation source for other/reasoner',
      })
    ).toHaveTextContent('upstream')
  })

  test('non-positive amount marks the input invalid and blocks the form', async () => {
    mockReservesResponse({ success: true, data: reservesPreview })
    renderReserveForm()
    const amount = manualInput('other/reasoner')
    fireEvent.change(amount, { target: { value: '0' } })
    fireEvent.blur(amount)
    expect(amount).toHaveAttribute('aria-invalid', 'true')
    expect(amount).toHaveAccessibleDescription('Enter a positive USD amount')
    expect(
      reserveRow('other/reasoner').getByText('Enter a positive USD amount')
    ).toBeVisible()
    await waitFor(() =>
      expect(
        screen.getByRole('group', { name: 'OpenRouter reserves' })
      ).toHaveAccessibleDescription(
        expect.stringContaining(
          'Use exact model IDs and finite positive USD amounts'
        )
      )
    )
  })

  test('saved channel shows the catalog example, providers and fetch time', async () => {
    mockReservesResponse({ success: true, data: reservesPreview })
    renderReserveForm()
    const row = reserveRow('openai/gpt-5-mini')
    expect(await row.findByText('$0.0571')).toBeVisible()
    expect(row.getByText('catalog')).toBeVisible()
    expect(screen.getByText('Catalog is stale')).toBeVisible()
    expect(
      screen.getByText(
        'Example request: chat/completions, 1000 prompt and 8192 output tokens'
      )
    ).toBeVisible()
    expect(screen.getByText(/^Catalog fetched at /)).toBeVisible()
    fireEvent.click(
      row.getByRole('button', { name: 'Providers for openai/gpt-5-mini' })
    )
    const providers = await screen.findByRole('dialog')
    expect(within(providers).getByText('azure')).toBeVisible()
    expect(within(providers).getByText('flex')).toBeVisible()
    expect(within(providers).getByText('$0.0312')).toBeVisible()
    const refusedProvider = within(providers).getByText('groq').closest('li')
    expect(refusedProvider).toHaveTextContent('Refused')
    expect(refusedProvider).not.toHaveTextContent('$')
  })

  test('row priced from its own reference request names that operation', async () => {
    const [catalogItem, liveItem] = reservesPreview.items
    mockReservesResponse({
      success: true,
      data: {
        ...reservesPreview,
        items: [
          catalogItem,
          {
            ...liveItem,
            effective_source: 'manual',
            reference_operation: 'audio/speech',
            example_usd: 0.1,
            example_status: 'ok',
          },
        ],
      },
    })
    renderReserveForm()
    const row = reserveRow('other/reasoner')
    expect(await row.findByText('$0.10')).toBeVisible()
    expect(row.getByText('Example request: audio/speech')).toBeVisible()
    expect(
      reserveRow('openai/gpt-5-mini').queryByText(/^Example request: /)
    ).not.toBeInTheDocument()
  })

  test('live-only row says it is priced live and shows its known lower bounds', async () => {
    mockReservesResponse({ success: true, data: reservesPreview })
    renderReserveForm()
    const row = reserveRow('other/reasoner')
    expect(await row.findByText('Priced live at request time')).toBeVisible()
    expect(
      row.getByText('At least $0.40 by the configured price')
    ).toBeVisible()
    expect(row.getByText('At least $2.00 by the manual reserve')).toBeVisible()
  })

  test('refused row shows the gateway error and its known lower bounds', async () => {
    const [catalogItem, liveItem] = reservesPreview.items
    mockReservesResponse({
      success: true,
      data: {
        ...reservesPreview,
        items: [
          catalogItem,
          {
            ...liveItem,
            upstream_model: null,
            effective_source: null,
            example_status: 'refused',
            example_error: 'OpenRouter configured price is missing',
          },
        ],
      },
    })
    renderReserveForm()
    const row = reserveRow('other/reasoner')
    expect(
      await row.findByText('OpenRouter configured price is missing')
    ).toBeVisible()
    expect(row.getByText('At least $2.00 by the manual reserve')).toBeVisible()
    expect(
      row.queryByText('Priced live at request time')
    ).not.toBeInTheDocument()
  })

  test('failed example request shows an error state and keeps the rows editable', async () => {
    mockReservesResponse({
      success: false,
      message: 'channel is not OpenRouter',
    })
    renderReserveForm()
    expect(
      await screen.findByText('Failed to load reserve examples')
    ).toBeVisible()
    expect(screen.getByText('channel is not OpenRouter')).toBeVisible()
    expect(screen.getByRole('button', { name: 'Retry' })).toBeVisible()
    expect(manualInput('other/reasoner')).toBeEnabled()
  })

  test('new channel asks to save before showing examples', () => {
    const get = mockReservesResponse({ success: true, data: reservesPreview })
    renderReserveForm({ channelId: null })
    expect(screen.getAllByText('Save to update the example')).toHaveLength(2)
    expect(get).not.toHaveBeenCalled()
  })

  test('unsaved edits replace the saved example until the channel is saved', async () => {
    mockReservesResponse({ success: true, data: reservesPreview })
    renderReserveForm()
    const row = reserveRow('openai/gpt-5-mini')
    expect(await row.findByText('$0.0571')).toBeVisible()
    fireEvent.change(manualInput('openai/gpt-5-mini'), {
      target: { value: '0.1' },
    })
    expect(row.queryByText('$0.0571')).not.toBeInTheDocument()
    expect(row.getByText('Save to update the example')).toBeVisible()
  })

  test('channel without upstream models or settings shows an empty state', () => {
    mockReservesResponse({ success: true, data: reservesPreview })
    render(
      <QueryClientProvider client={new QueryClient()}>
        <EmptyReserveForm />
      </QueryClientProvider>
    )
    expect(screen.getByText('No upstream models')).toBeInTheDocument()
  })

  test('model filter keeps only matching rows and names an empty match', () => {
    mockReservesResponse({ success: true, data: reservesPreview })
    renderReserveForm()
    const filter = screen.getByRole('searchbox', { name: 'Filter models...' })

    fireEvent.change(filter, { target: { value: 'REASONER' } })
    expect(
      screen
        .getAllByRole('combobox', { name: /^Reservation source for / })
        .map((select) => select.getAttribute('aria-label'))
    ).toEqual(['Reservation source for other/reasoner'])

    fireEvent.change(filter, { target: { value: 'missing' } })
    expect(screen.getByText('No models matched your search.')).toBeVisible()
  })

  test('external form reset rebuilds the rows', () => {
    mockReservesResponse({ success: true, data: reservesPreview })
    renderReserveForm({ reserves: '{"legacy/model":0.5}' })
    fireEvent.click(
      screen.getByRole('button', { name: 'Load another channel' })
    )
    expect(manualInput('openai/gpt-5-mini')).toHaveValue(0.1)
    expect(
      screen.queryByRole('spinbutton', {
        name: 'Manual reserve, USD for legacy/model',
      })
    ).not.toBeInTheDocument()
  })

  test('disables editing when sensitive settings are locked or submission is pending', () => {
    mockReservesResponse({ success: true, data: reservesPreview })
    renderReserveForm({ disabled: true, reserves: '{"legacy/model":0.5}' })
    expect(manualInput('other/reasoner')).toBeDisabled()
    expect(
      screen.getByRole('combobox', {
        name: 'Reservation source for other/reasoner',
      })
    ).toBeDisabled()
    expect(
      screen.getByRole('button', { name: 'Remove legacy/model' })
    ).toBeDisabled()
  })
})

function EmptyReserveForm() {
  const form = useForm<ChannelFormValues>({
    defaultValues: { ...CHANNEL_FORM_DEFAULT_VALUES, type: 20 },
  })
  return (
    <Form {...form}>
      <OpenRouterReserveField
        control={form.control}
        disabled={false}
        channelId={7}
        upstreamModels={[]}
      />
    </Form>
  )
}

describe('channel drawer reserve settings', () => {
  test.each([20, 28, 1])(
    'shows the reserve table only for OpenRouter type 20 (channel type %s)',
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
        if (url === '/api/channel/69/openrouter_reserves') {
          return {
            data: { success: true, data: { ...reservesPreview, items: [] } },
          }
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
      const table = screen.queryByRole('group', {
        name: 'OpenRouter reserves',
      })
      if (type === 20) {
        expect(table).toBeVisible()
        expect(
          within(table as HTMLElement).getByRole('spinbutton', {
            name: 'Manual reserve, USD for qwen/qwen3-reranker-8b',
          })
        ).toHaveValue(0.02)
        expect(get).toHaveBeenCalledWith('/api/channel/69/openrouter_reserves')
      } else {
        expect(table).not.toBeInTheDocument()
        expect(get).not.toHaveBeenCalledWith(
          '/api/channel/69/openrouter_reserves'
        )
      }
    }
  )
})
