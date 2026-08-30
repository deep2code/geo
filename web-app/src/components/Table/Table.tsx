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
  /** 排序实际取值的字段；缺省用 dataIndex，再退回 key。 */
  sortDataIndex?: keyof T
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
    // 排序取值字段：sortDataIndex > dataIndex > key。部分列的 key 只是展示
    // 标识（如 'sv'/'diff' 或品牌名），直接按 key 取记录字段得到 undefined，
    // sort 恒返回 0，点击表头排序静默失效。
    const col = columns.find(c => c.key === sortKey)
    const field = (col?.sortDataIndex ?? col?.dataIndex ?? sortKey) as keyof T
    const sorted = [...dataSource].sort((a, b) => {
      const aVal = a[field]
      const bVal = b[field]
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
  }, [dataSource, columns, sortKey, sortOrder])

  // dataSource 缩减后当前页可能超出总页数，钳制到有效范围
  // （否则停留在"第 3/1 页"的空页）
  const totalPages = pagination ? Math.max(1, Math.ceil(dataSource.length / pageSize)) : 1
  const effectivePage = Math.min(currentPage, totalPages)

  const pagedData = useMemo(() => {
    if (!pagination) return sortedData
    const start = (effectivePage - 1) * pageSize
    return sortedData.slice(start, start + pageSize)
  }, [sortedData, pagination, effectivePage, pageSize])

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
            共 {dataSource.length} 条，第 {effectivePage}/{totalPages || 1} 页
          </div>
          <div className="table-pagination-controls">
            <Button
              size="sm"
              variant="secondary"
              disabled={effectivePage <= 1}
              onClick={() => setCurrentPage(p => Math.max(1, p - 1))}
            >
              上一页
            </Button>
            <Button
              size="sm"
              variant="secondary"
              disabled={effectivePage >= totalPages}
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
