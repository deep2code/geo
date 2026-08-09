import React, { useRef, useState, useEffect } from 'react'
import './SparklineLine.scss'

export type SparklineVariant = 'default' | 'success' | 'warning' | 'error' | 'info'

export interface SparklineLineProps {
  data: number[]
  width?: number
  height?: number
  variant?: SparklineVariant
  showArea?: boolean
  showEndDot?: boolean
  showTooltip?: boolean
  formatValue?: (value: number) => string
  className?: string
}

export const SparklineLine: React.FC<SparklineLineProps> = ({
  data,
  width,
  height = 40,
  variant = 'default',
  showArea = true,
  showEndDot = true,
  showTooltip = true,
  formatValue = (v) => String(v),
  className
}) => {
  const containerRef = useRef<HTMLDivElement>(null)
  const [dimensions, setDimensions] = useState({ width: width || 200, height })
  const [hoveredIndex, setHoveredIndex] = useState<number | null>(null)
  const [tooltipPos, setTooltipPos] = useState({ x: 0, y: 0 })

  useEffect(() => {
    if (!containerRef.current || width) return
    const observer = new ResizeObserver((entries) => {
      for (const entry of entries) {
        setDimensions({
          width: entry.contentRect.width,
          height
        })
      }
    })
    observer.observe(containerRef.current)
    return () => observer.disconnect()
  }, [width, height])

  const gradientId = `sparkline-gradient-${React.useId()}`

  const classes = [
    'sparkline-line',
    variant !== 'default' ? `sparkline-${variant}` : '',
    className || ''
  ].filter(Boolean).join(' ')

  if (data.length === 0) {
    return <div ref={containerRef} className={classes} style={{ height }} />
  }

  const { width: w, height: h } = dimensions
  const padding = 4
  const innerWidth = w - padding * 2
  const innerHeight = h - padding * 2

  const minVal = Math.min(...data)
  const maxVal = Math.max(...data)
  const range = maxVal - minVal || 1

  const points = data.map((value, index) => {
    const x = padding + (index / Math.max(data.length - 1, 1)) * innerWidth
    const y = padding + innerHeight - ((value - minVal) / range) * innerHeight
    return { x, y, value }
  })

  const linePath = points
    .map((p, i) => `${i === 0 ? 'M' : 'L'} ${p.x.toFixed(2)} ${p.y.toFixed(2)}`)
    .join(' ')

  const areaPath = linePath
    + ` L ${points[points.length - 1].x.toFixed(2)} ${(padding + innerHeight).toFixed(2)}`
    + ` L ${points[0].x.toFixed(2)} ${(padding + innerHeight).toFixed(2)} Z`

  const endPoint = points[points.length - 1]

  const handleMouseMove = (e: React.MouseEvent) => {
    if (!showTooltip || !containerRef.current) return
    const rect = containerRef.current.getBoundingClientRect()
    const x = e.clientX - rect.left
    const ratio = Math.max(0, Math.min(1, (x - padding) / innerWidth))
    const index = Math.round(ratio * (data.length - 1))
    setHoveredIndex(index)
    setTooltipPos({
      x: points[index].x,
      y: points[index].y - 8
    })
  }

  const handleMouseLeave = () => {
    setHoveredIndex(null)
  }

  return (
    <div
      ref={containerRef}
      className={classes}
      style={{ height }}
      onMouseMove={handleMouseMove}
      onMouseLeave={handleMouseLeave}
    >
      <svg viewBox={`0 0 ${w} ${h}`} preserveAspectRatio="none">
        <defs>
          <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" />
            <stop offset="100%" stopOpacity="0" />
          </linearGradient>
        </defs>
        {showArea && (
          <path
            className="sparkline-area"
            d={areaPath}
            style={{ fill: `url(#${gradientId})` }}
          />
        )}
        <path className="sparkline-line" d={linePath} />
        {showEndDot && (
          <circle
            className="sparkline-dot"
            cx={endPoint.x}
            cy={endPoint.y}
            r={3.5}
          />
        )}
        {hoveredIndex != null && (
          <circle
            className="sparkline-dot"
            cx={points[hoveredIndex].x}
            cy={points[hoveredIndex].y}
            r={5}
          />
        )}
      </svg>
      {showTooltip && hoveredIndex != null && (
        <div
          className={`sparkline-line-tooltip ${hoveredIndex != null ? 'sparkline-line-tooltip-visible' : ''}`}
          style={{
            left: tooltipPos.x,
            top: tooltipPos.y,
            transform: 'translate(-50%, -100%)'
          }}
        >
          {formatValue(data[hoveredIndex])}
        </div>
      )}
    </div>
  )
}

export default SparklineLine
