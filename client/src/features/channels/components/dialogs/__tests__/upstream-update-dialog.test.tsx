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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'

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
})
