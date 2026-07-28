import { useEffect, useState } from 'react'

export function usePathNavigation() {
  const [path, setPath] = useState(() => window.location.pathname)

  useEffect(() => {
    const onPop = () => setPath(window.location.pathname)
    window.addEventListener('popstate', onPop)
    return () => window.removeEventListener('popstate', onPop)
  }, [])

  const navigate = (p: string) => {
    window.history.pushState(null, '', p)
    setPath(p)
  }

  return { path, navigate }
}
