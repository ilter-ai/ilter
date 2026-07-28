import { QueryClient } from '@tanstack/react-query'

/**
 * Shared QueryClient instance.
 *
 * With Astro's `client:only="react"` islands, each page is a separate React tree
 * so a module-level singleton is safe (no cross-island cache sharing needed).
 */
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      refetchOnWindowFocus: false,
      retry: 1,
    },
  },
})
