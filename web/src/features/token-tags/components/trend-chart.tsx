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
import { VChart } from '@visactor/react-vchart'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { getChartColor } from '@/lib/colors'
import { formatNumber, formatQuota } from '@/lib/format'
import { useChartTheme } from '@/lib/use-chart-theme'
import { cn } from '@/lib/utils'
import { VCHART_OPTION } from '@/lib/vchart'

import {
  filterTagTrendChartData,
  toggleTagTrendSeriesVisibility,
  type TokenTagRankingMetric,
  type TokenTagTrendData,
} from '../lib'

interface TrendChartProps {
  data: TokenTagTrendData
  metric: TokenTagRankingMetric
}

export function TrendChart({ data, metric }: TrendChartProps) {
  const { t } = useTranslation()
  const { resolvedTheme, themeReady } = useChartTheme()
  const [hiddenSeriesKeys, setHiddenSeriesKeys] = useState<Set<string>>(
    () => new Set()
  )
  const chartTextColor =
    resolvedTheme === 'dark'
      ? 'rgba(255, 255, 255, 0.68)'
      : 'rgba(15, 23, 42, 0.58)'
  const chartGridColor =
    resolvedTheme === 'dark'
      ? 'rgba(255, 255, 255, 0.12)'
      : 'rgba(15, 23, 42, 0.12)'
  const pointStrokeColor = resolvedTheme === 'dark' ? '#111827' : '#ffffff'

  useEffect(() => {
    const availableSeriesKeys = new Set(data.series.map((series) => series.key))
    setHiddenSeriesKeys((current) => {
      const next = new Set(
        [...current].filter((seriesKey) => availableSeriesKeys.has(seriesKey))
      )
      if (next.size === current.size) {
        return current
      }
      return next
    })
  }, [data.series])

  const visibleData = useMemo(
    () => filterTagTrendChartData(data, hiddenSeriesKeys),
    [data, hiddenSeriesKeys]
  )
  const seriesColors = useMemo(
    () =>
      new Map(
        data.series.map((series, index) => [series.key, getChartColor(index)])
      ),
    [data.series]
  )

  const formatMetric = useCallback(
    (value: number) => {
      if (metric === 'quota') {
        return formatQuota(value)
      }
      return formatNumber(value)
    },
    [metric]
  )

  const spec = useMemo(() => {
    if (visibleData.data.length === 0) {
      return null
    }

    const getSeriesColor = (datum: Record<string, unknown>) =>
      seriesColors.get(String(datum.seriesKey || '')) || getChartColor(0)

    const tooltipContent = [
      {
        key: t('Period'),
        value: (datum: Record<string, unknown>) =>
          String(datum.periodLabel || ''),
      },
      {
        key: t('Key Tag'),
        value: (datum: Record<string, unknown>) => String(datum.tagName || ''),
      },
      ...(visibleData.data.some((item) => item.username)
        ? [
            {
              key: t('User'),
              value: (datum: Record<string, unknown>) =>
                String(datum.username || '-'),
            },
          ]
        : []),
      {
        key: t('Value'),
        value: (datum: Record<string, unknown>) =>
          formatMetric(Number(datum.value) || 0),
      },
    ]

    return {
      type: 'line' as const,
      data: [{ id: 'token-tag-trend', values: visibleData.data }],
      xField: 'periodLabel',
      yField: 'value',
      seriesField: 'seriesKey',
      line: {
        style: { lineWidth: 2, stroke: getSeriesColor },
      },
      point: {
        visible: true,
        style: {
          size: 5,
          fill: getSeriesColor,
          stroke: pointStrokeColor,
          lineWidth: 1.5,
        },
      },
      legends: {
        visible: false,
      },
      axes: [
        {
          orient: 'bottom' as const,
          label: {
            style: { fill: chartTextColor, fontSize: 10 },
            autoHide: true,
            autoLimit: true,
          },
          tick: { visible: false },
        },
        {
          orient: 'left' as const,
          label: {
            formatMethod: (value: number | string) =>
              formatMetric(Number(value) || 0),
            style: { fill: chartTextColor, fontSize: 10 },
          },
          grid: {
            visible: true,
            style: { lineDash: [3, 3], stroke: chartGridColor },
          },
        },
      ],
      tooltip: {
        mark: {
          title: {
            value: (datum: Record<string, unknown>) =>
              String(datum.seriesLabel || ''),
          },
          content: tooltipContent,
        },
      },
      animationAppear: { duration: 400 },
    }
  }, [
    chartGridColor,
    chartTextColor,
    formatMetric,
    pointStrokeColor,
    seriesColors,
    t,
    visibleData.data,
  ])

  if (!themeReady || data.data.length === 0 || data.series.length === 0) {
    return (
      <div className='text-muted-foreground flex h-72 items-center justify-center text-sm'>
        {t('No data')}
      </div>
    )
  }

  const chartWidth = Math.max(720, data.periods.length * 64)
  return (
    <div className='space-y-3'>
      <div className='overflow-x-auto pb-2'>
        <div className='h-[360px]' style={{ width: chartWidth }}>
          {spec ? (
            <VChart
              key={`token-tag-trend-${metric}-${resolvedTheme}-${[...hiddenSeriesKeys].sort().join('-')}`}
              spec={{
                ...spec,
                theme: resolvedTheme === 'dark' ? 'dark' : 'light',
                background: 'transparent',
              }}
              option={VCHART_OPTION}
            />
          ) : (
            <div className='text-muted-foreground flex h-full items-center justify-center text-sm'>
              {t('All tags are hidden. Click a tag below to show it.')}
            </div>
          )}
        </div>
      </div>
      <div className='flex flex-wrap justify-center gap-2'>
        {data.series.map((series) => {
          const hidden = hiddenSeriesKeys.has(series.key)
          const color = seriesColors.get(series.key) || getChartColor(0)
          return (
            <button
              key={series.key}
              type='button'
              aria-pressed={!hidden}
              className={cn(
                'border-border bg-background hover:bg-muted focus-visible:ring-ring inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-xs transition focus-visible:ring-2 focus-visible:outline-none',
                hidden && 'text-muted-foreground opacity-55'
              )}
              onClick={() =>
                setHiddenSeriesKeys((current) =>
                  toggleTagTrendSeriesVisibility(current, series.key)
                )
              }
            >
              <span
                className='size-2.5 rounded-full border'
                style={{
                  backgroundColor: hidden ? 'transparent' : color,
                  borderColor: color,
                }}
              />
              <span className={cn(hidden && 'line-through')}>
                {series.label}
              </span>
            </button>
          )
        })}
      </div>
    </div>
  )
}
