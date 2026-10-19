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
  buildSettingJSON,
  channelFormSchema,
  transformChannelToFormDefaults,
} from '../channel-form'
import { hasAdvancedSettingsErrors } from '../channel-form-errors'

function reserveForm(setting: Record<string, unknown>) {
  return transformChannelToFormDefaults(
    channelSchema.parse({
      id: 68,
      type: 20,
      key: 'local-test',
      status: 1,
      name: 'OpenRouter',
      created_time: 0,
      test_time: 0,
      response_time: 0,
      balance_updated_time: 0,
      setting: JSON.stringify(setting),
      models: 'perplexity/sonar-deep-research,other/reasoner',
    })
  )
}

describe('OpenRouter model reserve channel form', () => {
  test('reads and saves reservation sources independently from minimum holds', () => {
    const sources = {
      'other/reasoner': 'configured_price',
      'other/text': 'upstream',
    }
    const form = reserveForm({ openrouter_reservation_source: sources })
    expect(channelFormSchema.safeParse(form).success).toBe(true)
    expect(
      JSON.parse(buildSettingJSON(form)).openrouter_reservation_source
    ).toEqual(sources)
    expect(
      JSON.parse(buildSettingJSON(form)).openrouter_model_reserve_usd
    ).toBeUndefined()
    form.openrouter_reservation_source = '{}'
    expect(
      JSON.parse(buildSettingJSON(form)).openrouter_reservation_source
    ).toBeUndefined()
  })

  test.each([
    'null',
    '[]',
    '{',
    '{"other/*":"configured_price"}',
    '{"other/text":"invalid"}',
    '{"other/text":2}',
  ])('rejects invalid reservation source %s at the advanced field', (value) => {
    const form = reserveForm({})
    form.openrouter_reservation_source = value
    const result = channelFormSchema.safeParse(form)
    expect(result.success).toBe(false)
    if (!result.success) {
      expect(
        result.error.issues.some(
          (issue) => issue.path[0] === 'openrouter_reservation_source'
        )
      ).toBe(true)
    }
    expect(
      hasAdvancedSettingsErrors({
        openrouter_reservation_source: { message: 'invalid' },
      })
    ).toBe(true)
  })
  test('reads and saves exact per-model amounts with mapping unchanged', () => {
    const policy = { 'perplexity/sonar-deep-research': 2, 'other/reasoner': 3 }
    const form = reserveForm({ openrouter_model_reserve_usd: policy })
    form.model_mapping = '{"alias":"other/reasoner"}'
    expect(channelFormSchema.safeParse(form).success).toBe(true)
    expect(
      JSON.parse(buildSettingJSON(form)).openrouter_model_reserve_usd
    ).toEqual(policy)
    expect(form.model_mapping).toBe('{"alias":"other/reasoner"}')
  })

  test('migrates legacy Sonar on save while exact policy takes precedence', () => {
    const legacy = reserveForm({ openrouter_sonar_research_reserve_usd: 2 })
    const saved = JSON.parse(buildSettingJSON(legacy))
    expect(saved.openrouter_model_reserve_usd).toEqual({
      'perplexity/sonar-deep-research': 2,
    })
    expect(saved.openrouter_sonar_research_reserve_usd).toBeUndefined()
    const newer = reserveForm({
      openrouter_sonar_research_reserve_usd: 2,
      openrouter_model_reserve_usd: { 'perplexity/sonar-deep-research': 3 },
    })
    expect(
      JSON.parse(buildSettingJSON(newer)).openrouter_model_reserve_usd
    ).toEqual({ 'perplexity/sonar-deep-research': 3 })
  })

  test.each(['{}', ''])(
    'clearing policy with %j also disables migrated legacy Sonar',
    (value) => {
      const form = reserveForm({ openrouter_sonar_research_reserve_usd: 2 })
      form.openrouter_model_reserve_usd = value
      const saved = JSON.parse(buildSettingJSON(form))
      expect(saved.openrouter_model_reserve_usd).toBeUndefined()
      expect(saved.openrouter_sonar_research_reserve_usd).toBeUndefined()
    }
  )

  test('unchanged channels keep reserve disabled', () => {
    const form = reserveForm({})
    expect(
      JSON.parse(buildSettingJSON(form)).openrouter_model_reserve_usd
    ).toBeUndefined()
  })

  test.each([
    'null',
    '[]',
    '{',
    '{"":2}',
    '{"other/*":2}',
    '{"other/reasoner ":2}',
    '{"other/reasoner":0}',
    '{"other/reasoner":-1}',
    '{"other/reasoner":null}',
    '{"other/reasoner":"2"}',
    '{"other/reasoner":1e999}',
  ])('rejects invalid policy %s at its field', (value) => {
    const form = reserveForm({})
    form.openrouter_model_reserve_usd = value
    const result = channelFormSchema.safeParse(form)
    expect(result.success).toBe(false)
    expect(
      hasAdvancedSettingsErrors({
        openrouter_model_reserve_usd: { message: 'invalid' },
      })
    ).toBe(true)
    if (!result.success) {
      expect(
        result.error.issues.some(
          (issue) => issue.path[0] === 'openrouter_model_reserve_usd'
        )
      ).toBe(true)
    }
  })
})
