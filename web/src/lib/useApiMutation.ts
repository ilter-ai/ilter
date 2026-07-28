import { type QueryKey, useMutation, useQueryClient } from '@tanstack/react-query'

/**
 * Standardised mutation helper that invalidates related query keys on **settle**
 * (both success and error) so the cache resyncs with server truth after every write.
 *
 * Usage:
 * ```ts
 * const create = useApiMutation(api.apiKeys.createAPIKey, {
 *   invalidate: [qk.apiKeys],
 *   onDone: () => setShowForm(false),
 * });
 * await create.mutateAsync({ name: 'foo' });
 * // create.isPending → button loading state
 * ```
 */
export function useApiMutation<TArgs, TData>(
  fn: (args: TArgs) => Promise<TData>,
  opts: {
    /** Query keys to invalidate after the mutation settles (success or error). */
    invalidate?: readonly (readonly unknown[])[]
    /** Called after success (e.g. close a modal, clear a form). */
    onDone?: () => void
    /** Called with the error if the mutation fails. */
    onError?: (err: Error) => void
  } = {},
) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: fn,
    onSettled: () => {
      if (opts.invalidate) {
        for (const key of opts.invalidate) {
          qc.invalidateQueries({ queryKey: key as QueryKey })
        }
      }
    },
    onSuccess: () => {
      opts.onDone?.()
    },
    onError: (err: Error) => {
      opts.onError?.(err)
    },
  })
}
