import React, { useState, useMemo } from 'react'
import './Table.scss'
import { Button } from '@/components/Button'

export interface TableColumn<T> {
  key: string
  title: React.ReactNode
  dataIndex?: keyof T
  render?: (record: T, index: number) => React.ReactNode
  width?: string | number
  align?: 'left' | 'center' | 'right'
  sortable?: boolean
}

export interface TableProps<T> {
  columns: TableColumn<T>[]
  dataSource: T[]
  rowKey?: keyof T | ((record: T) => string)
  loading?: boolean
  emptyText?: React.ReactNode
  striped?: boolean
  pagination?: boolean
  pageSize?: number
  onRowClick?: (record: T, index: number) => void
  className?: string
}

export function Table<T extends Record<string, any>>({
  columns,
  dataSource,
  rowKey = 'id' as keyof T,
  loading = false,
  emptyText,
  striped = false,
  pagination = false,
  pageSize = 10,
  onRowClick,
  className
}: TableProps<T>) {
  const [currentPage, setCurrentPage] = useState(1)
  const [sortKey, setSortKey] = useState<string | null>(null)
  const [sortOrder, setSortOrder] = useState<'asc' | 'desc'>('asc')

  const getRowKey = (record: T, index: number): string => {
    if (typeof rowKey === 'function') return rowKey(record)
    return String(record[rowKey] ?? index)
  }

  const sortedData = useMemo(() => {
    if (!sortKey) return dataSource
    const sorted = [...dataSource].sort((a, b) => {
      const aVal = a[sortKey]
      const bVal = b[sortKey]
      if (aVal == null && bVal == null) return 0
      if (aVal == null) return 1
      if (bVal == null) return -1
      if (typeof aVal === 'number' && typeof bVal === 'number') {
        return sortOrder === 'asc' ? aVal - bVal : bVal - aVal
      }
      const aStr = String(aVal)
      const bStr = String(bVal)
      return sortOrder === 'asc' ? aStr.localeCompare(bStr) : bStr.localeCompare(aStr)
    })
    return sorted
  }, [dataSource, sortKey, sortOrder])

  const pagedData = useMemo(() => {
    if (!pagination) return sortedData
    const start = (currentPage - 1) * pageSize
    return sortedData.slice(start, start + pageSize)
  }, [sortedData, pagination, currentPage, pageSize])

  const totalPages = pagination ? Math.ceil(dataSource.length / pageSize) : 1

  const handleSort = (key: string) => {
    if (sortKey === key) {
      setSortOrder(prev => prev === 'asc' ? 'desc' : 'asc')
    } else {
      setSortKey(key)
      setSortOrder('asc')
    }
  }

  const classes = [
    'table-wrapper',
    loading ? 'table-loading' : '',
    className || ''
  ].filter(Boolean).join(' ')

  return (
    <div className={classes}>
      <table className="table">
        <thead className="table-thead">
          <tr>
            {columns.map(col => (
              <th
                key={col.key}
                className={[
                  'table-th',
                  col.sortable ? 'table-th-sortable' : '',
                  sortKey === col.key ? 'table-th-sorted' : ''
                ].filter(Boolean).join(' ')}
                style={{
                  width: col.width,
                  textAlign: col.align
                }}
                onClick={() => col.sortable && handleSort(col.key)}
              >
                {col.title}
                {col.sortable && (
                  <span className="sort-icon">
                    {sortKey === col.key ? (sortOrder === 'asc' ? '↑' : '↓') : '↕'}
                  </span>
                )}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="table-tbody">
          {pagedData.length === 0 && !loading ? (
            <tr>
              <td colSpan={columns.length} className="table-td table-empty">
                {emptyText || '暂无数据'}
              </td>
            </tr>
          ) : (
            pagedData.map((record, index) => (
              <tr
                key={getRowKey(record, index)}
                className={[
                  'table-tr',
                  striped ? 'table-tr-striped' : '',
                  onRowClick ? 'table-tr-clickable' : ''
                ].filter(Boolean).join(' ')}
                onClick={() => onRowClick?.(record, index)}
              >
                {columns.map(col => (
                  <td
                    key={col.key}
                    className="table-td"
                    style={{ textAlign: col.align }}
                  >
                    {col.render
                      ? col.render(record, index)
                      : col.dataIndex
                        ? record[col.dataIndex] as React.ReactNode
                        : null}
                  </td>
                ))}
              </tr>
            ))
          )}
        </tbody>
      </table>
      {pagination && dataSource.length > 0 && (
        <div className="table-pagination">
          <div className="table-pagination-info">
            共 {dataSource.length} 条，第 {currentPage}/{totalPages || 1} 页
          </div>
          <div className="table-pagination-controls">
            <Button
              size="sm"
              variant="secondary"
              disabled={currentPage <= 1}
              onClick={() => setCurrentPage(p => Math.max(1, p - 1))}
            >
              上一页
            </Button>
            <Button
              size="sm"
              variant="secondary"
              disabled={currentPage >= totalPages}
              onClick={() => setCurrentPage(p => Math.min(totalPages, p + 1))}
            >
              下一页
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}

export default Table
