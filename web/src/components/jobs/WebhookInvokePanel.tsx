import { useEffect, useState } from 'react'
import { Button } from '@/components/ui/button'
import { CodeSnippet, OSTabbedCodeSnippet } from '@/components/ui/CodeSnippet'
import { X } from '@/components/ui/icons'
import { Skeleton } from '@/components/ui/skeleton'
import { revealTrigger } from '@/lib/api'

// Mirrors the slide-in "How To Connect" panel pattern used on the MCP
// servers page: fixed backdrop + right-hand drawer with a copyable setup
// guide. Shown persistently from a trigger's "How to Invoke" button — not
// just once right after creation — since credentials can be re-revealed
// anytime via the /reveal endpoint.
export function WebhookInvokePanel({
  jobId,
  triggerId,
  initialToken,
  initialSecret,
  onClose,
}: {
  jobId: string
  triggerId: string
  initialToken?: string
  initialSecret?: string
  onClose: () => void
}) {
  const [token, setToken] = useState(initialToken ?? '')
  const [secret, setSecret] = useState(initialSecret ?? '')
  const [loading, setLoading] = useState(!initialToken || !initialSecret)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (initialToken && initialSecret) return
    let cancelled = false
    setLoading(true)
    setError(null)
    revealTrigger(jobId, triggerId)
      .then((creds) => {
        if (cancelled) return
        setToken(creds.token)
        setSecret(creds.secret)
      })
      .catch(() => {
        if (!cancelled) setError('Failed to load webhook credentials')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [jobId, triggerId, initialToken, initialSecret])

  const webhookUrl = token ? `${window.location.origin}/api/webhooks/${token}` : ''
  const bodyExample = '{"example":"payload"}'

  const curlExample = `TOKEN="${token}"
SECRET="${secret}"
BODY='${bodyExample}'
SIG=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "$SECRET" | sed 's/^.* //')

curl -X POST "${webhookUrl}" \\
  -H "Content-Type: application/json" \\
  -H "X-Signature-256: $SIG" \\
  -d "$BODY"`

  const powershellExample = `$token = "${token}"
$secret = "${secret}"
$body = '${bodyExample}'

$hmac = New-Object System.Security.Cryptography.HMACSHA256
$hmac.Key = [Text.Encoding]::UTF8.GetBytes($secret)
$sig = [BitConverter]::ToString($hmac.ComputeHash([Text.Encoding]::UTF8.GetBytes($body))).Replace('-', '').ToLower()

Invoke-RestMethod -Uri "${webhookUrl}" -Method Post -Body $body -ContentType "application/json" \`
  -Headers @{ "X-Signature-256" = $sig }`

  return (
    <>
      <div className="fixed inset-0 z-50 bg-black/30 backdrop-blur-sm" onClick={onClose} />
      <div className="fixed right-0 top-0 z-50 flex h-full w-[520px] max-w-full flex-col border-l border-surface-200 bg-white shadow-2xl">
        <div className="flex items-center justify-between border-b border-surface-200 px-6 py-5">
          <h3 className="text-lg font-semibold text-surface-900">How to Invoke</h3>
          <Button variant="ghost" size="icon-sm" onClick={onClose} aria-label="Close">
            <X size={16} />
          </Button>
        </div>

        <div className="flex-1 overflow-y-auto px-6 py-6">
          {loading ? (
            <div className="space-y-3">
              <Skeleton className="h-16 rounded-lg" />
              <Skeleton className="h-16 rounded-lg" />
              <Skeleton className="h-40 rounded-lg" />
            </div>
          ) : error ? (
            <p className="text-sm text-error">{error}</p>
          ) : (
            <div className="space-y-6">
              <p className="text-sm leading-relaxed text-surface-600">
                Send a POST request to the URL below to trigger this job. Sign the raw request body with HMAC-SHA256
                using the secret as the key, hex-encode the digest, and send it as the{' '}
                <code className="bg-surface-100 px-1 rounded">X-Signature-256</code> header.
              </p>

              <div>
                <label className="mb-1.5 block text-sm font-medium text-surface-700">Webhook URL</label>
                <CodeSnippet code={webhookUrl} />
              </div>

              <div>
                <label className="mb-1.5 block text-sm font-medium text-surface-700">Signing Secret</label>
                <CodeSnippet code={secret} />
              </div>

              <div>
                <label className="mb-1.5 block text-sm font-medium text-surface-700">Example Request</label>
                <OSTabbedCodeSnippet bash={curlExample} powershell={powershellExample} />
              </div>
            </div>
          )}
        </div>
      </div>
    </>
  )
}
