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
import { MESSAGE_STATUS } from '../../constants.ts'
import type { Message } from '../../types.ts'
import {
  isLegacyImageGeneration524ErrorMessage,
  isRecoverableImageGenerationErrorMessage,
} from './image-generation-error-utils.ts'
import { getMessageContent } from './message-utils.ts'

export const MODEL_PRICING_SETTINGS_PATH =
  '/system-settings/billing/model-pricing'

const MODEL_PRICE_ERROR_CODE = 'model_price_error'
export const FALLBACK_ERROR_CONTENT = 'An unknown error occurred'
export const RESPONSE_NOT_COMPLETED_TITLE = 'Response not completed'
export const RETRYABLE_RESPONSE_ERROR_CONTENT =
  'Response did not finish. You can retry.'

const REQUEST_ERROR_DETAIL_PATTERNS = [
  /Request error occurred/i,
  /Request failed with status code \d+/i,
  /HTTP \d{3}/i,
  /Network Error/i,
  /gateway timeout/i,
  /bad gateway/i,
  /service unavailable/i,
  /upstream/i,
  /proxy/i,
  /timeout/i,
  /timed out/i,
  /ECONNABORTED/i,
]

type MessageErrorState = {
  content: string
  kind: 'generic' | 'model-price'
  showSettingsLink: boolean
  title: string
  tone: 'neutral' | 'warning'
}

export function isAdminRole(role?: number | null): boolean {
  return role != null && role >= 10
}

export function isErrorMessage(message: Message): boolean {
  return (
    message.status === MESSAGE_STATUS.ERROR &&
    !isRecoverableImageGenerationErrorMessage(message) &&
    !isLegacyImageGeneration524ErrorMessage(message)
  )
}

export function getMessageErrorState(
  message: Message,
  isAdmin: boolean
): MessageErrorState | null {
  if (!isErrorMessage(message)) {
    return null
  }

  const content = getMessageContent(message) || FALLBACK_ERROR_CONTENT
  const isModelPriceError = message.errorCode === MODEL_PRICE_ERROR_CODE

  return {
    content: isModelPriceError ? content : getSafeMessageErrorContent(content),
    kind: isModelPriceError ? 'model-price' : 'generic',
    showSettingsLink: isModelPriceError && isAdmin,
    title: isModelPriceError
      ? 'Model Price Not Configured'
      : RESPONSE_NOT_COMPLETED_TITLE,
    tone: isModelPriceError ? 'warning' : 'neutral',
  }
}

export function getSafeMessageErrorContent(content: string): string {
  if (isRequestErrorDetailContent(content)) {
    return RETRYABLE_RESPONSE_ERROR_CONTENT
  }
  return content
}

export function isRequestErrorDetailContent(content: string): boolean {
  return REQUEST_ERROR_DETAIL_PATTERNS.some((pattern) => pattern.test(content))
}
