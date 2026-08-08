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

import type { TokenTagDistributionData, TokenTagRankingMetric } from '../lib'

interface DistributionChartProps {
  data: TokenTagDistributionData
  metric: TokenTagRankingMetric
  kind: 'tag' | 'key'
}

export function DistributionChart({
  data,
  metric,
  kind,
}: DistributionChartProps) {
  const { t } = useTranslation()
  const { resolvedTheme, themeReady } = useChartTheme()
  const chartTextColor =
    resolvedTheme === 'dark'
      ? 'rgba(255, 255, 255, 0.78)'
      : 'rgba(15, 23, 42, 0.72)'
  const chartLineColor =
    resolvedTheme === 'dark'
      ? 'rgba(255, 255, 255, 0.32)'
      : 'rgba(15, 23, 42, 0.24)'

  const formatMetric = useCallback(
    (value: number) => {
      if (metric === 'quota') {
        return formatQuota(value)
      }
      return formatNumber(value)
    },
    [metric]
  )

  const formatShare = useCallback(
    (value: unknown) => `${((Number(value) || 0) * 100).toFixed(1)}%`,
    []
  )

  const spec = useMemo(() => {
    if (data.data.length === 0 || data.total <= 0) {
      return null
    }

    const hasUsername = data.data.some((item) => item.username)
    let metricLabel = t('Request Count')
    if (metric === 'quota') {
      metricLabel = t('Cost Consumption')
    } else if (metric === 'token_used') {
      metricLabel = t('Token Count')
    }

    return {
      type: 'pie' as const,
      data: [{ id: 'token-tag-distribution', values: data.data }],
      categoryField: 'label',
      valueField: 'value',
      outerRadius: 0.72,
      innerRadius: 0,
      padAngle: 0.5,
      legends: {
        visible: true,
        orient: 'bottom' as const,
        position: 'middle' as const,
        item: {
          label: {
            style: { fill: chartTextColor },
          },
        },
      },
      label: {
        visible: true,
        position: 'outside' as const,
        formatMethod: (
          _text: string | string[],
          datum?: Record<string, unknown>
        ) => formatShare(datum?.share),
        style: {
          fill: chartTextColor,
          fontSize: 12,
          fontWeight: 500,
        },
        line: {
          visible: true,
          style: { stroke: chartLineColor },
        },
      },
      tooltip: {
        mark: {
          title: {
            value: (datum: Record<string, unknown>) =>
              String(datum.label || ''),
          },
          content: [
            {
              key: kind === 'tag' ? t('Key Tag') : t('API Key'),
              value: (datum: Record<string, unknown>) =>
                String(
                  (kind === 'tag' ? datum.tagName : datum.tokenName) || '-'
                ),
            },
            ...(hasUsername
              ? [
                  {
                    key: t('User'),
                    value: (datum: Record<string, unknown>) =>
                      String(datum.username || '-'),
                  },
                ]
              : []),
            {
              key: metricLabel,
              value: (datum: Record<string, unknown>) =>
                formatMetric(Number(datum.value) || 0),
            },
            {
              key: t('Share'),
              value: (datum: Record<string, unknown>) =>
                formatShare(datum.share),
            },
          ],
        },
      },
      animationAppear: { duration: 400 },
    }
  }, [
    chartLineColor,
    chartTextColor,
    data.data,
    data.total,
    formatMetric,
    formatShare,
    kind,
    metric,
    t,
  ])

  if (!themeReady || !spec) {
    return (
      <div className='text-muted-foreground flex h-72 items-center justify-center text-sm'>
        {t('No data')}
      </div>
    )
  }

  return (
    <div className='h-[360px]'>
      <VChart
        key={`token-tag-distribution-${kind}-${resolvedTheme}`}
        spec={{
          ...spec,
          theme: resolvedTheme === 'dark' ? 'dark' : 'light',
          background: 'transparent',
        }}
        option={VCHART_OPTION}
      />
    </div>
  )
}
