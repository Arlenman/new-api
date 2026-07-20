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
import { LoaderCircle } from 'lucide-react'
import { useEffect, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

import {
  getManagedUpstreamPrioritySchedule,
  updateManagedUpstreamPrioritySchedule,
} from '../api'

interface UpstreamPriorityScheduleDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function UpstreamPriorityScheduleDialog(
  props: UpstreamPriorityScheduleDialogProps
) {
  const { t } = useTranslation()
  const [enabled, setEnabled] = useState(false)
  const [intervalSeconds, setIntervalSeconds] = useState('300')
  const [maxTestLatencySeconds, setMaxTestLatencySeconds] = useState('5')
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [errorMessage, setErrorMessage] = useState('')

  useEffect(() => {
    if (!props.open) return

    let active = true
    setLoading(true)
    setErrorMessage('')

    void getManagedUpstreamPrioritySchedule()
      .then((response) => {
        if (!active) return
        if (!response.success || !response.data) {
          setErrorMessage(
            response.message || t('Failed to load priority schedule')
          )
          return
        }
        setEnabled(response.data.enabled)
        setIntervalSeconds(String(response.data.interval_seconds))
        setMaxTestLatencySeconds(String(response.data.max_test_latency_seconds))
      })
      .catch((error) => {
        if (!active) return
        setErrorMessage(
          error instanceof Error
            ? error.message
            : t('Failed to load priority schedule')
        )
      })
      .finally(() => {
        if (active) setLoading(false)
      })

    return () => {
      active = false
    }
  }, [props.open, t])

  function handleOpenChange(nextOpen: boolean) {
    if (saving) return
    props.onOpenChange(nextOpen)
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (loading || saving) return

    const parsedIntervalSeconds = Number(intervalSeconds)
    if (
      !Number.isInteger(parsedIntervalSeconds) ||
      parsedIntervalSeconds < 15 ||
      parsedIntervalSeconds > 86400
    ) {
      setErrorMessage(
        t('Execution interval must be an integer between 15 and 86400 seconds')
      )
      return
    }

    const parsedMaxTestLatencySeconds = Number(maxTestLatencySeconds)
    if (
      !Number.isInteger(parsedMaxTestLatencySeconds) ||
      parsedMaxTestLatencySeconds < 1 ||
      parsedMaxTestLatencySeconds > 300
    ) {
      setErrorMessage(
        t('Maximum test latency must be an integer between 1 and 300 seconds')
      )
      return
    }

    setSaving(true)
    setErrorMessage('')
    try {
      const response = await updateManagedUpstreamPrioritySchedule({
        enabled,
        interval_seconds: parsedIntervalSeconds,
        max_test_latency_seconds: parsedMaxTestLatencySeconds,
      })
      if (!response.success) {
        setErrorMessage(
          response.message || t('Failed to save priority schedule')
        )
        return
      }
      toast.success(t('Priority schedule saved'))
      props.onOpenChange(false)
    } catch (error) {
      setErrorMessage(
        error instanceof Error
          ? error.message
          : t('Failed to save priority schedule')
      )
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={props.open} onOpenChange={handleOpenChange}>
      <DialogContent showCloseButton={!saving} className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Cost-effective priority scheduling')}</DialogTitle>
          <DialogDescription>
            {t(
              'Automatically rank upstream channels by effective multiplier and sync priorities after a successful channel test.'
            )}
          </DialogDescription>
        </DialogHeader>

        <form className='space-y-4' onSubmit={handleSubmit}>
          {errorMessage && (
            <Alert variant='destructive'>
              <AlertDescription>{errorMessage}</AlertDescription>
            </Alert>
          )}

          {loading ? (
            <div className='text-muted-foreground flex min-h-32 items-center justify-center gap-2'>
              <LoaderCircle className='size-4 animate-spin' />
              {t('Loading...')}
            </div>
          ) : (
            <fieldset className='space-y-4' disabled={saving}>
              <div className='flex items-center justify-between gap-4 rounded-lg border p-3'>
                <Label htmlFor='upstream-priority-schedule-enabled'>
                  {t('Enable scheduled priority adjustment')}
                </Label>
                <Switch
                  id='upstream-priority-schedule-enabled'
                  checked={enabled}
                  onCheckedChange={setEnabled}
                />
              </div>

              {enabled && (
                <div className='space-y-4 rounded-lg border p-3'>
                  <div className='space-y-1.5'>
                    <Label htmlFor='upstream-priority-schedule-interval'>
                      {t('Execution interval')}
                    </Label>
                    <div className='flex items-center gap-2'>
                      <Input
                        id='upstream-priority-schedule-interval'
                        type='number'
                        min='15'
                        max='86400'
                        step='1'
                        inputMode='numeric'
                        value={intervalSeconds}
                        onChange={(event) =>
                          setIntervalSeconds(event.target.value)
                        }
                      />
                      <span className='text-muted-foreground shrink-0'>
                        {t('seconds')}
                      </span>
                    </div>
                  </div>

                  <div className='space-y-1.5'>
                    <Label htmlFor='upstream-priority-schedule-latency'>
                      {t('Maximum test latency')}
                    </Label>
                    <div className='flex items-center gap-2'>
                      <Input
                        id='upstream-priority-schedule-latency'
                        type='number'
                        min='1'
                        max='300'
                        step='1'
                        inputMode='numeric'
                        value={maxTestLatencySeconds}
                        onChange={(event) =>
                          setMaxTestLatencySeconds(event.target.value)
                        }
                      />
                      <span className='text-muted-foreground shrink-0'>
                        {t('seconds')}
                      </span>
                    </div>
                  </div>
                </div>
              )}
            </fieldset>
          )}

          <DialogFooter>
            <Button
              type='button'
              variant='outline'
              disabled={saving}
              onClick={() => handleOpenChange(false)}
            >
              {t('Cancel')}
            </Button>
            <Button type='submit' disabled={loading || saving}>
              {saving && <LoaderCircle className='animate-spin' />}
              {t('Save schedule')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
