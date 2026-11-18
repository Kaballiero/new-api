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
import type { CellContext, ColumnDef } from '@tanstack/react-table'
import { render, renderHook, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test, vi } from 'vitest'

import type { OpenRouterSyncAdminView, TaskLog } from '../../types'
import { useTaskLogsColumns } from '../columns/task-logs-columns'
import { OpenRouterSyncTaskFilters } from '../openrouter-sync-task-filters'

const task: TaskLog = {
  id: 1,
  user_id: 7,
  platform: 'openrouter-chat-image',
  task_id: 'task_hold',
  action: 'chat',
  channel_id: 3,
  group: 'default',
  quota: 50,
  submit_time: 1,
  status: 'FAILURE',
}

function renderDetailsCell(log: TaskLog) {
  const { result } = renderHook(() => useTaskLogsColumns(true, false))
  const details = result.current.find(
    (column) => 'accessorKey' in column && column.accessorKey === 'fail_reason'
  ) as ColumnDef<TaskLog> & {
    cell: (context: CellContext<TaskLog, unknown>) => React.ReactNode
  }
  return render(
    <>
      {details.cell({ row: { original: log } } as CellContext<
        TaskLog,
        unknown
      >)}
    </>
  )
}

function withSyncView(view: Partial<OpenRouterSyncAdminView>): TaskLog {
  return {
    ...task,
    admin_info: {
      openrouter_sync: view as OpenRouterSyncAdminView,
    },
  }
}

describe('OpenRouter manual-hold queue', () => {
  test('manual hold row shows the needs decision badge', () => {
    renderDetailsCell(
      withSyncView({
        billing_state: 'manual_reconciliation',
        manual_hold: true,
      })
    )

    expect(screen.getByText('Needs decision')).toBeVisible()
  })

  test('manual hold with an unknown reservation shows the needs decision badge', () => {
    renderDetailsCell(
      withSyncView({ billing_state: 'reservation_unknown', manual_hold: true })
    )

    expect(screen.getByText('Needs decision')).toBeVisible()
  })

  test.each([
    ['audio', 'Audio'],
    ['rerank', 'Rerank'],
    ['chat-image', 'Chat image'],
  ])('task with action %j names the action %s', (action, label) => {
    const { result } = renderHook(() => useTaskLogsColumns(true, false))
    const taskId = result.current.find(
      (column) => 'accessorKey' in column && column.accessorKey === 'task_id'
    ) as ColumnDef<TaskLog> & {
      cell: (context: CellContext<TaskLog, unknown>) => React.ReactNode
    }
    const log = { ...task, platform: 'openrouter-audio', action }
    render(
      <>
        {taskId.cell({
          row: { original: log, getValue: () => log.task_id },
        } as unknown as CellContext<TaskLog, unknown>)}
      </>
    )

    expect(screen.getByText(`openrouter-audio · ${label}`)).toBeVisible()
  })

  test('settled row and row without admin info show no badge', () => {
    renderDetailsCell(
      withSyncView({ billing_state: 'settled', manual_hold: false })
    )
    renderDetailsCell(task)

    expect(screen.queryByText('Needs decision')).not.toBeInTheDocument()
  })

  test.each([
    ['', 'Needs decision', 'manual_reconciliation'],
    ['awaiting_cost', 'All billing states', ''],
  ])(
    'with state %j choosing %s reports %j',
    async (current, option, reported) => {
      const onBillingStateChange = vi.fn()
      const user = userEvent.setup()
      render(
        <OpenRouterSyncTaskFilters
          billingState={current}
          onBillingStateChange={onBillingStateChange}
          onHoldReasonChange={vi.fn()}
          onKeyDown={vi.fn()}
        />
      )

      await user.click(
        screen.getByRole('combobox', { name: 'OpenRouter billing state' })
      )
      await user.click(screen.getByRole('option', { name: option }))

      expect(onBillingStateChange).toHaveBeenCalledWith(reported)
    }
  )
})
