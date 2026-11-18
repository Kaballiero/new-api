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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'

import { TooltipProvider } from '@/components/ui/tooltip'

import { ChannelsDialogs } from '../../channels-dialogs'
import { ChannelsProvider, useChannels } from '../../channels-provider'
import { UpstreamUpdateDialog } from '../upstream-update-dialog'

// jsdom has no Web Animations API, which the scroll area calls.
const originalGetAnimations = Object.getOwnPropertyDescriptor(
  HTMLElement.prototype,
  'getAnimations'
)

beforeAll(() => {
  Object.defineProperty(HTMLElement.prototype, 'getAnimations', {
    configurable: true,
    value: () => [],
  })
})

afterAll(() => {
  if (originalGetAnimations) {
    Object.defineProperty(
      HTMLElement.prototype,
      'getAnimations',
      originalGetAnimations
    )
    return
  }
  Reflect.deleteProperty(HTMLElement.prototype, 'getAnimations')
})

describe('upstream update dialog needs-decision section', () => {
  it('lists conflicting models read-only and keeps confirm disabled when nothing is pending', async () => {
    const user = userEvent.setup()
    render(
      <UpstreamUpdateDialog
        open
        addModels={[]}
        removeModels={[]}
        conflictModels={['mongodb/voyage-4', 'acme/gpt-4o']}
        reserveRefusedModels={[]}
        preferredTab='add'
        confirmLoading={false}
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
      />
    )

    await user.click(screen.getByRole('tab', { name: /Needs decision/ }))

    const conflictList = screen.getByRole('list')
    expect(conflictList).toHaveTextContent('mongodb/voyage-4')
    expect(conflictList).toHaveTextContent('acme/gpt-4o')
    expect(screen.queryByRole('checkbox')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Confirm' })).toBeDisabled()
  })

  it('lists additions with a refused reserve under needs decision and counts them', async () => {
    const user = userEvent.setup()
    render(
      <UpstreamUpdateDialog
        open
        addModels={['acme/chat', 'acme/unreservable']}
        removeModels={[]}
        conflictModels={[]}
        reserveRefusedModels={['acme/unreservable']}
        preferredTab='add'
        confirmLoading={false}
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
      />
    )

    const needsDecision = screen.getByRole('tab', { name: /Needs decision/ })
    expect(needsDecision).toHaveTextContent('1')
    await user.click(needsDecision)

    expect(screen.getByRole('list')).toHaveTextContent('acme/unreservable')
    expect(
      screen.queryByText('Nothing needs a decision')
    ).not.toBeInTheDocument()
  })

  it('shows the empty state when no conflict and no refused reserve need a decision', async () => {
    const user = userEvent.setup()
    render(
      <UpstreamUpdateDialog
        open
        addModels={['acme/chat']}
        removeModels={[]}
        conflictModels={[]}
        reserveRefusedModels={[]}
        preferredTab='add'
        confirmLoading={false}
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
      />
    )

    await user.click(screen.getByRole('tab', { name: /Needs decision/ }))

    expect(screen.getByText('Nothing needs a decision')).toBeInTheDocument()
    expect(screen.queryByRole('list')).not.toBeInTheDocument()
  })
})

function UpstreamUpdateOpeners() {
  const { upstream } = useChannels()
  return (
    <>
      <button
        type='button'
        onClick={() =>
          upstream.openModal({ id: 1 }, ['model-a', 'model-b'], [], [], 'add')
        }
      >
        Open channel 1
      </button>
      <button
        type='button'
        onClick={() =>
          upstream.openModal(
            { id: 2 },
            ['model-c'],
            [],
            ['acme/gpt-4o'],
            'needs-decision'
          )
        }
      >
        Open channel 2
      </button>
    </>
  )
}

describe('upstream update dialog reopening', () => {
  it('starts each opening from its own models and preferred tab', async () => {
    const user = userEvent.setup()
    render(
      <QueryClientProvider client={new QueryClient()}>
        <TooltipProvider>
          <ChannelsProvider>
            <UpstreamUpdateOpeners />
            <ChannelsDialogs />
          </ChannelsProvider>
        </TooltipProvider>
      </QueryClientProvider>
    )

    await user.click(screen.getByRole('button', { name: 'Open channel 1' }))
    expect(screen.getByRole('checkbox', { name: 'model-a' })).toBeChecked()
    expect(screen.getByRole('checkbox', { name: 'model-b' })).toBeChecked()
    await user.click(screen.getByRole('checkbox', { name: 'model-a' }))
    await user.click(screen.getByRole('button', { name: 'Cancel' }))

    await user.click(screen.getByRole('button', { name: 'Open channel 2' }))
    expect(screen.getByRole('tab', { name: /Needs decision/ })).toHaveAttribute(
      'aria-selected',
      'true'
    )
    expect(screen.getByRole('list')).toHaveTextContent('acme/gpt-4o')
    await user.click(screen.getByRole('tab', { name: /Add Models/ }))
    expect(screen.getByRole('checkbox', { name: 'model-c' })).toBeChecked()
    expect(
      screen.queryByRole('checkbox', { name: 'model-a' })
    ).not.toBeInTheDocument()
  })
})
