/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
  transformFormDataToUpdatePayload,
} from '../channel-form'

const requestPathMapping = JSON.stringify({
  channel: {
    '/v1/responses': '/v1/chat/completions',
  },
  models: {
    'gpt-4o': {
      '/v1/responses': '/v1/chat/completions',
    },
  },
})

describe('request path mapping form field', () => {
  test('loads and preserves channel and model mappings', () => {
    const defaults = transformChannelToFormDefaults({
      request_path_mapping: requestPathMapping,
      channel_info: { multi_key_mode: 'random' },
    } as never)

    assert.equal(defaults.request_path_mapping, requestPathMapping)
  })

  test('validates the mapping as a JSON object of path maps', () => {
    const valid = channelFormSchema.safeParse({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'Mapped channel',
      models: 'gpt-4o',
      request_path_mapping: requestPathMapping,
    })
    assert.equal(valid.success, true)

    const invalid = channelFormSchema.safeParse({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'Mapped channel',
      models: 'gpt-4o',
      request_path_mapping: '{"channel":{"responses":"/v1/chat"}}',
    })
    assert.equal(invalid.success, false)
  })

  test('includes the field in create and update payloads', () => {
    const formData = {
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'Mapped channel',
      models: 'gpt-4o',
      request_path_mapping: requestPathMapping,
    }

    assert.equal(
      transformFormDataToCreatePayload(formData).channel.request_path_mapping,
      requestPathMapping
    )
    assert.equal(
      transformFormDataToUpdatePayload(formData, 42).request_path_mapping,
      requestPathMapping
    )
  })
})
