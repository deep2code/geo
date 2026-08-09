import React from 'react'
import './Button.scss'

export type ButtonVariant = 'primary' | 'secondary' | 'outline' | 'ghost' | 'danger' | 'success'
export type ButtonSize = 'xs' | 'sm' | 'md' | 'lg' | 'icon' | 'icon-sm'

export interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant
  size?: ButtonSize
  loading?: boolean
  icon?: React.ReactNode
  iconPosition?: 'left' | 'right'
}

export const Button: React.FC<ButtonProps> = ({
  variant = 'primary',
  size = 'md',
  loading = false,
  icon,
  iconPosition = 'left',
  children,
  disabled,
  className,
  ...props
}) => {
  const classes = [
    'button',
    `button-${variant}`,
    `button-${size}`,
    loading ? 'button-is-loading' : '',
    className || ''
  ].filter(Boolean).join(' ')

  return (
    <button
      className={classes}
      disabled={disabled || loading}
      {...props}
    >
      {loading && <span className="button-loading" aria-hidden="true" />}
      {!loading && icon && iconPosition === 'left' && <span className="button-icon">{icon}</span>}
      {children && <span className="button-content">{children}</span>}
      {!loading && icon && iconPosition === 'right' && <span className="button-icon">{icon}</span>}
    </button>
  )
}

export default Button
