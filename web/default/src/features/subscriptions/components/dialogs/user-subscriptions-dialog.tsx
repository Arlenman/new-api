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
import { Pencil, Plus } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { StaticDataTable } from '@/components/data-table'
import {
  sideDrawerContentClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { StatusBadge } from '@/components/status-badge'
import { TableId } from '@/components/table-id'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from '@/components/ui/sheet'
import dayjs from '@/lib/dayjs'
import { getCurrencyDisplay, getCurrencyLabel } from '@/lib/currency'
import {
  formatQuota,
  parseQuotaFromDollars,
  quotaUnitsToDollars,
} from '@/lib/format'
import {
  getAdminPlans,
  getUserSubscriptions,
  createUserSubscription,
  invalidateUserSubscription,
  deleteUserSubscription,
  updateUserSubscription,
} from '../../api'
import { formatTimestamp } from '../../lib'
import type {
  PlanRecord,
  SubscriptionQuotaAdjustMode,
  UserSubscriptionRecord,
} from '../../types'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  user: { id: number; username?: string } | null
  onSuccess?: () => void
}

function SubscriptionStatusBadge(props: {
  sub: UserSubscriptionRecord['subscription']
  t: (key: string) => string
}) {
  // eslint-disable-next-line react-hooks/purity
  const now = Date.now() / 1000
  const isExpired = (props.sub.end_time || 0) > 0 && props.sub.end_time < now
  const isActive = props.sub.status === 'active' && !isExpired
  if (isActive)
    return (
      <StatusBadge
        label={props.t('Active')}
        variant='success'
        copyable={false}
      />
    )
  if (props.sub.status === 'cancelled')
    return (
      <StatusBadge
        label={props.t('Invalidated')}
        variant='neutral'
        copyable={false}
      />
    )
  return (
    <StatusBadge
      label={props.t('Expired')}
      variant='neutral'
      copyable={false}
    />
  )
}

function formatSubscriptionInput(timestamp: number) {
  if (!timestamp || timestamp < 0) return ''
  return dayjs(timestamp * 1000).format('YYYY-MM-DDTHH:mm:ss')
}

function parseSubscriptionInput(value: string) {
  if (!value) return 0
  const timestamp = Math.floor(new Date(value).getTime() / 1000)
  return Number.isFinite(timestamp) ? timestamp : 0
}

function formatAmountInput(value: number, tokensOnly: boolean) {
  if (!Number.isFinite(value)) return ''
  return tokensOnly ? String(Math.round(value)) : value.toFixed(6)
}

function getAdjustedQuota(
  current: number,
  mode: SubscriptionQuotaAdjustMode,
  value: number
) {
  switch (mode) {
    case 'add':
      return current + value
    case 'subtract':
      return current - value
    case 'override':
      return value
  }
}

function EditUserSubscriptionDialog(props: {
  record: UserSubscriptionRecord | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onSaved: () => Promise<void>
}) {
  const { t } = useTranslation()
  const [endTime, setEndTime] = useState('')
  const [mode, setMode] = useState<SubscriptionQuotaAdjustMode>('override')
  const [amount, setAmount] = useState('')
  const [saving, setSaving] = useState(false)
  const { meta: currencyMeta } = getCurrencyDisplay()
  const currencyLabel = getCurrencyLabel()
  const tokensOnly = currencyMeta.kind === 'tokens'

  const sub = props.record?.subscription

  useEffect(() => {
    if (!props.open || !sub) return
    setEndTime(formatSubscriptionInput(sub.end_time))
    setMode('override')
    setAmount(formatAmountInput(quotaUnitsToDollars(sub.amount_total || 0), tokensOnly))
  }, [props.open, sub, tokensOnly])

  const amountValue = parseFloat(amount) || 0
  const quotaValue =
    mode === 'override'
      ? parseQuotaFromDollars(amountValue)
      : parseQuotaFromDollars(Math.abs(amountValue))
  const currentTotal = Number(sub?.amount_total || 0)
  const nextTotal = getAdjustedQuota(currentTotal, mode, quotaValue)

  const handleCancel = () => {
    props.onOpenChange(false)
  }

  const handleSave = async () => {
    if (!sub) return
    const parsedEndTime = parseSubscriptionInput(endTime)
    if (parsedEndTime <= 0) {
      toast.error(t('Please select an expiration time'))
      return
    }
    if (mode !== 'override' && quotaValue <= 0) {
      toast.error(t('Please enter a quota amount'))
      return
    }
    if (nextTotal < 0) {
      toast.error(t('Total quota cannot be negative'))
      return
    }

    setSaving(true)
    try {
      const res = await updateUserSubscription(sub.id, {
        end_time: parsedEndTime,
        quota_mode: mode,
        quota_value: quotaValue,
      })
      if (res.success) {
        toast.success(res.data?.message || t('Saved successfully'))
        props.onOpenChange(false)
        await props.onSaved()
      } else {
        toast.error(res.message || t('Save failed'))
      }
    } catch {
      toast.error(t('Request failed'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Edit subscription')}
      description={sub ? `#${sub.id}` : ''}
      contentHeight='auto'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button variant='outline' onClick={handleCancel}>
            {t('Cancel')}
          </Button>
          <Button onClick={handleSave} disabled={saving}>
            {saving ? t('Saving...') : t('Save changes')}
          </Button>
        </>
      }
    >
      <div className='space-y-4'>
        <div className='space-y-2'>
          <Label>{t('Expiration time')}</Label>
          <Input
            type='datetime-local'
            step={1}
            value={endTime}
            onChange={(event) => setEndTime(event.target.value)}
          />
        </div>

        <div className='space-y-2'>
          <Label>{t('Mode')}</Label>
          <div className='flex gap-1'>
            {(['add', 'subtract', 'override'] as const).map((item) => (
              <Button
                key={item}
                type='button'
                variant={mode === item ? 'default' : 'outline'}
                size='sm'
                onClick={() => {
                  setMode(item)
                  setAmount(
                    item === 'override' && sub
                      ? formatAmountInput(
                          quotaUnitsToDollars(sub.amount_total || 0),
                          tokensOnly
                        )
                      : ''
                  )
                }}
              >
                {item === 'add'
                  ? t('Add')
                  : item === 'subtract'
                    ? t('Subtract')
                    : t('Override')}
              </Button>
            ))}
          </div>
        </div>

        <div className='space-y-2'>
          <Label>
            {t('Total Quota')} ({currencyLabel})
          </Label>
          <Input
            type='number'
            step={tokensOnly ? 1 : 0.000001}
            min={mode === 'subtract' || mode === 'add' ? 0 : undefined}
            value={amount}
            onChange={(event) => setAmount(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter') handleSave()
            }}
          />
        </div>

        {sub && (
          <div className='text-muted-foreground space-y-1 text-sm'>
            <div>
              {t('Used')}: {formatQuota(sub.amount_used || 0)}
            </div>
            <div>
              {t('Current total')}: {currentTotal > 0 ? formatQuota(currentTotal) : t('Unlimited')}
            </div>
            <div>
              {t('New total')}: {nextTotal > 0 ? formatQuota(nextTotal) : t('Unlimited')}
            </div>
          </div>
        )}
      </div>
    </Dialog>
  )
}

export function UserSubscriptionsDialog(props: Props) {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(false)
  const [creating, setCreating] = useState(false)
  const [plans, setPlans] = useState<PlanRecord[]>([])
  const [subs, setSubs] = useState<UserSubscriptionRecord[]>([])
  const [selectedPlanId, setSelectedPlanId] = useState<string>('')
  const [confirmAction, setConfirmAction] = useState<{
    type: 'invalidate' | 'delete'
    subId: number
  } | null>(null)
  const [editingRecord, setEditingRecord] =
    useState<UserSubscriptionRecord | null>(null)

  const planTitleMap = useMemo(() => {
    const map = new Map<number, string>()
    plans.forEach((p) => {
      if (p.plan.id) map.set(p.plan.id, p.plan.title || `#${p.plan.id}`)
    })
    return map
  }, [plans])

  const loadData = useCallback(async () => {
    if (!props.user?.id) return
    setLoading(true)
    try {
      const [plansRes, subsRes] = await Promise.all([
        getAdminPlans(),
        getUserSubscriptions(props.user.id),
      ])
      if (plansRes.success) setPlans(plansRes.data || [])
      if (subsRes.success) setSubs(subsRes.data || [])
    } catch {
      toast.error(t('Loading failed'))
    } finally {
      setLoading(false)
    }
  }, [props.user?.id, t])

  useEffect(() => {
    if (props.open && props.user?.id) {
      setSelectedPlanId('')
      loadData()
    }
  }, [props.open, props.user?.id, loadData])

  const handleCreate = async () => {
    if (!props.user?.id || !selectedPlanId) {
      toast.error(t('Please select a subscription plan'))
      return
    }
    setCreating(true)
    try {
      const res = await createUserSubscription(props.user.id, {
        plan_id: Number(selectedPlanId),
      })
      if (res.success) {
        toast.success(res.data?.message || t('Added successfully'))
        setSelectedPlanId('')
        await loadData()
        props.onSuccess?.()
      }
    } catch {
      toast.error(t('Request failed'))
    } finally {
      setCreating(false)
    }
  }

  const handleConfirmAction = async () => {
    if (!confirmAction) return
    try {
      if (confirmAction.type === 'invalidate') {
        const res = await invalidateUserSubscription(confirmAction.subId)
        if (res.success) {
          toast.success(res.data?.message || t('Has been invalidated'))
          await loadData()
          props.onSuccess?.()
        }
      } else {
        const res = await deleteUserSubscription(confirmAction.subId)
        if (res.success) {
          toast.success(t('Deleted'))
          await loadData()
          props.onSuccess?.()
        }
      }
    } catch {
      toast.error(t('Operation failed'))
    } finally {
      setConfirmAction(null)
    }
  }

  return (
    <>
      <Sheet open={props.open} onOpenChange={props.onOpenChange}>
        <SheetContent className={sideDrawerContentClassName('sm:max-w-2xl')}>
          <SheetHeader className={sideDrawerHeaderClassName()}>
            <SheetTitle>{t('User Subscription Management')}</SheetTitle>
            <SheetDescription>
              {props.user?.username || '-'} (ID: {props.user?.id || '-'})
            </SheetDescription>
          </SheetHeader>

          <div className={sideDrawerFormClassName()}>
            <div className='flex gap-2'>
              <Select
                items={[
                  ...plans.map((p) => ({
                    value: String(p.plan.id),
                    label: (
                      <>
                        {p.plan.title}($
                        {Number(p.plan.price_amount || 0).toFixed(2)})
                      </>
                    ),
                  })),
                ]}
                value={selectedPlanId}
                onValueChange={(v) => v !== null && setSelectedPlanId(v)}
              >
                <SelectTrigger className='flex-1'>
                  <SelectValue placeholder={t('Select subscription plan')} />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    {plans.map((p) => (
                      <SelectItem key={p.plan.id} value={String(p.plan.id)}>
                        {p.plan.title} ($
                        {Number(p.plan.price_amount || 0).toFixed(2)})
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <Button
                onClick={handleCreate}
                disabled={creating || !selectedPlanId}
              >
                <Plus className='mr-1 h-4 w-4' />
                {t('Add subscription')}
              </Button>
            </div>

            <StaticDataTable
              data={loading ? [] : subs}
              getRowKey={(record) => record.subscription.id}
              emptyClassName={loading ? 'py-8' : 'text-muted-foreground py-8'}
              emptyContent={
                loading ? t('Loading...') : t('No subscription records')
              }
              columns={[
                {
                  id: 'id',
                  header: t('ID'),
                  cell: (record) => <TableId value={record.subscription.id} />,
                },
                {
                  id: 'plan',
                  header: t('Plan'),
                  cell: (record) => {
                    const sub = record.subscription

                    return (
                      <div>
                        <div className='font-medium'>
                          {planTitleMap.get(sub.plan_id) || `#${sub.plan_id}`}
                        </div>
                        <div className='text-muted-foreground text-sm'>
                          {t('Source')}: {sub.source || '-'}
                        </div>
                      </div>
                    )
                  },
                },
                {
                  id: 'status',
                  header: t('Status'),
                  cell: (record) => (
                    <SubscriptionStatusBadge sub={record.subscription} t={t} />
                  ),
                },
                {
                  id: 'validity',
                  header: t('Validity'),
                  cell: (record) => {
                    const sub = record.subscription

                    return (
                      <div className='text-sm'>
                        <div>
                          {t('Start')}: {formatTimestamp(sub.start_time)}
                        </div>
                        <div>
                          {t('End')}: {formatTimestamp(sub.end_time)}
                        </div>
                      </div>
                    )
                  },
                },
                {
                  id: 'quota',
                  header: t('Total Quota'),
                  cell: (record) => {
                    const sub = record.subscription
                    const total = Number(sub.amount_total || 0)
                    const used = Number(sub.amount_used || 0)
                    return total > 0
                      ? `${formatQuota(used)}/${formatQuota(total)}`
                      : t('Unlimited')
                  },
                },
                {
                  id: 'actions',
                  header: t('Actions'),
                  className: 'text-right',
                  cellClassName: 'text-right',
                  cell: (record) => {
                    const sub = record.subscription
                    const now = Date.now() / 1000
                    const isExpired =
                      (sub.end_time || 0) > 0 && sub.end_time < now
                    const isActive = sub.status === 'active' && !isExpired

                    return (
                      <div className='flex justify-end gap-1'>
                        <Button
                          size='sm'
                          variant='outline'
                          disabled={!isActive}
                          onClick={() =>
                            setConfirmAction({
                              type: 'invalidate',
                              subId: sub.id,
                            })
                          }
                        >
                          {t('Invalidate')}
                        </Button>
                        <Button
                          size='sm'
                          variant='destructive'
                          onClick={() =>
                            setConfirmAction({
                              type: 'delete',
                              subId: sub.id,
                            })
                          }
                        >
                          {t('Delete')}
                        </Button>
                        <Button
                          size='sm'
                          variant='outline'
                          onClick={() => setEditingRecord(record)}
                        >
                          <Pencil className='mr-1 h-3.5 w-3.5' />
                          {t('Edit')}
                        </Button>
                      </div>
                    )
                  },
                },
              ]}
            />
          </div>
        </SheetContent>
      </Sheet>

      {confirmAction && (
        <ConfirmDialog
          open
          onOpenChange={(v) => !v && setConfirmAction(null)}
          title={
            confirmAction.type === 'invalidate'
              ? t('Confirm invalidate')
              : t('Confirm delete')
          }
          desc={
            confirmAction.type === 'invalidate'
              ? t(
                  'After invalidating, this subscription will be immediately deactivated. Historical records are not affected. Continue?'
                )
              : t(
                  'Deleting will permanently remove this subscription record (including benefit details). Continue?'
                )
          }
          handleConfirm={handleConfirmAction}
          destructive={confirmAction.type === 'delete'}
        />
      )}
      <EditUserSubscriptionDialog
        open={editingRecord != null}
        record={editingRecord}
        onOpenChange={(open) => {
          if (!open) setEditingRecord(null)
        }}
        onSaved={async () => {
          setEditingRecord(null)
          await loadData()
          props.onSuccess?.()
        }}
      />
    </>
  )
}
