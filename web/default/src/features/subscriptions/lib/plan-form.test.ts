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
import assert from 'node:assert/strict'
import { after, before, describe, test } from 'node:test'

import { createServer, type ViteDevServer } from 'vite'

import type { SubscriptionPlan } from '../types.ts'
import type { PlanFormValues } from './plan-form.ts'

interface PlanFormModule {
  PLAN_FORM_DEFAULTS: PlanFormValues
  formValuesToPlanPayload: (values: PlanFormValues) => {
    plan: Partial<SubscriptionPlan>
  }
  planToFormValues: (plan: SubscriptionPlan) => PlanFormValues
}

let server: ViteDevServer
let planForm: PlanFormModule

before(async () => {
  server = await createServer({
    configFile: false,
    root: process.cwd(),
    resolve: { alias: { '@': `${process.cwd()}/src` } },
    server: { middlewareMode: true, hmr: false },
    appType: 'custom',
    logLevel: 'silent',
  })
  planForm = (await server.ssrLoadModule(
    '/src/features/subscriptions/lib/plan-form.ts'
  )) as unknown as PlanFormModule
})

after(async () => {
  await server.close()
})

describe('subscription plan user group form mapping', () => {
  test('defaults new plans to all groups', () => {
    assert.equal(planForm.PLAN_FORM_DEFAULTS.user_group, '')
  })

  test('preserves the configured user group through form mapping', () => {
    const plan = {
      ...planForm.PLAN_FORM_DEFAULTS,
      id: 1,
      currency: 'USD',
      user_group: 'vip',
    } as SubscriptionPlan & { user_group: string }

    const values = planForm.planToFormValues(plan)
    assert.equal(values.user_group, 'vip')

    const payload = planForm.formValuesToPlanPayload(values)
    assert.equal(payload.plan.user_group, 'vip')
  })
})
