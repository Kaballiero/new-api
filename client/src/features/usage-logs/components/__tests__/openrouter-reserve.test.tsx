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
import { render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, test } from 'vitest'

import type { UsageLog } from '../../data/schema'
import type { LogOtherData, TaskLog } from '../../types'
import { DetailsDialog } from '../dialogs/details-dialog'
import { TaskDetailsDialog } from '../dialogs/task-details-dialog'

const queryClients: QueryClient[] = []

function makeLog(other: LogOtherData): UsageLog {
  return {
    id: 1,
    user_id: 1,
    created_at: 1,
    type: 2,
    content: 'OpenRouter request',
    username: 'user',
    token_name: 'token',
    model_name: 'openai/gpt-5-mini',
    quota: 10,
    prompt_tokens: 1,
    completion_tokens: 1,
    use_time: 1,
    is_stream: false,
    channel: 1,
    channel_name: 'channel',
    token_id: 1,
    group: 'default',
    ip: '',
    other: JSON.stringify(other),
    request_id: 'req-1',
    upstream_request_id: '',
  }
}

function newQueryClient(): QueryClient {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  queryClient.setQueryData(['status'], {}, { updatedAt: Date.now() + 60_000 })
  queryClients.push(queryClient)
  return queryClient
}

function renderUsageLog(other: LogOtherData, isAdmin: boolean): void {
  render(
    <QueryClientProvider client={newQueryClient()}>
      <DetailsDialog
        log={makeLog(other)}
        isAdmin={isAdmin}
        isRoot={false}
        open
        onOpenChange={() => undefined}
      />
    </QueryClientProvider>
  )
}

function reserveSection(): HTMLElement {
  const label = screen.getByText('OpenRouter reserve')
  return label.parentElement as HTMLElement
}

afterEach(() => {
  for (const queryClient of queryClients) {
    queryClient.clear()
  }
  queryClients.length = 0
})

describe('OpenRouter reserve in log details', () => {
  test('shows a stale catalog source as a warning with the admission reserve to admins', () => {
    renderUsageLog(
      {
        admin_info: {
          openrouter_reservation_source: 'catalog_stale',
          openrouter_admission_policy: 'model_approximate_reserve',
          openrouter_approximate_reserve_usd: 0.25,
          openrouter_approximate_reserve_model: 'openai/gpt-5-mini',
        },
      },
      true
    )

    const section = within(reserveSection())
    const badge = section.getByText('Stale catalog')
    expect(badge.closest('[data-slot="status-badge"]')).toHaveClass(
      'text-warning'
    )
    expect(section.getByText('model_approximate_reserve')).toBeVisible()
    expect(section.getByText('0.25 USD · openai/gpt-5-mini')).toBeVisible()
  })

  test('shows a configured price reserve without a warning', () => {
    renderUsageLog(
      {
        admin_info: {
          openrouter_reservation_source: 'configured_price',
          openrouter_configured_reserve_usd: 1.2,
          openrouter_configured_reserve_model: 'openai/gpt-5',
          openrouter_configured_reserve_expr_hash: 'abc123',
        },
      },
      true
    )

    const section = within(reserveSection())
    expect(
      section
        .getByText('Configured price')
        .closest('[data-slot="status-badge"]')
    ).not.toHaveClass('text-warning')
    expect(section.getByText('1.2 USD · openai/gpt-5')).toBeVisible()
    expect(section.getByText('abc123')).toBeVisible()
  })

  test('hides the reserve from non-admin users', () => {
    renderUsageLog(
      { admin_info: { openrouter_reservation_source: 'manual' } },
      false
    )

    expect(screen.queryByText('OpenRouter reserve')).toBeNull()
  })

  test('renders no reserve section for a log without reserve fields', () => {
    renderUsageLog({ admin_info: { reject_reason: 'blocked' } }, true)

    expect(screen.queryByText('OpenRouter reserve')).toBeNull()
  })

  test('shows the task order reserve of a media task log', () => {
    renderUsageLog(
      {
        admin_info: {
          openrouter_reservation_source: 'manual',
          openrouter_order_facts: {
            reservation_source: 'manual',
            reservation: {
              source: 'approximate_media',
              model: 'google/veo-3.1',
              usd: 1.5,
              hard_bound: false,
            },
          },
        },
      },
      true
    )

    const section = within(reserveSection())
    expect(
      section.getByText('Manual reserve').closest('[data-slot="status-badge"]')
    ).toHaveClass('text-warning')
    expect(
      section.getByText('1.5 USD · google/veo-3.1 · approximate_media')
    ).toBeVisible()
  })
})

describe('OpenRouter reserve in task details', () => {
  function makeTask(adminInfo: TaskLog['admin_info']): TaskLog {
    return {
      id: 1,
      user_id: 1,
      platform: 'openrouter-video',
      task_id: 'task_video',
      action: 'textGenerate',
      channel_id: 1,
      group: 'default',
      quota: 10,
      submit_time: 1,
      status: 'SUCCESS',
      admin_info: adminInfo,
    }
  }

  function renderTask(isAdmin: boolean): void {
    render(
      <QueryClientProvider client={newQueryClient()}>
        <TaskDetailsDialog
          log={makeTask({
            openrouter_order_facts: {
              reservation_source: 'catalog',
              reservation: {
                source: 'catalog',
                model: 'openai/gpt-image-1',
                usd: 0.04,
              },
            },
          })}
          isAdmin={isAdmin}
          isRoot={false}
          open
          onOpenChange={() => undefined}
        />
      </QueryClientProvider>
    )
  }

  test('shows the order reserve and its source to admins', () => {
    renderTask(true)

    const section = within(reserveSection())
    expect(section.getByText('Catalog')).toBeVisible()
    expect(
      section.getByText('0.04 USD · openai/gpt-image-1 · catalog')
    ).toBeVisible()
  })

  test('hides the order reserve from non-admin users', () => {
    renderTask(false)

    expect(screen.queryByText('OpenRouter reserve')).toBeNull()
  })
})
