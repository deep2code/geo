import React, { useEffect } from 'react'
import { createPortal } from 'react-dom'
import './Modal.scss'

export type ModalSize = 'sm' | 'md' | 'lg' | 'xl'

export interface ModalProps {
  open: boolean
  title?: React.ReactNode
  description?: React.ReactNode
  children?: React.ReactNode
  footer?: React.ReactNode
  onClose: () => void
  size?: ModalSize
  closeOnBackdropClick?: boolean
  closeOnEsc?: boolean
  showCloseButton?: boolean
  className?: string
}

export const Modal: React.FC<ModalProps> = ({
  open,
  title,
  description,
  children,
  footer,
  onClose,
  size = 'md',
  closeOnBackdropClick = true,
  closeOnEsc = true,
  showCloseButton = true,
  className
}) => {
  useEffect(() => {
    if (!open || !closeOnEsc) return

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [open, closeOnEsc, onClose])

  useEffect(() => {
    if (open) {
      document.body.style.overflow = 'hidden'
    } else {
      document.body.style.overflow = ''
    }
    return () => { document.body.style.overflow = '' }
  }, [open])

  if (!open) return null

  const modalClasses = [
    'modal',
    `modal-${size}`,
    className || ''
  ].filter(Boolean).join(' ')

  return createPortal(
    <>
      <div
        className="modal-backdrop"
        onClick={() => closeOnBackdropClick && onClose()}
      />
      <div
        className={modalClasses}
        role="dialog"
        aria-modal="true"
        aria-labelledby={title ? 'modal-title' : undefined}
        onClick={(e) => e.stopPropagation()}
      >
        {(title || showCloseButton) && (
          <div className="modal-header">
            <div>
              {title && (
                <h2 id="modal-title" className="modal-title">{title}</h2>
              )}
              {description && (
                <div className="modal-description">{description}</div>
              )}
            </div>
            {showCloseButton && (
              <button
                type="button"
                className="modal-close"
                onClick={onClose}
                aria-label="Close"
              >
                ×
              </button>
            )}
          </div>
        )}
        {children && <div className="modal-body">{children}</div>}
        {footer && <div className="modal-footer">{footer}</div>}
      </div>
    </>,
    document.body
  )
}

export default Modal
