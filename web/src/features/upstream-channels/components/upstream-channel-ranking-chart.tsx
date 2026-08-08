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

import { getChartColor } from '@/lib/colors'
import { formatNumber, formatQuota } from '@/lib/format'
import { useChartTheme } from '@/lib/use-chart-theme'
import { VCHART_OPTION } from '@/lib/vchart'

import type {
  UpstreamChannelRankingData,
  UpstreamChannelStatisticsMetric,
} from '../statistics-lib'

interface UpstreamChannelRankingChartProps {
  data: UpstreamChannelRankingData
  metric: UpstreamChannelStatisticsMetric
}

export function UpstreamChannelRankingChart({
  data,
  metric,
}: UpstreamChannelRankingChartProps) {
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
    (value: number) =>
      metric === 'quota' ? formatQuota(value) : formatNumber(value),
    [metric]
  )

  const spec = useMemo(() => {
    if (data.data.length === 0) return null
    const colorMap = new Map(
      data.data.map((row, index) => [row.key, getChartColor(index)])
    )
    return {
      type: 'bar' as const,
      data: [{ id: 'upstream-channel-ranking', values: data.data }],
      xField: 'label',
      yField: 'value',
      seriesField: 'key',
      color: { specified: Object.fromEntries(colorMap) },
      legends: { visible: false },
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
          content: [
            {
              key: t('Provider'),
              value: (datum: Record<string, unknown>) =>
                String(datum.provider || '-'),
            },
            {
              key: t('Base URL'),
              value: (datum: Record<string, unknown>) =>
                String(datum.baseUrl || '-'),
            },
            {
              key: t('Value'),
              value: (datum: Record<string, unknown>) =>
                formatMetric(Number(datum.value) || 0),
            },
            {
              key: t('Share'),
              value: (datum: Record<string, unknown>) =>
                `${((Number(datum.share) || 0) * 100).toFixed(1)}%`,
            },
          ],
        },
      },
      animationAppear: { duration: 400 },
    }
  }, [chartGridColor, chartTextColor, data.data, formatMetric, t])

  if (!themeReady || !spec) {
    return (
      <div className='text-muted-foreground flex h-72 items-center justify-center text-sm'>
        {t('No data')}
      </div>
    )
  }

  const chartWidth = Math.max(720, data.data.length * 96)
  return (
    <div className='overflow-x-auto pb-2'>
      <div className='h-[360px]' style={{ width: chartWidth }}>
        <VChart
          key={`upstream-channel-ranking-${metric}-${resolvedTheme}`}
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
