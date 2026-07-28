import { useEffect, useRef } from 'react'

/**
 * Auto-selects the first item from `items` when no item is selected.
 * Eliminates the useRef+useEffect boilerplate duplicated across scope views.
 */
export function useAutoSelectFirst<T>(items: T[], selected: T | null, isLoading: boolean, onSelect: (item: T) => void) {
  const selectRef = useRef(onSelect)
  selectRef.current = onSelect

  useEffect(() => {
    if (items.length > 0 && !selected && !isLoading) {
      selectRef.current(items[0])
    }
  }, [items, selected, isLoading])
}
