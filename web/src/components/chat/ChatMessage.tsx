import { useDeferredValue, useEffect, useMemo, useRef, useState } from 'react'
import Markdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { cn } from '../../lib/utils'
import { Bot, Check, ChevronDown, ChevronRight, Copy, Loader2, RefreshCw, User as UserIcon } from '../ui/icons'

export interface Message {
  role: 'user' | 'assistant'
  content: string
  reasoningContent?: string
  model?: string
  timestamp?: number
  toolCalls?: ToolCallEvent[]
  usageCost?: number
  billingKey?: string
}

export interface ToolCallEvent {
  id: string
  name: string
  args: string
  status: 'calling' | 'completed' | 'failed'
  result?: string
}

interface ChatMessageProps {
  message: Message
  isLast?: boolean
  isStreaming?: boolean
  onRegenerate?: () => void
}

const mdComponents = {
  pre: (props: React.ComponentPropsWithoutRef<'pre'>) => (
    <pre
      className="overflow-x-auto rounded-lg bg-surface-900 p-3 text-xs leading-relaxed my-1 text-surface-50"
      {...props}
    />
  ),
  code: ({ className, ...props }: React.ComponentPropsWithoutRef<'code'>) =>
    className ? (
      <code className={className} {...props} />
    ) : (
      <code className="rounded bg-surface-200 px-1 py-0.5 text-[13px] font-mono text-surface-800" {...props} />
    ),
  strong: (props: React.ComponentPropsWithoutRef<'strong'>) => <strong className="font-semibold" {...props} />,
  a: (props: React.ComponentPropsWithoutRef<'a'>) => (
    <a
      target="_blank"
      rel="noopener noreferrer"
      className="text-brand-600 underline underline-offset-2 hover:text-brand-700"
      {...props}
    />
  ),
  p: (props: React.ComponentPropsWithoutRef<'p'>) => <p className="mb-2 last:mb-0" {...props} />,
  table: (props: React.ComponentPropsWithoutRef<'table'>) => (
    <div className="overflow-x-auto my-2">
      <table className="w-full text-xs border-collapse border border-surface-300" {...props} />
    </div>
  ),
  th: (props: React.ComponentPropsWithoutRef<'th'>) => (
    <th className="border border-surface-300 bg-surface-100 px-3 py-1.5 font-semibold text-left" {...props} />
  ),
  td: (props: React.ComponentPropsWithoutRef<'td'>) => (
    <td className="border border-surface-300 px-3 py-1.5" {...props} />
  ),
}

// Marker constants — must match tool_injection.go
const TOOL_MARKER_RE = /\ue000ilter:tool:(\d+)\ue001/g

/** Strip private-use markers from text (belt-and-suspenders for API-bound text). */
export function stripMarkers(text: string): string {
  return text.replace(TOOL_MARKER_RE, '')
}

/** Strip raw XML markup that may have leaked past the backend stripper. */
export function stripRawXML(text: string): string {
  const withoutToolCalls = text.replace(/<tool_calls[\s\S]*?<\/tool_calls>/g, '')
  const withoutInvoke = withoutToolCalls.replace(/<invoke[\s\S]*?<\/invoke>/g, '')
  // Hermes/Qwen-style single tool_call tag (distinct dialect from the
  // <tool_calls><invoke> convention above — some models emit this even when
  // the actual call is already delivered via the native tool_calls SSE event).
  const withoutToolCall = withoutInvoke.replace(/<tool_call>[\s\S]*?<\/tool_call>/g, '')
  const withoutOrphans = withoutToolCall.replace(/<\/?(?:tool_calls|tool_call|invoke|parameter)[^>]*>/g, '')
  return withoutOrphans.trim()
}

function ChatMarkdown({ content }: { content: string }) {
  const deferred = useDeferredValue(content)
  const clean = useMemo(() => stripRawXML(deferred), [deferred])
  return (
    <Markdown remarkPlugins={[remarkGfm]} components={mdComponents}>
      {clean}
    </Markdown>
  )
}

export function ThinkingBlock({ content, isStreaming }: { content: string; isStreaming?: boolean }) {
  const [expanded, setExpanded] = useState(false)
  const scrollRef = useRef<HTMLDivElement>(null)
  const autoScrollRef = useRef(true)

  useEffect(() => {
    if (expanded && autoScrollRef.current && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [expanded])

  const handleScroll = () => {
    if (!scrollRef.current) return
    const { scrollTop, scrollHeight, clientHeight } = scrollRef.current
    autoScrollRef.current = scrollHeight - scrollTop - clientHeight < 30
  }

  if (!content && !isStreaming) return null

  return (
    <div className="mb-2">
      {expanded ? (
        <>
          <button
            onClick={() => setExpanded(false)}
            className="flex items-center gap-1 text-[11px] font-medium text-amber-600/70 hover:text-amber-700 mb-1"
          >
            <ChevronDown size={11} />
            Thinking
            {isStreaming && <Loader2 size={10} className="animate-spin ml-0.5" />}
          </button>
          <div
            ref={scrollRef}
            onScroll={handleScroll}
            className="max-h-36 overflow-y-auto rounded-lg bg-amber-50/60 border border-amber-200/60 p-2.5 text-[11px] leading-relaxed text-amber-800 font-mono whitespace-pre-wrap"
          >
            {content || (isStreaming ? '…' : '')}
            {isStreaming && (
              <span className="inline-block w-1 h-3 bg-amber-500 animate-pulse ml-0.5 align-text-bottom" />
            )}
          </div>
        </>
      ) : (
        <button
          onClick={() => setExpanded(true)}
          className="flex items-center gap-1 text-[11px] text-amber-600/60 hover:text-amber-700 transition-colors"
        >
          {isStreaming ? <Loader2 size={10} className="animate-spin" /> : <ChevronRight size={11} />}
          {isStreaming ? 'Thinking…' : 'Show thinking'}
        </button>
      )}
    </div>
  )
}

const RESULT_ERROR_TAG_RE = /<error>([\s\S]*?)<\/error>/g

/** Splits tool result content on <error>...</error> tags (e.g. the fetch
 * tool's truncation notice) so they render as a flagged callout instead of
 * blending into the rest of the result as plain/markdown text. */
function useResultParts(content: string) {
  return useMemo(() => {
    const parts: Array<{ type: 'text' | 'error'; value: string }> = []
    let lastIndex = 0
    let match: RegExpExecArray | null
    RESULT_ERROR_TAG_RE.lastIndex = 0
    while ((match = RESULT_ERROR_TAG_RE.exec(content)) !== null) {
      if (match.index > lastIndex) {
        parts.push({ type: 'text', value: content.slice(lastIndex, match.index) })
      }
      parts.push({ type: 'error', value: match[1].trim() })
      lastIndex = match.index + match[0].length
    }
    if (lastIndex < content.length) {
      parts.push({ type: 'text', value: content.slice(lastIndex) })
    }
    return parts.length > 0 ? parts : [{ type: 'text' as const, value: content }]
  }, [content])
}

/** Reformats a raw tool result for display and decides whether it should be
 * rendered as markdown (plain text results) or as pretty-printed JSON
 * (structured results) — the two need different rendering, so the decision
 * lives next to the reformatting that depends on it. */
function useFormattedResult(content: string) {
  return useMemo(() => {
    if (!content) return { display: '', isMarkdown: false }
    try {
      const parsed = JSON.parse(content)
      // Unwrap OpenAI-style content array: [{"type":"text","text":"..."}]
      if (Array.isArray(parsed) && parsed.length > 0 && parsed[0]?.text !== undefined) {
        let unwrapped = parsed.map((p: { text?: string }) => p.text || '').join('\n')
        try {
          unwrapped = JSON.stringify(JSON.parse(unwrapped), null, 2)
          return { display: unwrapped, isMarkdown: false }
        } catch {
          // not JSON after unwrap — render as markdown/plain text instead
          return { display: unwrapped, isMarkdown: true }
        }
      }
      return { display: JSON.stringify(parsed, null, 2), isMarkdown: false }
    } catch {
      // Plain text / markdown result — unescape literal \n and \" for readability
      return { display: content.replace(/\\n/g, '\n').replace(/\\"/g, '"'), isMarkdown: true }
    }
  }, [content])
}

/** Renders a tool call's raw result: detects markdown vs. JSON and formats
 * accordingly, flagging any <error>...</error> tags as callouts. */
function ToolResultBody({ content }: { content: string }) {
  const { display, isMarkdown } = useFormattedResult(content)
  const parts = useResultParts(display)

  const body = parts.map((p, i) => {
    if (p.type === 'error') {
      return (
        <div
          key={i}
          className="flex items-start gap-1.5 my-1.5 rounded-md border border-red-200 bg-red-50 px-2 py-1.5 font-sans text-red-700"
        >
          <span className="shrink-0">⚠️</span>
          <span>{p.value}</span>
        </div>
      )
    }
    if (!p.value.trim()) return null
    return isMarkdown ? (
      <ChatMarkdown key={i} content={p.value} />
    ) : (
      <span key={i} className="whitespace-pre-wrap">
        {p.value}
      </span>
    )
  })

  return isMarkdown ? (
    <div className="font-sans [&_pre]:font-mono [&_code]:font-mono">{body}</div>
  ) : (
    <div className="whitespace-pre-wrap break-words font-mono">{body}</div>
  )
}

export function ToolCallCard({ call }: { call: ToolCallEvent }) {
  const running = call.status === 'calling'
  const failed = call.status === 'failed'
  const [expanded, setExpanded] = useState(false)
  const [copiedSection, setCopiedSection] = useState<'args' | 'result' | null>(null)

  let firstParamLabel = ''
  let fullParamsFormatted = ''

  try {
    let parsed: Record<string, unknown> = {}
    if (typeof call.args === 'string' && call.args.trim()) {
      parsed = JSON.parse(call.args)
    } else if (typeof call.args === 'object' && call.args !== null) {
      parsed = call.args as Record<string, unknown>
    }
    const entries = Object.entries(parsed)
    if (entries.length > 0) {
      const [k, v] = entries[0]
      const valStr = typeof v === 'string' ? `"${v}"` : JSON.stringify(v)
      firstParamLabel = `${k}: ${valStr}`
      if (entries.length > 1) {
        firstParamLabel += `, +${entries.length - 1} more`
      }
      fullParamsFormatted = JSON.stringify(parsed, null, 2)
    }
  } catch {
    firstParamLabel = typeof call.args === 'string' ? call.args : ''
  }

  const shortLabel = firstParamLabel ? `${call.name}(${firstParamLabel})` : `${call.name}()`
  const rawResult = call.result || ''
  const hasDetails = Boolean(fullParamsFormatted || rawResult || running || failed)

  const copyToClipboard = async (text: string, section: 'args' | 'result', e: React.MouseEvent) => {
    e.stopPropagation()
    try {
      await navigator.clipboard.writeText(text)
      setCopiedSection(section)
      setTimeout(() => setCopiedSection(null), 2000)
    } catch (err) {
      console.warn('Failed to copy to clipboard:', err)
    }
  }

  return (
    <div
      className={cn(
        'rounded-xl px-3 py-2 text-xs font-mono text-surface-700 border my-1.5 shadow-sm transition-all',
        failed
          ? 'bg-red-50/90 border-red-200'
          : running
            ? 'bg-blue-50/80 border-blue-200'
            : 'bg-amber-50/70 border-amber-200/80',
      )}
    >
      <div
        className={cn('flex items-center gap-2 select-none', hasDetails && 'cursor-pointer')}
        onClick={() => hasDetails && setExpanded(!expanded)}
      >
        <span className="shrink-0">{failed ? '❌' : running ? '🔄' : '✅'}</span>
        <span className="flex-1 truncate font-medium text-surface-900">{shortLabel}</span>
        {hasDetails && (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation()
              setExpanded(!expanded)
            }}
            className="shrink-0 text-brand-600 hover:text-brand-700 font-semibold underline underline-offset-2 cursor-pointer transition-colors px-2 py-0.5 rounded bg-brand-50 hover:bg-brand-100"
          >
            {expanded ? 'Hide' : 'Show'}
          </button>
        )}
      </div>

      {expanded && hasDetails && (
        <div className="mt-2.5 pt-2 border-t border-surface-200/60 space-y-3">
          {fullParamsFormatted && (
            <div>
              <div className="flex items-center justify-between mb-1">
                <span className="text-[11px] font-sans font-medium text-surface-500">Arguments</span>
                <button
                  type="button"
                  onClick={(e) => copyToClipboard(fullParamsFormatted, 'args', e)}
                  className="text-[10px] font-sans text-surface-400 hover:text-surface-700 underline cursor-pointer"
                >
                  {copiedSection === 'args' ? 'Copied' : 'Copy'}
                </button>
              </div>
              <pre className="whitespace-pre-wrap break-words text-surface-800 leading-relaxed max-h-36 overflow-y-auto rounded-md bg-white/90 p-2 border border-surface-200 font-mono text-[11px]">
                {fullParamsFormatted}
              </pre>
            </div>
          )}
          {rawResult ? (
            <div>
              <div className="flex items-center justify-between mb-1">
                <span className="text-[11px] font-sans font-medium text-surface-500">Result</span>
                <button
                  type="button"
                  onClick={(e) => copyToClipboard(rawResult, 'result', e)}
                  className="text-[10px] font-sans text-surface-400 hover:text-surface-700 underline cursor-pointer"
                >
                  {copiedSection === 'result' ? 'Copied' : 'Copy'}
                </button>
              </div>
              <div className="text-surface-800 leading-relaxed max-h-48 overflow-y-auto rounded-md bg-white/90 p-2 border border-surface-200 text-[11px]">
                <ToolResultBody content={rawResult} />
              </div>
            </div>
          ) : (
            <div>
              <div className="flex items-center justify-between mb-1">
                <span className="text-[11px] font-sans font-medium text-surface-500">Result</span>
              </div>
              <div className="text-[11px] text-surface-500 italic font-sans p-2 bg-white/70 rounded-md border border-surface-200 font-mono">
                {running ? 'Running tool...' : 'No output returned.'}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

/**
 * Split content on \ue000ilter:tool:N\ue001 markers into an ordered parts array.
 * Each part is either { t: 'text', v: string } or { t: 'tool', i: number }.
 * Messages stored before markers existed are rendered as a single text part.
 */
function useContentParts(content: string, toolCalls?: ToolCallEvent[]) {
  return useMemo(() => {
    const parts: Array<{ type: 'text' | 'tool'; value?: string; index?: number }> = []
    let last = 0
    let match: RegExpExecArray | null
    TOOL_MARKER_RE.lastIndex = 0
    while ((match = TOOL_MARKER_RE.exec(content)) !== null) {
      if (match.index > last) {
        parts.push({ type: 'text', value: content.slice(last, match.index) })
      }
      parts.push({ type: 'tool', index: parseInt(match[1], 10) })
      last = match.index + match[0].length
    }
    if (last < content.length) {
      parts.push({ type: 'text', value: content.slice(last) })
    }
    // Fallback: if no markers in content but toolCalls exist, render them after text
    if (toolCalls && toolCalls.length > 0 && !parts.some((p) => p.type === 'tool')) {
      for (let i = 0; i < toolCalls.length; i++) {
        parts.push({ type: 'tool', index: i })
      }
    }
    return parts.length > 0 ? parts : [{ type: 'text' as const, value: content }]
  }, [content, toolCalls])
}

export function ChatMessage({ message, isLast, isStreaming, onRegenerate }: ChatMessageProps) {
  const [copied, setCopied] = useState(false)
  const parts = useContentParts(message.content, message.toolCalls)

  const handleCopy = async () => {
    try {
      // Strip markers before copying
      await navigator.clipboard.writeText(stripMarkers(message.content))
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch (e) {
      console.warn('Failed to copy message to clipboard:', e)
    }
  }

  return (
    <div className={cn('flex gap-2.5 group', message.role === 'user' ? 'flex-row-reverse' : 'flex-row')}>
      <div
        className={cn(
          'flex shrink-0 items-center justify-center w-7 h-7 rounded-full mt-0.5',
          message.role === 'assistant' ? 'bg-brand-100 text-brand-600' : 'bg-surface-200 text-surface-500',
        )}
      >
        {message.role === 'assistant' ? <Bot size={14} /> : <UserIcon size={14} />}
      </div>

      <div className={cn('flex flex-col max-w-[75%]', message.role === 'user' ? 'items-end' : 'items-start')}>
        {message.role === 'assistant' && message.model && (
          <span className="text-[11px] text-surface-400 font-medium mb-0.5 px-1">{message.model}</span>
        )}

        {message.role === 'assistant' && message.reasoningContent && (
          <ThinkingBlock content={message.reasoningContent} />
        )}

        {/* Inline parts: text and tool calls interleaved where markers appear */}
        {(message.content ||
          message.reasoningContent ||
          message.role === 'user' ||
          (message.role === 'assistant' && message.toolCalls && message.toolCalls.length > 0)) && (
          <div
            className={cn(
              'rounded-2xl px-3.5 py-2.5 text-sm leading-relaxed whitespace-pre-wrap break-words',
              message.role === 'user'
                ? 'bg-brand-600 text-white rounded-br-md'
                : 'bg-surface-100 text-surface-900 rounded-bl-md border border-surface-200',
            )}
          >
            {message.role === 'assistant' && parts.length > 0 ? (
              <div className="flex flex-col gap-1.5">
                {parts.map((part, i) =>
                  part.type === 'tool' ? (
                    message.toolCalls?.[part.index!] ? (
                      <ToolCallCard key={`tool-${i}`} call={message.toolCalls[part.index!]} />
                    ) : null
                  ) : (
                    <ChatMarkdown key={`text-${i}`} content={part.value || ''} />
                  ),
                )}
                {isStreaming && (
                  <span className="inline-flex items-center gap-0.5 ml-0.5 align-text-bottom h-4">
                    <span
                      className="w-1.5 h-1.5 rounded-full bg-brand-500"
                      style={{ animation: 'typing-dot 1.4s ease-in-out 0s infinite both' }}
                    />
                    <span
                      className="w-1.5 h-1.5 rounded-full bg-brand-500"
                      style={{ animation: 'typing-dot 1.4s ease-in-out 0.2s infinite both' }}
                    />
                    <span
                      className="w-1.5 h-1.5 rounded-full bg-brand-500"
                      style={{ animation: 'typing-dot 1.4s ease-in-out 0.4s infinite both' }}
                    />
                  </span>
                )}
              </div>
            ) : message.role === 'assistant' ? (
              <>
                <ChatMarkdown content={message.content} />
                {isStreaming && (
                  <span className="inline-flex items-center gap-0.5 ml-0.5 align-text-bottom h-4">
                    <span
                      className="w-1.5 h-1.5 rounded-full bg-brand-500"
                      style={{ animation: 'typing-dot 1.4s ease-in-out 0s infinite both' }}
                    />
                    <span
                      className="w-1.5 h-1.5 rounded-full bg-brand-500"
                      style={{ animation: 'typing-dot 1.4s ease-in-out 0.2s infinite both' }}
                    />
                    <span
                      className="w-1.5 h-1.5 rounded-full bg-brand-500"
                      style={{ animation: 'typing-dot 1.4s ease-in-out 0.4s infinite both' }}
                    />
                  </span>
                )}
              </>
            ) : (
              <div className="whitespace-pre-wrap">{message.content}</div>
            )}
          </div>
        )}

        {message.role === 'assistant' && (message.usageCost !== undefined || message.billingKey) && !isStreaming && (
          <div className="flex items-center gap-2 mt-0.5 px-1">
            {message.usageCost !== undefined && (
              <span className="text-[11px] text-surface-400">${message.usageCost.toFixed(6)}</span>
            )}
            {message.billingKey && (
              <span className="text-[11px] text-surface-400">
                billed to: <code className="text-[10px] bg-surface-200 px-1 rounded">{message.billingKey}</code>
              </span>
            )}
          </div>
        )}

        <div
          className={cn(
            'flex items-center gap-1 mt-0.5 opacity-0 group-hover:opacity-100 transition-opacity',
            message.role === 'user' ? 'flex-row-reverse' : 'flex-row',
          )}
        >
          {message.role === 'assistant' && message.content && (
            <>
              <button
                onClick={handleCopy}
                className="p-1 rounded-md text-surface-400 hover:text-surface-600 hover:bg-surface-100 transition-colors"
                title="Copy message"
              >
                {copied ? <Check size={12} /> : <Copy size={12} />}
              </button>
              {isLast && !isStreaming && onRegenerate && (
                <button
                  onClick={onRegenerate}
                  className="p-1 rounded-md text-surface-400 hover:text-surface-600 hover:bg-surface-100 transition-colors"
                  title="Regenerate"
                >
                  <RefreshCw size={12} />
                </button>
              )}
            </>
          )}
          {message.timestamp && (
            <span className="text-[10px] text-surface-400 px-1">
              {new Date(message.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
            </span>
          )}
        </div>
      </div>
    </div>
  )
}

export function StreamingMessage({
  content,
  thinkingContent,
  model,
  toolEvents,
}: {
  content: string
  thinkingContent?: string
  model?: string
  toolEvents?: ToolCallEvent[]
}) {
  // Split content on markers for inline tool card rendering
  const parts = useMemo(() => {
    if (!toolEvents || toolEvents.length === 0) return null
    const result: Array<{ type: 'text' | 'tool'; value?: string; index?: number }> = []
    let last = 0
    let match: RegExpExecArray | null
    const re = /\ue000ilter:tool:(\d+)\ue001/g
    while ((match = re.exec(content)) !== null) {
      if (match.index > last) result.push({ type: 'text', value: content.slice(last, match.index) })
      result.push({ type: 'tool', index: parseInt(match[1], 10) })
      last = match.index + match[0].length
    }
    if (last < content.length) result.push({ type: 'text', value: content.slice(last) })
    return result.length > 0 ? result : null
  }, [content, toolEvents])

  return (
    <div className="flex gap-2.5 flex-row">
      <div className="flex shrink-0 items-center justify-center w-7 h-7 rounded-full mt-0.5 bg-brand-100 text-brand-600">
        <Bot size={14} />
      </div>
      <div className="flex flex-col max-w-[75%] items-start">
        {model && <span className="text-[11px] text-surface-400 font-medium mb-0.5 px-1">{model}</span>}
        <ThinkingBlock content={thinkingContent || ''} isStreaming={!!thinkingContent && !content} />
        <div
          className={cn(
            'rounded-2xl rounded-bl-md bg-surface-100 text-surface-900 px-3.5 py-2.5 text-sm leading-relaxed break-words border border-surface-200',
            !content && 'min-h-[36px] flex items-center',
          )}
        >
          {parts ? (
            <div className="flex flex-col gap-1.5">
              {parts.map((part, i) =>
                part.type === 'tool' ? (
                  toolEvents?.[part.index!] ? (
                    <ToolCallCard key={`tool-${i}`} call={toolEvents[part.index!]} />
                  ) : null
                ) : (
                  <ChatMarkdown key={`text-${i}`} content={part.value || ''} />
                ),
              )}
            </div>
          ) : content ? (
            <ChatMarkdown content={stripMarkers(content)} />
          ) : null}
          <span className="inline-flex items-center gap-0.5 ml-0.5 align-text-bottom h-4">
            <span
              className="w-1.5 h-1.5 rounded-full bg-brand-500"
              style={{ animation: 'typing-dot 1.4s ease-in-out 0s infinite both' }}
            />
            <span
              className="w-1.5 h-1.5 rounded-full bg-brand-500"
              style={{ animation: 'typing-dot 1.4s ease-in-out 0.2s infinite both' }}
            />
            <span
              className="w-1.5 h-1.5 rounded-full bg-brand-500"
              style={{ animation: 'typing-dot 1.4s ease-in-out 0.4s infinite both' }}
            />
          </span>
        </div>
      </div>
    </div>
  )
}
