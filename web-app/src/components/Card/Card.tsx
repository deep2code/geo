import React from 'react'
import './Card.scss'

export interface CardProps {
  title?: React.ReactNode
  subtitle?: React.ReactNode
  actions?: React.ReactNode
  children?: React.ReactNode
  footer?: React.ReactNode
  clickable?: boolean
  compact?: boolean
  elevated?: boolean
  gradient?: boolean
  variant?: string
  className?: string
  style?: React.CSSProperties
  id?: string
  role?: React.AriaRole
  onClick?: () => void
  onMouseEnter?: React.MouseEventHandler<HTMLDivElement>
  onMouseLeave?: React.MouseEventHandler<HTMLDivElement>
  'aria-label'?: string
}

export const Card: React.FC<CardProps> = ({
  title,
  subtitle,
  actions,
  children,
  footer,
  clickable = false,
  compact = false,
  elevated = false,
  gradient = false,
  variant,
  className,
  style,
  id,
  role,
  onClick,
  onMouseEnter,
  onMouseLeave,
  'aria-label': ariaLabel,
}) => {
  const classes = [
    'card',
    clickable ? 'card-clickable' : '',
    compact ? 'card-compact' : '',
    elevated ? 'card-elevated' : '',
    gradient ? 'card-gradient' : '',
    className || ''
  ].filter(Boolean).join(' ')

  const classesFull = [
    ...classes,
    variant ? `card-variant-${variant}` : ''
  ].filter(Boolean).join(' ')
  return (
    <div
      id={id}
      className={classesFull}
      style={style}
      onClick={onClick}
      onMouseEnter={onMouseEnter}
      onMouseLeave={onMouseLeave}
      role={role ?? (clickable ? 'button' : undefined)}
      aria-label={ariaLabel}
    >
      {(title || actions) && (
        <div className="card-header">
          <div className="card-header-text">
            {title && <h3 className="card-title">{title}</h3>}
            {subtitle && <div className="card-subtitle">{subtitle}</div>}
          </div>
          {actions && <div className="card-actions">{actions}</div>}
        </div>
      )}
      {children && <div className="card-body">{children}</div>}
      {footer && <div className="card-footer">{footer}</div>}
    </div>
  )
}

export default Card
