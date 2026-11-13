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
import { describe, expect, test } from 'vitest'

import { channelSchema } from '../../types'
import {
  transformChannelToFormDefaults,
  transformFormDataToUpdatePayload,
} from '../channel-form'

function openRouterForm() {
  return transformChannelToFormDefaults(
    channelSchema.parse({
      id: 70,
      type: 20,
      key: '',
      status: 1,
      name: 'OpenRouter',
      created_time: 0,
      test_time: 0,
      response_time: 0,
      balance_updated_time: 0,
      models: 'openai/gpt-5-mini',
      settings: JSON.stringify({
        upstream_model_update_check_enabled: true,
        upstream_model_update_catalog_sync_enabled: true,
      }),
    })
  )
}

function savedSettings(form: ReturnType<typeof openRouterForm>) {
  return JSON.parse(transformFormDataToUpdatePayload(form, 70).settings || '')
}

describe('upstream catalog sync channel setting', () => {
  test('saved OpenRouter catalog sync is read and saved unchanged', () => {
    const form = openRouterForm()
    expect(form.upstream_model_update_catalog_sync_enabled).toBe(true)
    expect(savedSettings(form).upstream_model_update_catalog_sync_enabled).toBe(
      true
    )
  })

  test('turning off upstream checks saves catalog sync as off', () => {
    const form = openRouterForm()
    form.upstream_model_update_check_enabled = false
    expect(savedSettings(form).upstream_model_update_catalog_sync_enabled).toBe(
      false
    )
  })

  test('changing the channel type away from OpenRouter drops catalog sync', () => {
    const form = openRouterForm()
    form.type = 1
    expect(savedSettings(form)).not.toHaveProperty(
      'upstream_model_update_catalog_sync_enabled'
    )
  })
})
