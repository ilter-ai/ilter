import { useEffect, useState } from 'react'
import { OpenAPISpecDetailView } from './OpenAPISpecDetailView'
import { OpenAPISpecsView } from './OpenAPISpecsView'

export function OpenAPIView() {
  const [path, setPath] = useState(() => window.location.pathname)

  useEffect(() => {
    const handler = () => setPath(window.location.pathname)
    window.addEventListener('popstate', handler)
    return () => window.removeEventListener('popstate', handler)
  }, [])

  const navigate = (p: string) => {
    window.history.pushState(null, '', p)
    setPath(p)
  }

  // Match /openapi/specs/:id
  const detailMatch = path.match(/^\/openapi\/specs\/([^/]+)\/?$/)
  if (detailMatch) {
    return <OpenAPISpecDetailView specId={detailMatch[1]} onBack={() => navigate('/openapi/specs')} />
  }

  // Default: list view
  return <OpenAPISpecsView onNavigate={navigate} />
}
