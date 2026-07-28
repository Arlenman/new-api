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
import { getApiKeyDisplayLabel } from './token-selection.ts'

interface RuntimeSessionResponse {
  success: boolean
  message?: string
  data?: {
    credential: string
    expires_at: number
    token: {
      name?: string | null
      group?: string | null
      display_label?: string | null
    }
  }
}

type RuntimeSessionCreator = (
  tool: 'infinite-canvas',
  tokenId: number
) => Promise<RuntimeSessionResponse>

export interface InfiniteCanvasRuntimeSession {
  credential: string
  expiresAt: number
  displayLabel: string
}

export async function requestInfiniteCanvasRuntimeSession(
  createSession: RuntimeSessionCreator,
  tokenId: number,
  unnamedApiKeyLabel: string
): Promise<InfiniteCanvasRuntimeSession> {
  const response = await createSession('infinite-canvas', tokenId)
  const session = response.data
  const credential = session?.credential.trim()
  if (!response.success || !session || !credential?.startsWith('utrs_')) {
    throw new Error(response.message || 'Failed to create runtime credential')
  }

  return {
    credential,
    expiresAt: session.expires_at,
    displayLabel: getApiKeyDisplayLabel(session.token, unnamedApiKeyLabel),
  }
}
