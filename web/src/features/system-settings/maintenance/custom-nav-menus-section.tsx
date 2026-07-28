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
import { useEffect, useMemo, useState } from 'react'
import * as z from 'zod'
import { useFieldArray, useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import {
  SettingsControlChildren,
  SettingsControlGroup,
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import {
  CUSTOM_NAV_MENUS_DEFAULT,
  type CustomNavMenuConfig,
  type CustomNavPlacement,
  serializeCustomNavMenus,
} from './config'

type CustomNavMenusSectionProps = {
  config: CustomNavMenuConfig[]
  initialSerialized: string
}

const customNavMenuSchema = z.object({
  id: z
    .string()
    .trim()
    .min(1, 'Menu ID is required')
    .max(80, 'Menu ID must be 80 characters or fewer')
    .regex(/^[a-zA-Z0-9_-]+$/, 'Use letters, numbers, underscores, or hyphens'),
  title: z
    .string()
    .trim()
    .min(1, 'Menu name is required')
    .max(80, 'Menu name must be 80 characters or fewer'),
  url: z
    .string()
    .trim()
    .min(1, 'Link URL is required')
    .max(500, 'Link URL must be 500 characters or fewer')
    .refine((value) => {
      if (value.startsWith('/') && !value.startsWith('//')) return true
      try {
        const parsed = new URL(value)
        return parsed.protocol === 'http:' || parsed.protocol === 'https:'
      } catch {
        return false
      }
    }, 'Use a relative path or an HTTP(S) URL'),
  enabled: z.boolean(),
  placement: z.enum(['top', 'sidebar', 'both']),
  openInNewTab: z.boolean(),
  requireAuth: z.boolean(),
})

const customNavMenusSchema = z.object({
  menus: z
    .array(customNavMenuSchema)
    .max(30, 'Custom menu count must be 30 or fewer')
    .superRefine((menus, ctx) => {
      const seen = new Map<string, number>()
      menus.forEach((menu, index) => {
        const previousIndex = seen.get(menu.id)
        if (previousIndex !== undefined) {
          ctx.addIssue({
            code: 'custom',
            message: 'Menu ID must be unique',
            path: [index, 'id'],
          })
          ctx.addIssue({
            code: 'custom',
            message: 'Menu ID must be unique',
            path: [previousIndex, 'id'],
          })
          return
        }
        seen.set(menu.id, index)
      })
    }),
})

type CustomNavMenusFormValues = z.infer<typeof customNavMenusSchema>

const createMenu = (index: number): CustomNavMenuConfig => ({
  id: `custom-${Date.now()}-${index}`,
  title: '',
  url: '',
  enabled: true,
  placement: 'both',
  openInNewTab: false,
  requireAuth: false,
})

const normalizeMenus = (
  menus: CustomNavMenusFormValues['menus']
): CustomNavMenuConfig[] =>
  menus.map((menu) => ({
    ...menu,
    id: menu.id.trim(),
    title: menu.title.trim(),
    url: menu.url.trim(),
  }))

export function CustomNavMenusSection({
  config,
  initialSerialized,
}: CustomNavMenusSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [savedSerialized, setSavedSerialized] = useState(initialSerialized)
  const formDefaults = useMemo<CustomNavMenusFormValues>(
    () => ({ menus: config }),
    [config]
  )

  const form = useForm<CustomNavMenusFormValues>({
    resolver: zodResolver(customNavMenusSchema),
    defaultValues: formDefaults,
  })

  const fieldArray = useFieldArray({
    control: form.control,
    name: 'menus',
    keyName: 'fieldId',
  })

  useEffect(() => {
    form.reset(formDefaults)
    setSavedSerialized(initialSerialized)
  }, [form, formDefaults, initialSerialized])

  const onSubmit = async (values: CustomNavMenusFormValues) => {
    const normalizedMenus = normalizeMenus(values.menus)
    const serialized = serializeCustomNavMenus(normalizedMenus)
    if (serialized === savedSerialized) {
      return
    }

    const result = await updateOption.mutateAsync({
      key: 'CustomNavMenus',
      value: serialized,
    })
    if (result.success) {
      setSavedSerialized(serialized)
      form.reset({ menus: normalizedMenus })
    }
  }

  const resetToDefault = () => {
    form.reset({ menus: CUSTOM_NAV_MENUS_DEFAULT })
  }

  const placementOptions: Array<{ value: CustomNavPlacement; label: string }> =
    [
      { value: 'both', label: t('Top navigation and sidebar') },
      { value: 'top', label: t('Top navigation') },
      { value: 'sidebar', label: t('Sidebar') },
    ]

  return (
    <SettingsSection title={t('Custom menus')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            onReset={resetToDefault}
            isSaving={updateOption.isPending}
            resetLabel='Reset to empty'
            saveLabel='Save custom menus'
          />

          <SettingsControlGroup>
            <div className='flex items-center justify-between gap-3 border-b p-4'>
              <div className='min-w-0'>
                <div className='text-sm font-medium'>{t('Menu items')}</div>
                <p className='text-muted-foreground text-sm'>
                  {t('Add custom links to the top navigation or sidebar.')}
                </p>
              </div>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={() =>
                  fieldArray.append(createMenu(fieldArray.fields.length))
                }
              >
                <Plus className='size-4' />
                {t('Add menu')}
              </Button>
            </div>

            <SettingsControlChildren className='gap-4'>
              {fieldArray.fields.length === 0 ? (
                <div className='text-muted-foreground rounded-lg border border-dashed p-6 text-center text-sm'>
                  {t('No custom menus configured.')}
                </div>
              ) : (
                fieldArray.fields.map((field, index) => (
                  <div
                    key={field.fieldId}
                    className='rounded-lg border p-3 sm:p-4'
                  >
                    <div className='mb-3 flex items-center justify-between gap-3'>
                      <FormField
                        control={form.control}
                        name={`menus.${index}.enabled`}
                        render={({ field }) => (
                          <SettingsSwitchItem className='flex-1 border-b-0 p-0'>
                            <SettingsSwitchContent>
                              <FormLabel>{t('Enabled')}</FormLabel>
                              <FormDescription>
                                {t('Show or hide this menu item.')}
                              </FormDescription>
                            </SettingsSwitchContent>
                            <FormControl>
                              <Switch
                                checked={field.value}
                                onCheckedChange={field.onChange}
                              />
                            </FormControl>
                          </SettingsSwitchItem>
                        )}
                      />
                      <Button
                        type='button'
                        variant='ghost'
                        size='icon-sm'
                        aria-label={t('Delete menu')}
                        onClick={() => fieldArray.remove(index)}
                      >
                        <Trash2 className='size-4' />
                      </Button>
                    </div>

                    <div className='grid gap-3 md:grid-cols-2'>
                      <FormField
                        control={form.control}
                        name={`menus.${index}.id`}
                        render={({ field }) => (
                          <FormItem>
                            <FormLabel>{t('Menu ID')}</FormLabel>
                            <FormControl>
                              <Input placeholder='docs' {...field} />
                            </FormControl>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                      <FormField
                        control={form.control}
                        name={`menus.${index}.title`}
                        render={({ field }) => (
                          <FormItem>
                            <FormLabel>{t('Menu name')}</FormLabel>
                            <FormControl>
                              <Input placeholder={t('Docs')} {...field} />
                            </FormControl>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                      <FormField
                        control={form.control}
                        name={`menus.${index}.url`}
                        render={({ field }) => (
                          <FormItem className='md:col-span-2'>
                            <FormLabel>{t('Link URL')}</FormLabel>
                            <FormControl>
                              <Input
                                placeholder='https://example.com/docs'
                                {...field}
                              />
                            </FormControl>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                      <FormField
                        control={form.control}
                        name={`menus.${index}.placement`}
                        render={({ field }) => (
                          <FormItem>
                            <FormLabel>{t('Display position')}</FormLabel>
                            <Select
                              value={field.value}
                              onValueChange={field.onChange}
                              items={placementOptions}
                            >
                              <FormControl>
                                <SelectTrigger className='w-full'>
                                  <SelectValue />
                                </SelectTrigger>
                              </FormControl>
                              <SelectContent>
                                {placementOptions.map((option) => (
                                  <SelectItem
                                    key={option.value}
                                    value={option.value}
                                  >
                                    {option.label}
                                  </SelectItem>
                                ))}
                              </SelectContent>
                            </Select>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                      <div className='grid gap-3'>
                        <FormField
                          control={form.control}
                          name={`menus.${index}.openInNewTab`}
                          render={({ field }) => (
                            <SettingsSwitchItem className='border-b-0 p-0'>
                              <SettingsSwitchContent>
                                <FormLabel>{t('Open in new tab')}</FormLabel>
                              </SettingsSwitchContent>
                              <FormControl>
                                <Switch
                                  checked={field.value}
                                  onCheckedChange={field.onChange}
                                />
                              </FormControl>
                            </SettingsSwitchItem>
                          )}
                        />
                        <FormField
                          control={form.control}
                          name={`menus.${index}.requireAuth`}
                          render={({ field }) => (
                            <SettingsSwitchItem className='border-b-0 p-0'>
                              <SettingsSwitchContent>
                                <FormLabel>{t('Require login')}</FormLabel>
                              </SettingsSwitchContent>
                              <FormControl>
                                <Switch
                                  checked={field.value}
                                  onCheckedChange={field.onChange}
                                />
                              </FormControl>
                            </SettingsSwitchItem>
                          )}
                        />
                      </div>
                    </div>
                  </div>
                ))
              )}
            </SettingsControlChildren>
          </SettingsControlGroup>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
