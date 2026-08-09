import React from 'react'
import './Kpi.scss'
import { SparklineLine } from '@/components/SparklineLine'

export type KpiVariant = 'default' | 'success' | 'warning' | 'error' | 'info'
export type TrendDirection = 'up' | 'down' | 'neutral'

export interface KpiProps {
  label: string
  value: string | number
  prefix?: string
  suffix?: string
  icon?: React.ReactNode
  trendValue?: string | number
  trendDirection?: TrendDirection
  sparklineData?: number[]
  footer?: React.ReactNode
  variant?: KpiVariant
  className?: string
  onClick?: () => void
}

export const Kpi: React.FC<KpiProps> = ({
  label,
  value,
  prefix,
  suffix,
  icon,
  trendValue,
  trendDirection = 'neutral',
  sparklineData,
  footer,
  variant = 'default',
  className,
  onClick
}) => {
  const classes = [
    'kpi',
    variant !== 'default' ? `kpi-variant-${variant}` : '',
    className || ''
  ].filter(Boolean).join(' ')

  const trendClass = `kpi-trend-${trendDirection}`
  const trendIcon = trendDirection === 'up' ? '↑' : trendDirection === 'down' ? '↓' : '→'

  return (
    <div className={classes} onClick={onClick} role={onClick ? 'button' : undefined}>
      <div className="kpi-header">
        <span className="kpi-label">{label}</span>
        {icon && <div className="kpi-icon">{icon}</div>}
      </div>
      <div className="kpi-value">
        {prefix && <span className="kpi-value-prefix">{prefix}</span>}
        {value}
        {suffix && <span className="kpi-value-suffix">{suffix}</span>}
      </div>
      {trendValue != null && (
        <span className={`kpi-trend ${trendClass}`}>
          {trendIcon} {trendValue}
        </span>
      )}
      {sparklineData && sparklineData.length > 0 && (
        <div className="kpi-sparkline">
          <SparklineLine data={sparklineData} />
        </div>
      )}
      {footer && <div className="kpi-footer">{footer}</div>}
    </div>
  )
}

export default Kpi
