import { useCallback } from 'react'
import { logger } from '../../lib/logger'

function escapeCsvValue(value: unknown): string {
  const str = value == null ? '' : String(value)
  if (str.includes(',') || str.includes('"') || str.includes('\n') || str.includes('\r')) {
    return `"${str.replace(/"/g, '""')}"`
  }
  return str
}

function triggerDownload(content: string, filename: string, mimeType: string) {
  const blob = new Blob([content], { type: mimeType })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}

export function exportToJson<T>(data: T[], filename = 'export.json') {
  const json = JSON.stringify(data, null, 2)
  triggerDownload(json, filename, 'application/json')
}

export function exportToCsv<T extends Record<string, unknown>>(
  data: T[],
  columns: { key: keyof T; header: string }[],
  filename = 'export.csv',
) {
  if (data.length === 0) {
    triggerDownload('', filename, 'text/csv')
    return
  }

  const headerRow = columns.map((c) => escapeCsvValue(c.header)).join(',')
  const dataRows = data.map((item) => columns.map((c) => escapeCsvValue(item[c.key])).join(','))

  const csv = [headerRow, ...dataRows].join('\n')
  triggerDownload(csv, filename, 'text/csv')
}

export function useExport() {
  const exportJson = useCallback(<T>(data: T[], filename?: string) => {
    exportToJson(data, filename)
  }, [])

  const exportCsv = useCallback(
    <T extends Record<string, unknown>>(data: T[], columns: { key: keyof T; header: string }[], filename?: string) => {
      exportToCsv(data, columns, filename)
    },
    [],
  )

  const exportCopy = useCallback(async <T>(data: T[], format: 'json' | 'csv' = 'json') => {
    const text = format === 'json' ? JSON.stringify(data, null, 2) : ''
    try {
      await navigator.clipboard.writeText(text)
    } catch (err) {
      // Clipboard API only throws DOMException on permission errors
      logger.error('Copy error', err instanceof DOMException ? 'Clipboard permission error' : err)
    }
  }, [])

  return { exportJson, exportCsv, exportCopy }
}
