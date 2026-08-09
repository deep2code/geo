import React from 'react'
import './Input.scss'

export type InputSize = 'sm' | 'md' | 'lg'
export type InputVariant = 'default' | 'error' | 'success'

export interface InputProps extends Omit<React.InputHTMLAttributes<HTMLInputElement>, 'prefix' | 'size' | 'style'> {
  label?: string
  hint?: string
  error?: string
  inputSize?: InputSize
  variant?: InputVariant
  isSearch?: boolean
  required?: boolean
  prefix?: React.ReactNode
  suffix?: React.ReactNode
  style?: React.CSSProperties
}

export const Input: React.FC<InputProps> = ({
  label,
  hint,
  error,
  inputSize = 'md',
  variant = 'default',
  isSearch = false,
  required,
  prefix,
  suffix,
  className,
  id,
  style,
  ...props
}) => {
  const inputId = id || `input-${Math.random().toString(36).slice(2, 9)}`
  const hasError = variant === 'error' || !!error

  const inputClasses = [
    'input',
    `input-${inputSize}`,
    hasError ? 'input-error' : '',
    variant === 'success' ? 'input-success' : '',
    isSearch ? 'input-search' : '',
    prefix ? 'input-has-prefix' : '',
    suffix ? 'input-has-suffix' : '',
    className || ''
  ].filter(Boolean).join(' ')

  return (
    <div className="input-wrapper" style={style}>
      {label && (
        <label htmlFor={inputId} className="input-wrapper-label">
          {label}
          {required && <span className="input-wrapper-required">*</span>}
        </label>
      )}
      <div className="input-box" style={{ display: 'flex', alignItems: 'center', position: 'relative' }}>
        {prefix && <div className="input-prefix" style={{ padding: '0 10px', color: 'var(--text-secondary)', flexShrink: 0 }}>{prefix}</div>}
        <input
          id={inputId}
          className={inputClasses}
          style={{ flex: 1, minWidth: 0 }}
          aria-invalid={hasError || undefined}
          aria-describedby={error ? `${inputId}-error` : hint ? `${inputId}-hint` : undefined}
          {...props}
        />
        {suffix && <div className="input-suffix" style={{ padding: '0 10px', color: 'var(--text-secondary)', flexShrink: 0, whiteSpace: 'nowrap' }}>{suffix}</div>}
      </div>
      {hint && !error && (
        <div id={`${inputId}-hint`} className="input-wrapper-hint">{hint}</div>
      )}
      {error && (
        <div id={`${inputId}-error`} className="input-wrapper-error" role="alert">{error}</div>
      )}
    </div>
  )
}

export interface TextareaProps extends React.TextareaHTMLAttributes<HTMLTextAreaElement> {
  label?: string
  hint?: string
  error?: string
  required?: boolean
}

export const Textarea: React.FC<TextareaProps> = ({
  label,
  hint,
  error,
  required,
  className,
  id,
  ...props
}) => {
  const textareaId = id || `textarea-${Math.random().toString(36).slice(2, 9)}`
  const hasError = !!error

  const textareaClasses = [
    'textarea',
    hasError ? 'textarea-error' : '',
    className || ''
  ].filter(Boolean).join(' ')

  return (
    <div className="input-wrapper">
      {label && (
        <label htmlFor={textareaId} className="input-wrapper-label">
          {label}
          {required && <span className="input-wrapper-required">*</span>}
        </label>
      )}
      <textarea
        id={textareaId}
        className={textareaClasses}
        aria-invalid={hasError || undefined}
        aria-describedby={error ? `${textareaId}-error` : hint ? `${textareaId}-hint` : undefined}
        {...props}
      />
      {hint && !error && (
        <div id={`${textareaId}-hint`} className="input-wrapper-hint">{hint}</div>
      )}
      {error && (
        <div id={`${textareaId}-error`} className="input-wrapper-error" role="alert">{error}</div>
      )}
    </div>
  )
}

export default Input
