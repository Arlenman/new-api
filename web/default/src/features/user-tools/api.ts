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
import { api } from '@/lib/api'

export type UserTool = 'image-playground' | 'infinite-canvas'

interface ApiResponse<T> {
  success: boolean
  message: string
  data: T
}

export interface UserToolPreference {
  selected_token_id: number
  updated_at: number
}

export interface UserToolRuntimeSession {
  credential: string
  expires_at: number
  token: {
    id: number
    name: string
    masked_key: string
  }
}

export async function getUserToolPreference(
  tool: UserTool
): Promise<ApiResponse<UserToolPreference>> {
  const response = await api.get(`/api/user-tools/${tool}/preferences`)
  return response.data
}

export async function updateUserToolPreference(
  tool: UserTool,
  selectedTokenId: number
): Promise<ApiResponse<UserToolPreference>> {
  const response = await api.put(`/api/user-tools/${tool}/preferences`, {
    selected_token_id: selectedTokenId,
  })
  return response.data
}

export async function createUserToolRuntimeSession(
  tool: UserTool,
  tokenId: number
): Promise<ApiResponse<UserToolRuntimeSession>> {
  const response = await api.post(`/api/user-tools/${tool}/runtime-session`, {
    token_id: tokenId,
  })
  return response.data
}
