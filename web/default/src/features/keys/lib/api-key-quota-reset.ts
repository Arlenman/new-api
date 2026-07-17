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
import { API_KEY_QUOTA_RESET_PERIOD_OPTIONS } from '../constants'
import type {
  ApiKey,
  ApiKeyQuotaResetFormPeriod,
  ApiKeyQuotaResetPeriod,
} from '../types'

type QuotaResetDescriptionInput = {
  period: ApiKeyQuotaResetFormPeriod
  intervalHours: number
  amount: number | string
  currency: string
}

export function getApiKeyQuotaResetDescription({
  period,
  intervalHours,
  amount,
  currency,
}: QuotaResetDescriptionInput) {
  const values = { amount, currency }
  switch (period) {
    case 'daily':
      return {
        key: 'Reset {{amount}} {{currency}} every day at 00:00',
        values,
      } as const
    case 'weekly':
      return {
        key: 'Reset {{amount}} {{currency}} every Monday at 00:00',
        values,
      } as const
    case 'monthly':
      return {
        key: 'Reset {{amount}} {{currency}} on the first day of every month at 00:00',
        values,
      } as const
    case 'custom_hours':
      return {
        key: 'Starting now, reset {{amount}} {{currency}} every {{hours}} hours',
        values: { ...values, hours: intervalHours },
      } as const
  }
}

function getQuotaResetCadence(period: ApiKeyQuotaResetPeriod, hours: number) {
  if (period === 'custom_hours' || period === 'hourly') {
    return {
      cadenceLabel: 'Every {{hours}} hours',
      cadenceValues: { hours: period === 'hourly' ? 1 : Math.max(hours, 1) },
    } as const
  }

  return {
    cadenceLabel: API_KEY_QUOTA_RESET_PERIOD_OPTIONS[period].cadenceLabel,
  } as const
}

export function getApiKeyPeriodicQuotaView(apiKey: ApiKey) {
  if (!apiKey.quota_reset_enabled) {
    return { enabled: false } as const
  }

  const cadence = getQuotaResetCadence(
    apiKey.quota_reset_period,
    apiKey.quota_reset_interval_hours ?? 0
  )

  return {
    enabled: true,
    remaining: apiKey.quota_reset_remaining,
    amount: apiKey.quota_reset_amount,
    nextTime: apiKey.quota_reset_next_time,
    ...cadence,
  } as const
}
