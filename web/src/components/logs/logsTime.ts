export type TimeRange = '1h' | '24h' | '7d' | 'custom'

export function timeRangeToParams(range: TimeRange, customFrom: string, _customTo: string) {
  const now = new Date()
  let from: string | undefined
  const to = now.toISOString()
  switch (range) {
    case '1h':
      from = new Date(now.getTime() - 60 * 60 * 1000).toISOString()
      break
    case '24h':
      from = new Date(now.getTime() - 24 * 60 * 60 * 1000).toISOString()
      break
    case '7d':
      from = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000).toISOString()
      break
    case 'custom':
      from = customFrom ? new Date(customFrom).toISOString() : undefined
      break
  }
  return { from, to }
}
