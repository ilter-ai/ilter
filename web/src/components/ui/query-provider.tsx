import { QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { queryClient } from '../../lib/queryClient'

/**
 * Wraps a React island with the shared QueryClientProvider.
 *
 * Usage in Astro pages:
 *   <QueryProvider client:only="react">
 *     <LatencyView />
 *   </QueryProvider>
 */
export function QueryProvider({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
}
