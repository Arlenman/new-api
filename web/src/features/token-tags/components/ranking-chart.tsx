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
import { useCallback, useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { formatNumber, formatQuota } from '@/lib/format'
import { useChartTheme } from '@/lib/use-chart-theme'
import { VCHART_OPTION } from '@/lib/vchart'

import type { TokenTagChartData, TokenTagRankingMetric } from '../lib'

interface RankingChartProps {
  data: TokenTagChartData
  metric: TokenTagRankingMetric
  colorMap: Map<string, string>
  kind: 'tag' | 'key'
}

export function RankingChart({
  data,
  metric,
  colorMap,
  kind,
}: RankingChartProps) {
  const { t } = useTranslation()
  const { resolvedTheme, themeReady } = useChartTheme()
  const chartTextColor =
    resolvedTheme === 'dark'
      ? 'rgba(255, 255, 255, 0.68)'
      : 'rgba(15, 23, 42, 0.58)'
  const chartGridColor =
    resolvedTheme === 'dark'
      ? 'rgba(255, 255, 255, 0.12)'
      : 'rgba(15, 23, 42, 0.12)'

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
    if (data.data.length === 0) {
      return null
    }
    const tooltipContent = [
      ...(kind === 'key'
        ? [
            {
              key: t('API Key'),
              value: (datum: Record<string, unknown>) =>
                String(datum.tokenName || ''),
            },
          ]
        : []),
      ...(data.data.some((item) => item.username)
        ? [
            {
              key: t('User'),
              value: (datum: Record<string, unknown>) =>
                String(datum.username || '-'),
            },
          ]
        : []),
      {
        key: kind === 'tag' ? t('Key Tag') : t('Tags'),
        value: (datum: Record<string, unknown>) => String(datum.tagName || ''),
      },
      {
        key: t('Model'),
        value: (datum: Record<string, unknown>) =>
          String(datum.modelName || ''),
      },
      {
        key: t('Current model value'),
        value: (datum: Record<string, unknown>) =>
          formatMetric(Number(datum.value) || 0),
      },
      {
        key: t('Total'),
        value: (datum: Record<string, unknown>) =>
          formatMetric(Number(datum.total) || 0),
      },
      {
        key: t('Share'),
        value: (datum: Record<string, unknown>) =>
          `${((Number(datum.share) || 0) * 100).toFixed(1)}%`,
      },
    ]

    return {
      type: 'bar' as const,
      data: [{ id: 'token-tag-ranking', values: data.data }],
      xField: 'label',
      yField: 'value',
      seriesField: 'modelName',
      stack: true,
      color: { specified: Object.fromEntries(colorMap) },
      legends: {
        visible: true,
        orient: 'bottom' as const,
        position: 'middle' as const,
      },
      axes: [
        {
          orient: 'bottom' as const,
          label: {
            style: { fill: chartTextColor, fontSize: 10 },
            autoHide: false,
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
              String(datum.label || ''),
          },
          content: tooltipContent,
        },
      },
      animationAppear: { duration: 400 },
    }
  }, [
    chartGridColor,
    chartTextColor,
    colorMap,
    data.data,
    formatMetric,
    kind,
    t,
  ])

  if (!themeReady || !spec) {
    return (
      <div className='text-muted-foreground flex h-72 items-center justify-center text-sm'>
        {t('No data')}
      </div>
    )
  }

  const chartWidth = Math.max(720, data.categories.length * 96)
  return (
    <div className='overflow-x-auto pb-2'>
      <div className='h-[360px]' style={{ width: chartWidth }}>
        <VChart
          key={`${kind}-${metric}-${resolvedTheme}`}
          spec={{
            ...spec,
            theme: resolvedTheme === 'dark' ? 'dark' : 'light',
            background: 'transparent',
          }}
          option={VCHART_OPTION}
        />
      </div>
    </div>
  )
}
