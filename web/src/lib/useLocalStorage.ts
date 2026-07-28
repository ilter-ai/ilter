import { useCallback, useEffect, useState } from 'react'
import { logger } from './logger'

export function useLocalStorage<T>(key: string | undefined, initial: T): [T, (v: T) => void] {
  const [value, setValue] = useState<T>(() => {
    if (!key) return initial
    try {
      const raw = localStorage.getItem(key)
      return raw !== null ? (JSON.parse(raw) as T) : initial
    } catch (err) {
      // JSON.parse only throws SyntaxError
      if (err instanceof SyntaxError) {
        return initial
      } else {
        logger.error('LocalStorage read error', err)
        return initial
      }
    }
  })

  useEffect(() => {
    if (!key) return
    try {
      localStorage.setItem(key, JSON.stringify(value))
    } catch (err) {
      // quota / private mode
      logger.error('LocalStorage write error', err)
    }
  }, [key, value])

  const set = useCallback((v: T) => setValue(v), [])
  return [value, set]
}
