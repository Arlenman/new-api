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
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Checkbox } from '@/components/ui/checkbox'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

import { setUserHidden } from '../api'
import { ERROR_MESSAGES, SUCCESS_MESSAGES, isUserDeleted } from '../constants'
import type { User } from '../types'
import { useUsers } from './users-provider'

export function HiddenUserCheckbox({ user }: { user: User }) {
  const { t } = useTranslation()
  const { triggerRefresh } = useUsers()
  const [hidden, setHidden] = useState(user.hidden === true)
  const [isUpdating, setIsUpdating] = useState(false)

  useEffect(() => {
    setHidden(user.hidden === true)
  }, [user.hidden])

  const handleCheckedChange = async (checked: boolean) => {
    if (isUpdating || checked === hidden) return

    const previousHidden = hidden
    setHidden(checked)
    setIsUpdating(true)

    try {
      const result = await setUserHidden(user.id, checked)
      if (!result.success) {
        setHidden(previousHidden)
        toast.error(result.message || t(ERROR_MESSAGES.UPDATE_FAILED))
        return
      }

      toast.success(t(SUCCESS_MESSAGES.USER_UPDATED))
      triggerRefresh()
    } catch {
      setHidden(previousHidden)
      toast.error(t(ERROR_MESSAGES.UNEXPECTED))
    } finally {
      setIsUpdating(false)
    }
  }

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <div className='inline-flex cursor-help items-center justify-center' />
        }
      >
        <Checkbox
          checked={hidden}
          disabled={isUpdating || isUserDeleted(user)}
          onCheckedChange={(checked) =>
            void handleCheckedChange(checked === true)
          }
          aria-label={t('Hide user')}
        />
      </TooltipTrigger>
      <TooltipContent>
        {t('Hidden users are excluded from admin lists, logs, and analytics.')}
      </TooltipContent>
    </Tooltip>
  )
}
