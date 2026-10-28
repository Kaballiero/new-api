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
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { releaseOpenRouterSyncHold, settleOpenRouterSyncCost } from '../../api'
import type { OpenRouterSyncAdminView } from '../../types'
import { OpenRouterSyncReconciliation } from '../dialogs/openrouter-sync-reconciliation'

// The network boundary only; the component, form and schema run for real.
vi.mock('../../api', () => ({
  releaseOpenRouterSyncHold: vi.fn(),
  settleOpenRouterSyncCost: vi.fn(),
}))

const manualHold: OpenRouterSyncAdminView = {
  submission_state: 'identity_confirmed',
  billing_state: 'manual_reconciliation',
  reason_code: 'cost_lookup_deadline_or_attempt_limit',
  replay_allowed: false,
  identity_available: true,
  cost_confirmed: false,
  next_lookup_at: null,
  result_ready: false,
  result_failed: true,
  reserved_quota: 50,
  target_quota: 0,
  result_reason: '',
  last_lookup_class: 'temporary_http_error',
  last_http_status: 503,
  lookup_attempts: 16,
  identity_conflict: false,
  operator_decision: null,
  release_allowed: true,
  settle_allowed: true,
}

const success = {
  success: true,
  data: {
    task_id: 'task_hold',
    action: 'settle' as const,
    billing_state: 'settled',
    reserved_quota: 50,
    target_quota: 80,
    already_applied: false,
  },
}

let queryClient: QueryClient

function renderReconciliation(view: OpenRouterSyncAdminView, isRoot: boolean) {
  return render(
    <QueryClientProvider client={queryClient}>
      <OpenRouterSyncReconciliation
        taskId='task_hold'
        view={view}
        isRoot={isRoot}
      />
    </QueryClientProvider>
  )
}

function openDecision(name: string) {
  fireEvent.click(screen.getByRole('button', { name }))
  const dialog = screen.getByRole('alertdialog')
  return {
    dialog,
    confirm: within(dialog).getByRole('button', { name }),
  }
}

describe('OpenRouter sync reconciliation actions', () => {
  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
  })

  afterEach(() => {
    queryClient.clear()
  })

  test('admin without root sees diagnostics but no operator actions', () => {
    renderReconciliation(manualHold, false)

    expect(screen.getByText('temporary_http_error / HTTP 503')).toBeVisible()
    expect(
      screen.queryByRole('button', { name: 'Release hold' })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Settle with cost' })
    ).not.toBeInTheDocument()
  })

  test('root sees no action the server does not allow', () => {
    renderReconciliation(
      { ...manualHold, release_allowed: false, settle_allowed: false },
      true
    )

    expect(
      screen.queryByRole('button', { name: 'Release hold' })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Settle with cost' })
    ).not.toBeInTheDocument()
  })

  test('settle confirmation stays disabled until cost and evidence are valid', async () => {
    renderReconciliation({ ...manualHold, release_allowed: false }, true)
    expect(
      screen.queryByRole('button', { name: 'Release hold' })
    ).not.toBeInTheDocument()
    const { dialog, confirm } = openDecision('Settle with cost')
    const cost = within(dialog).getByLabelText('Upstream cost (USD)')
    const evidence = within(dialog).getByLabelText('Evidence')
    expect(confirm).toBeDisabled()

    fireEvent.change(cost, { target: { value: '0.08' } })
    fireEvent.change(evidence, { target: { value: '   ' } })
    await waitFor(() => expect(confirm).toBeDisabled())

    fireEvent.change(evidence, { target: { value: 'OpenRouter activity' } })
    await waitFor(() => expect(confirm).toBeEnabled())

    for (const invalid of ['50.01', '0', '0.0000000000001', '1e-3']) {
      fireEvent.change(cost, { target: { value: invalid } })
      await waitFor(() => expect(confirm).toBeDisabled())
    }
    expect(settleOpenRouterSyncCost).not.toHaveBeenCalled()
  })

  test('valid settle submits the trimmed cost and evidence', async () => {
    vi.mocked(settleOpenRouterSyncCost).mockResolvedValue(success)
    renderReconciliation(manualHold, true)
    const { dialog, confirm } = openDecision('Settle with cost')

    fireEvent.change(within(dialog).getByLabelText('Upstream cost (USD)'), {
      target: { value: '50' },
    })
    fireEvent.change(within(dialog).getByLabelText('Evidence'), {
      target: { value: ' OpenRouter activity ' },
    })
    await waitFor(() => expect(confirm).toBeEnabled())
    fireEvent.click(confirm)

    await waitFor(() =>
      expect(settleOpenRouterSyncCost).toHaveBeenCalledWith(
        'task_hold',
        '50',
        'OpenRouter activity'
      )
    )
    expect(releaseOpenRouterSyncHold).not.toHaveBeenCalled()
  })

  test('release requires evidence and submits it without a cost', async () => {
    vi.mocked(releaseOpenRouterSyncHold).mockResolvedValue({
      ...success,
      data: { ...success.data, action: 'release', target_quota: 0 },
    })
    renderReconciliation(manualHold, true)
    const { dialog, confirm } = openDecision('Release hold')

    expect(
      within(dialog).queryByLabelText('Upstream cost (USD)')
    ).not.toBeInTheDocument()
    expect(confirm).toBeDisabled()
    fireEvent.change(within(dialog).getByLabelText('Evidence'), {
      target: { value: 'No generation in OpenRouter activity' },
    })
    await waitFor(() => expect(confirm).toBeEnabled())
    fireEvent.click(confirm)

    await waitFor(() =>
      expect(releaseOpenRouterSyncHold).toHaveBeenCalledWith(
        'task_hold',
        'No generation in OpenRouter activity'
      )
    )
    expect(settleOpenRouterSyncCost).not.toHaveBeenCalled()
  })
})
