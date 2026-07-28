import { useCallback, useState } from 'react'
import type { TimeRange } from './logsTime'

function getUrlParam(key: string): string {
  if (typeof window === 'undefined') return ''
  return new URLSearchParams(window.location.search).get(key) || ''
}

function updateUrlParam(key: string, value: string) {
  const url = new URL(window.location.href)
  if (value) {
    url.searchParams.set(key, value)
  } else {
    url.searchParams.delete(key)
  }
  window.history.replaceState({}, '', url)
}

export function useLogsPageState() {
  const [timeRange, setTimeRange] = useState<TimeRange>('24h')
  const [customFrom, setCustomFrom] = useState(getUrlParam('start'))
  const [customTo, setCustomTo] = useState(getUrlParam('end'))
  const [polling, setPolling] = useState(true)

  const handleTimeRange = useCallback((range: TimeRange) => {
    setTimeRange(range)
    if (range !== 'custom') {
      setCustomFrom('')
      setCustomTo('')
      updateUrlParam('start', '')
      updateUrlParam('end', '')
    }
  }, [])

  const handleCustomFrom = useCallback((val: string) => {
    setCustomFrom(val)
    updateUrlParam('start', val)
  }, [])

  const handleCustomTo = useCallback((val: string) => {
    setCustomTo(val)
    updateUrlParam('end', val)
  }, [])

  const clearTimeRange = useCallback(() => {
    setTimeRange('24h')
    setCustomFrom('')
    setCustomTo('')
    updateUrlParam('start', '')
    updateUrlParam('end', '')
  }, [])

  return {
    timeRange,
    setTimeRange,
    customFrom,
    setCustomFrom,
    customTo,
    setCustomTo,
    polling,
    setPolling,
    handleTimeRange,
    handleCustomFrom,
    handleCustomTo,
    clearTimeRange,
  }
}
