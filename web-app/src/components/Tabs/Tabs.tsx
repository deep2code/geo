import React, { useState, Children, isValidElement } from 'react'
import './Tabs.scss'

export interface TabPaneProps {
  tabKey: string
  tab: React.ReactNode
  badge?: React.ReactNode
  disabled?: boolean
  children?: React.ReactNode
}

export const TabPane: React.FC<TabPaneProps> = ({ children }) => {
  return <>{children}</>
}

export type TabsVariant = 'underline' | 'pills'
export type TabsSize = 'sm' | 'md' | 'lg'

export interface TabsProps {
  activeKey?: string
  defaultActiveKey?: string
  onChange?: (key: string) => void
  variant?: TabsVariant
  size?: TabsSize
  children: React.ReactNode
  className?: string
}

export const Tabs: React.FC<TabsProps> = ({
  activeKey: controlledKey,
  defaultActiveKey,
  onChange,
  variant = 'underline',
  size = 'md',
  children,
  className
}) => {
  const [internalKey, setInternalKey] = useState<string>(() => {
    if (defaultActiveKey) return defaultActiveKey
    const childArray = Children.toArray(children).filter(isValidElement) as React.ReactElement<TabPaneProps>[]
    return childArray[0]?.props.tabKey ?? ''
  })

  const activeKey = controlledKey ?? internalKey

  const handleTabClick = (key: string) => {
    if (controlledKey == null) {
      setInternalKey(key)
    }
    onChange?.(key)
  }

  const childArray = Children.toArray(children).filter(isValidElement) as React.ReactElement<TabPaneProps>[]

  const classes = [
    'tabs',
    `tabs-variant-${variant}`,
    `tabs-size-${size}`,
    className || ''
  ].filter(Boolean).join(' ')

  return (
    <div className={classes}>
      <div className="tabs-nav" role="tablist">
        {childArray.map(child => {
          const { tabKey, tab, badge, disabled } = child.props
          const isActive = activeKey === tabKey
          return (
            <button
              key={tabKey}
              role="tab"
              type="button"
              className={[
                'tabs-tab',
                isActive ? 'tabs-tab-active' : '',
                disabled ? 'tabs-tab-disabled' : ''
              ].filter(Boolean).join(' ')}
              aria-selected={isActive}
              disabled={disabled}
              onClick={() => !disabled && handleTabClick(tabKey)}
            >
              {tab}
              {badge != null && badge !== false && <span className="tabs-tab-badge">{badge}</span>}
            </button>
          )
        })}
      </div>
      <div className="tabs-content" role="tabpanel">
        {childArray.map(child => {
          if (child.props.tabKey !== activeKey) return null
          return (
            <div key={child.props.tabKey} className="tabs-pane">
              {child.props.children}
            </div>
          )
        })}
      </div>
    </div>
  )
}

export default Tabs
