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
import { describe, expect, test, vi } from 'vitest'

import { getAllTaskLogs, getUserTaskLogs } from '../api'
import { buildSearchParams } from '../lib/filter'
import { fetchLogsByCategory } from '../lib/utils'

// The network boundary only; parameter building runs for real.
vi.mock('../api', () => ({
  getAllLogs: vi.fn(),
  getUserLogs: vi.fn(),
  getAllMidjourneyLogs: vi.fn(),
  getUserMidjourneyLogs: vi.fn(),
  getAllTaskLogs: vi.fn(),
  getUserTaskLogs: vi.fn(),
}))

const syncSearch = {
  syncBillingState: 'manual_reconciliation',
  syncHoldReason: 'auth',
}

describe('OpenRouter sync task filters', () => {
  test('task filters keep the billing state and hold reason in the URL search', () => {
    expect(
      buildSearchParams({ taskId: 'task_1', ...syncSearch }, 'task')
    ).toEqual({ filter: 'task_1', ...syncSearch })
  })

  test('admin task list sends the billing state and hold reason to the server', async () => {
    await fetchLogsByCategory({
      logCategory: 'task',
      isAdmin: true,
      page: 1,
      pageSize: 20,
      searchParams: syncSearch,
      columnFilters: [],
    })

    expect(getAllTaskLogs).toHaveBeenCalledWith(
      expect.objectContaining({
        sync_billing_state: 'manual_reconciliation',
        sync_hold_reason: 'auth',
      })
    )
  })

  test('own task list never sends the admin-only sync filters', async () => {
    await fetchLogsByCategory({
      logCategory: 'task',
      isAdmin: false,
      page: 1,
      pageSize: 20,
      searchParams: syncSearch,
      columnFilters: [],
    })

    const params = vi.mocked(getUserTaskLogs).mock.calls[0][0]
    expect(params).not.toHaveProperty('sync_billing_state')
    expect(params).not.toHaveProperty('sync_hold_reason')
  })
})
