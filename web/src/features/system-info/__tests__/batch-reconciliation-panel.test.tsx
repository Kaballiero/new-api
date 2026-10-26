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
import { QueryClientProvider, type QueryClient } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {
  AxiosError,
  type AxiosAdapter,
  type InternalAxiosRequestConfig,
} from 'axios'
import { toast } from 'sonner'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/http-client'
import { createAppQueryClient } from '@/lib/query-client'

import { BatchReconciliationPanel } from '../components/batch-reconciliation-panel'
import type { BatchReconciliationJob } from '../types'

const originalAdapter = api.defaults.adapter
let client: QueryClient | undefined

afterEach(() => {
  client?.clear()
  api.defaults.adapter = originalAdapter
  vi.restoreAllMocks()
})

function job(
  overrides: Partial<BatchReconciliationJob>
): BatchReconciliationJob {
  return {
    id: 'batch_unknown',
    user_id: 7,
    token_id: 8,
    channel_id: 9,
    model: 'openai/gpt-4.1-nano',
    upstream_model: 'openai/gpt-4.1-nano',
    status: 'validating',
    submit_state: 'submission_unknown',
    billing_status: 'reconciliation_required',
    purchase_status: '',
    upstream_id: '',
    request_count: 1,
    result_count: 0,
    settled_count: 0,
    reserved_quota: 100,
    charged_quota: 0,
    created_at: Math.floor(Date.now() / 1000) - 3600,
    updated_at: Math.floor(Date.now() / 1000),
    diagnostic: { code: 'transport_error', message: 'm' },
    reason: 'submission_unknown',
    can_release: true,
    ...overrides,
  }
}

type ReleaseReply = { status: number; data: unknown }

function serve(lists: BatchReconciliationJob[][], release?: ReleaseReply) {
  let listCalls = 0
  const releases: unknown[] = []
  const reply = (
    config: InternalAxiosRequestConfig,
    status: number,
    data: unknown
  ) => {
    const response = { data, status, statusText: '', headers: {}, config }
    if (status >= 400) {
      throw new AxiosError(
        'Request failed',
        'ERR_BAD_REQUEST',
        config,
        undefined,
        response
      )
    }
    return response
  }
  const adapter: AxiosAdapter = async (config) => {
    if (
      config.method === 'get' &&
      config.url === '/api/batches/reconciliation'
    ) {
      const data = lists[Math.min(listCalls, lists.length - 1)]
      listCalls++
      return reply(config, 200, { success: true, data, has_more: false })
    }
    if (
      config.method === 'post' &&
      config.url === '/api/batches/batch_unknown/release-unsubmitted' &&
      release
    ) {
      releases.push(JSON.parse(String(config.data)))
      return reply(config, release.status, release.data)
    }
    throw new Error(`Unexpected request: ${config.method} ${config.url}`)
  }
  api.defaults.adapter = adapter
  client = createAppQueryClient()
  render(
    <QueryClientProvider client={client}>
      <BatchReconciliationPanel />
    </QueryClientProvider>
  )
  return { releases, listCalls: () => listCalls }
}

it('disables the release action when the Batch cannot be released', async () => {
  serve([
    [
      job({
        id: 'batch_submitted',
        can_release: false,
        reason: 'provider_window_exceeded',
      }),
    ],
  ])

  const row = (await screen.findByText('batch_submitted')).closest('tr')
  expect(row).not.toBeNull()
  expect(
    within(row as HTMLElement).getByRole('button', { name: 'Release hold' })
  ).toBeDisabled()
})

it('keeps confirmation disabled until evidence is entered', async () => {
  const user = userEvent.setup()
  const server = serve([[job({})]], { status: 200, data: { success: true } })

  await user.click(await screen.findByRole('button', { name: 'Release hold' }))
  const dialog = await screen.findByRole('alertdialog')
  const confirm = within(dialog).getByRole('button', { name: 'Release hold' })
  expect(confirm).toBeDisabled()
  await user.type(within(dialog).getByLabelText('Provider evidence'), '   ')
  expect(confirm).toBeDisabled()
  expect(server.releases).toEqual([])
})

it('sends trimmed evidence and refetches the queue after a successful release', async () => {
  const user = userEvent.setup()
  const notify = vi.spyOn(toast, 'success').mockReturnValue('released')
  const server = serve([[job({})], []], {
    status: 200,
    data: { success: true, id: 'batch_unknown', released_quota: 100 },
  })

  await user.click(await screen.findByRole('button', { name: 'Release hold' }))
  const dialog = await screen.findByRole('alertdialog')
  await user.type(
    within(dialog).getByLabelText('Provider evidence'),
    '  Провайдер: Batch отсутствует  '
  )
  await user.click(within(dialog).getByRole('button', { name: 'Release hold' }))

  await waitFor(() => expect(server.listCalls()).toBe(2))
  expect(server.releases).toEqual([
    { evidence: 'Провайдер: Batch отсутствует' },
  ])
  expect(notify).toHaveBeenCalledTimes(1)
  expect(
    await screen.findByText('No Batches need reconciliation.')
  ).toBeInTheDocument()
  expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
})

it('shows the server conflict once and keeps the dialog open when release is ineligible', async () => {
  const user = userEvent.setup()
  const notify = vi.spyOn(toast, 'error').mockReturnValue('conflict')
  const message =
    'only an unknown creation without provider identity, results or charges can be released'
  const server = serve([[job({})]], {
    status: 409,
    data: {
      error: { type: 'batch_error', code: 'release_ineligible', message },
    },
  })

  await user.click(await screen.findByRole('button', { name: 'Release hold' }))
  const dialog = await screen.findByRole('alertdialog')
  await user.type(within(dialog).getByLabelText('Provider evidence'), 'checked')
  await user.click(within(dialog).getByRole('button', { name: 'Release hold' }))

  await waitFor(() =>
    expect(notify.mock.calls.map(([text]) => text)).toEqual([message])
  )
  await waitFor(() => expect(server.listCalls()).toBe(2))
  expect(screen.getByRole('alertdialog')).toBeInTheDocument()
})
