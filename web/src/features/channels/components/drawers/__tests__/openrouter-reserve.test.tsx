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
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useForm } from 'react-hook-form'
import { describe, expect, test } from 'vitest'

import { Form } from '@/components/ui/form'

import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  type ChannelFormValues,
} from '../../../lib/channel-form'
import { OpenRouterReserveField } from '../sections/openrouter-reserve-field'

function ReserveForm(props: { disabled: boolean }) {
  const form = useForm<ChannelFormValues>({
    resolver: zodResolver(channelFormSchema),
    defaultValues: {
      ...CHANNEL_FORM_DEFAULT_VALUES,
      type: 28,
      name: 'OpenRouter',
      key: 'local',
      models: 'other/reasoner',
      openrouter_model_reserve_usd: '{"other/reasoner":2}',
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
  })
})
