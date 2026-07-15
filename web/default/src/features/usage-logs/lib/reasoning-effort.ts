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
import type { StatusBadgeProps } from '@/components/status-badge'

export interface ReasoningEffortMeta {
  label: string
  variant: StatusBadgeProps['variant']
}

export function getReasoningEffortMeta(
  effort: string | null | undefined
): ReasoningEffortMeta | null {
  const label = effort?.trim()
  if (!label) return null

  const normalizedEffort = label.toLowerCase()
  if (normalizedEffort === 'none') {
    return { label, variant: 'neutral' }
  }
  if (normalizedEffort === 'medium') {
    return { label, variant: 'yellow' }
  }
  if (['high', 'xhigh', 'max'].includes(normalizedEffort)) {
    return { label, variant: 'orange' }
  }
  return { label, variant: 'green' }
}
