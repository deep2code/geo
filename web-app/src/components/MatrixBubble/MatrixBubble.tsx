import React, { useRef, useState, useEffect } from 'react'
import './MatrixBubble.scss'

export interface MatrixBubbleDatum {
  id: string
  label: string
  x: number
  y: number
  size: number
  color?: string
  category?: string
  value?: number
  meta?: Record<string, any>
}

export interface MatrixBubbleProps {
  data: MatrixBubbleDatum[]
  xLabel?: string
  yLabel?: string
  xMax?: number
  yMax?: number
  quadrantMode?: boolean
  showLabels?: boolean
  showValues?: boolean
  formatValue?: (value: number) => string
  onBubbleClick?: (datum: MatrixBubbleDatum) => void
  className?: string
}

const defaultColors = [
  '#6366f1', '#8b5cf6', '#ec4899', '#f59e0b', '#10b981',
  '#3b82f6', '#14b8a6', '#f43f5e', '#8b5cf6', '#0ea5e9'
]

export const MatrixBubble: React.FC<MatrixBubbleProps> = ({
  data,
  xLabel = 'X 轴',
  yLabel = 'Y 轴',
  xMax,
  yMax,
  quadrantMode = true,
  showLabels = true,
  showValues = true,
  formatValue = (v) => v.toFixed(1),
  onBubbleClick,
  className
}) => {
  const containerRef = useRef<HTMLDivElement>(null)
  const [dimensions, setDimensions] = useState({ width: 600, height: 400 })
  const [hoveredId, setHoveredId] = useState<string | null>(null)
  const [tooltipPos, setTooltipPos] = useState({ x: 0, y: 0 })

  useEffect(() => {
    if (!containerRef.current) return
    const observer = new ResizeObserver((entries) => {
      for (const entry of entries) {
        setDimensions({
          width: entry.contentRect.width,
          height: Math.max(300, entry.contentRect.height)
        })
      }
    })
    observer.observe(containerRef.current)
    return () => observer.disconnect()
  }, [])

  const { width, height } = dimensions
  const padding = { top: 30, right: 30, bottom: 50, left: 60 }
  const innerW = width - padding.left - padding.right
  const innerH = height - padding.top - padding.bottom

  const calculatedXMax = xMax ?? Math.max(...data.map(d => d.x), 100)
  const calculatedYMax = yMax ?? Math.max(...data.map(d => d.y), 100)
  const maxSize = Math.max(...data.map(d => d.size), 1)

  const categorySet = new Set(data.map(d => d.category).filter(Boolean) as string[])
  const categoryColors: Record<string, string> = {}
  Array.from(categorySet).forEach((cat, i) => {
    categoryColors[cat] = defaultColors[i % defaultColors.length]
  })

  const scaleX = (x: number) => padding.left + (x / calculatedXMax) * innerW
  const scaleY = (y: number) => padding.top + innerH - (y / calculatedYMax) * innerH
  const scaleSize = (s: number) => {
    const minR = 6
    const maxR = 40
    return minR + Math.sqrt(s / maxSize) * (maxR - minR)
  }

  const midX = padding.left + innerW / 2
  const midY = padding.top + innerH / 2

  const classes = [
    'matrix-bubble',
    className || ''
  ].filter(Boolean).join(' ')

  const getColor = (d: MatrixBubbleDatum) => {
    if (d.color) return d.color
    if (d.category && categoryColors[d.category]) return categoryColors[d.category]
    return defaultColors[data.indexOf(d) % defaultColors.length]
  }

  const handleBubbleHover = (e: React.MouseEvent, d: MatrixBubbleDatum) => {
    if (!containerRef.current) return
    const rect = containerRef.current.getBoundingClientRect()
    setHoveredId(d.id)
    setTooltipPos({
      x: e.clientX - rect.left + 10,
      y: e.clientY - rect.top - 10
    })
  }

  return (
    <div
      ref={containerRef}
      className={classes}
      onMouseLeave={() => setHoveredId(null)}
    >
      <svg viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="xMidYMid meet">
        {quadrantMode && (
          <>
            <rect
              x={midX} y={padding.top}
              width={innerW / 2} height={innerH / 2}
              className="matrix-bubble-quadrant matrix-bubble-quadrant-top-right"
            />
            <rect
              x={padding.left} y={padding.top}
              width={innerW / 2} height={innerH / 2}
              className="matrix-bubble-quadrant matrix-bubble-quadrant-top-left"
            />
            <rect
              x={midX} y={midY}
              width={innerW / 2} height={innerH / 2}
              className="matrix-bubble-quadrant matrix-bubble-quadrant-bottom-right"
            />
            <rect
              x={padding.left} y={midY}
              width={innerW / 2} height={innerH / 2}
              className="matrix-bubble-quadrant matrix-bubble-quadrant-bottom-left"
            />
          </>
        )}
        {[0.25, 0.5, 0.75].map((ratio, i) => (
          <g key={`grid-${i}`}>
            <line
              x1={padding.left}
              y1={padding.top + innerH * ratio}
              x2={padding.left + innerW}
              y2={padding.top + innerH * ratio}
              className="matrix-bubble-grid-line"
            />
            <line
              x1={padding.left + innerW * ratio}
              y1={padding.top}
              x2={padding.left + innerW * ratio}
              y2={padding.top + innerH}
              className="matrix-bubble-grid-line"
            />
          </g>
        ))}
        {quadrantMode && (
          <>
            <line
              x1={midX} y1={padding.top}
              x2={midX} y2={padding.top + innerH}
              className="matrix-bubble-axis-line"
            />
            <line
              x1={padding.left} y1={midY}
              x2={padding.left + innerW} y2={midY}
              className="matrix-bubble-axis-line"
            />
          </>
        )}
        <line
          x1={padding.left}
          y1={padding.top + innerH}
          x2={padding.left + innerW}
          y2={padding.top + innerH}
          className="matrix-bubble-axis-line"
        />
        <line
          x1={padding.left}
          y1={padding.top}
          x2={padding.left}
          y2={padding.top + innerH}
          className="matrix-bubble-axis-line"
        />
        <text
          x={padding.left + innerW / 2}
          y={height - 12}
          textAnchor="middle"
          className="matrix-bubble-axis"
        >
          {xLabel}
        </text>
        <text
          x={12}
          y={padding.top + innerH / 2}
          textAnchor="middle"
          transform={`rotate(-90, 12, ${padding.top + innerH / 2})`}
          className="matrix-bubble-axis"
        >
          {yLabel}
        </text>
        {[0, 0.5, 1].map((t, i) => (
          <g key={`tick-x-${i}`}>
            <text
              x={padding.left + innerW * t}
              y={padding.top + innerH + 18}
              textAnchor="middle"
              className="matrix-bubble-axis"
            >
              {Math.round(calculatedXMax * t)}
            </text>
            <text
              x={padding.left - 8}
              y={padding.top + innerH * (1 - t) + 4}
              textAnchor="end"
              className="matrix-bubble-axis"
            >
              {Math.round(calculatedYMax * t)}
            </text>
          </g>
        ))}
        {data.map((d) => {
          const cx = scaleX(d.x)
          const cy = scaleY(d.y)
          const r = scaleSize(d.size)
          const color = getColor(d)
          const isHovered = hoveredId === d.id
          return (
            <g
              key={d.id}
              className="matrix-bubble-bubble"
              onClick={() => onBubbleClick?.(d)}
              onMouseEnter={(e) => handleBubbleHover(e, d)}
              onMouseMove={(e) => handleBubbleHover(e, d)}
              transform={`translate(${cx}, ${cy}) scale(${isHovered ? 1.08 : 1})`}
              style={{ transformOrigin: `${cx}px ${cy}px` }}
            >
              <circle
                className="matrix-bubble-circle"
                r={r}
                fill={color}
                stroke={color}
              />
              {showLabels && r > 18 && (
                <text className="matrix-bubble-label" y={-r / 4}>
                  {d.label.length > 8 ? d.label.slice(0, 8) + '…' : d.label}
                </text>
              )}
              {showValues && r > 22 && (
                <text className="matrix-bubble-value" y={r / 3}>
                  {formatValue(d.value ?? d.size)}
                </text>
              )}
            </g>
          )
        })}
      </svg>
      {hoveredId && (() => {
        const datum = data.find(d => d.id === hoveredId)
        if (!datum) return null
        return (
          <div
            className={`matrix-bubble-tooltip ${hoveredId ? 'matrix-bubble-tooltip-visible' : ''}`}
            style={{ left: tooltipPos.x, top: tooltipPos.y, transform: 'translate(0, -100%)' }}
          >
            <div className="matrix-bubble-tooltip-title">{datum.label}</div>
            <div className="matrix-bubble-tooltip-row">
              <span className="matrix-bubble-tooltip-label">{xLabel}:</span>
              <span>{formatValue(datum.x)}</span>
            </div>
            <div className="matrix-bubble-tooltip-row">
              <span className="matrix-bubble-tooltip-label">{yLabel}:</span>
              <span>{formatValue(datum.y)}</span>
            </div>
            <div className="matrix-bubble-tooltip-row">
              <span className="matrix-bubble-tooltip-label">量级:</span>
              <span>{formatValue(datum.size)}</span>
            </div>
            {datum.category && (
              <div className="matrix-bubble-tooltip-row">
                <span className="matrix-bubble-tooltip-label">分类:</span>
                <span>{datum.category}</span>
              </div>
            )}
          </div>
        )
      })()}
    </div>
  )
}

export default MatrixBubble
