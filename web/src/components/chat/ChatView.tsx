import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { PromptTemplate } from '../../lib/api'
import { api, type ModelProvider } from '../../lib/api'
import { getAuthHeaders } from '../../lib/auth'
import { logger } from '../../lib/logger'
import { cn } from '../../lib/utils'
import { Button } from '../ui/button'
import {
  AlertTriangle,
  Bot,
  Check,
  ChevronUp,
  Edit3,
  Loader2,
  PanelLeftClose,
  PanelRightOpen,
  Plus,
  Send,
  Settings,
  Square,
  Trash2,
  X,
} from '../ui/icons'
import { QueryProvider } from '../ui/query-provider'
import {
  ChatMessage,
  type Message,
  StreamingMessage,
  stripMarkers,
  stripRawXML,
  type ToolCallEvent,
} from './ChatMessage'
import { ModelSelector } from './ModelSelector'
import { PromptSelector } from './PromptSelector'
import { VariableFillDialog } from './VariableFillDialog'

interface ChatThread {
  id: string
  title: string
  messages: Message[]
  lastActive: number
}

const MODEL_KEY = 'ilter-chat-model'

export function ChatView() {
  const [models, setModels] = useState<ModelProvider[]>([])
  const [loading, setLoading] = useState(true)
  const [sending, setSending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const abortRef = useRef<AbortController | null>(null)
  const streamedContentRef = useRef('')
  const [thinkingContent, setThinkingContent] = useState('')
  const thinkingContentRef = useRef('')
  const toolEventsRef = useRef<ToolCallEvent[]>([])

  const savedModel = localStorage.getItem(MODEL_KEY)
  const [selectedModel, setSelectedModel] = useState(savedModel || '')
  const [systemPrompt, setSystemPrompt] = useState('')
  const [temperature, setTemperature] = useState(0.7)
  const [maxTokens, setMaxTokens] = useState(4096)
  const [topP, setTopP] = useState(0.95)
  const [showParams, setShowParams] = useState(false)
  const [userMessage, setUserMessage] = useState('')

  const [templateForFill, setTemplateForFill] = useState<PromptTemplate | null>(null)

  const [threads, setThreads] = useState<ChatThread[]>([])
  const [activeThreadId, setActiveThreadId] = useState<string | null>(null)

  const [messages, setMessages] = useState<Message[]>([])
  const [streamingContent, setStreamingContent] = useState('')
  const [responseModel, setResponseModel] = useState<string | null>(null)
  const responseModelRef = useRef<string | null>(null)
  const [toolEvents, setToolEvents] = useState<ToolCallEvent[]>([])

  const [sidebarOpen, setSidebarOpen] = useState(true)
  const [editingThreadId, setEditingThreadId] = useState<string | null>(null)
  const [editTitle, setEditTitle] = useState('')

  const messagesEndRef = useRef<HTMLDivElement>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const handleSendRef = useRef(handleSend)
  handleSendRef.current = handleSend

  // Pagination — only load recent messages, lazy-load rest on scroll-up
  const PAGE_SIZE = 4
  const INITIAL_LOAD = 2
  const [visibleCount, setVisibleCount] = useState(INITIAL_LOAD)
  const messagesContainerRef = useRef<HTMLDivElement>(null)
  const prevScrollInfoRef = useRef<{ scrollTop: number; scrollHeight: number } | null>(null)
  const isLoadingOlderRef = useRef(false)

  const visibleMessages = useMemo(
    () => messages.slice(-Math.min(visibleCount, Math.max(messages.length, 1))),
    [messages, visibleCount],
  )
  const hasOlderMessages = messages.length > visibleCount

  const loadOlder = useCallback(() => {
    if (!hasOlderMessages || isLoadingOlderRef.current) return
    isLoadingOlderRef.current = true
    const container = messagesContainerRef.current
    if (container) {
      prevScrollInfoRef.current = {
        scrollTop: container.scrollTop,
        scrollHeight: container.scrollHeight,
      }
    }
    setVisibleCount((prev) => Math.min(prev + PAGE_SIZE, messages.length))
  }, [hasOlderMessages, messages.length])

  const handleScroll = useCallback(() => {
    const el = messagesContainerRef.current
    if (!el || !hasOlderMessages) return
    if (el.scrollTop < 60) {
      loadOlder()
    }
  }, [hasOlderMessages, loadOlder])

  // Restore scroll position after older messages render above viewport
  // biome-ignore lint/correctness/useExhaustiveDependencies: visibleCount triggers re-run after state update, refs are stable
  useEffect(() => {
    if (prevScrollInfoRef.current) {
      const container = messagesContainerRef.current
      if (container) {
        const heightDiff = container.scrollHeight - prevScrollInfoRef.current.scrollHeight
        container.scrollTop = prevScrollInfoRef.current.scrollTop + heightDiff
        prevScrollInfoRef.current = null
      }
      isLoadingOlderRef.current = false
    }
  }, [visibleCount])

  const selectTemplateFromShortcut = (template: PromptTemplate) => {
    if (template.variables && template.variables.length > 0) {
      setTemplateForFill(template)
    } else {
      setUserMessage((prev) => {
        const separator = prev ? '\n\n---\n\n' : ''
        return prev + separator + template.content
      })
    }
  }

  useEffect(() => {
    let cancelled = false
    api.models
      .getModelProviders()
      .then((data) => {
        if (cancelled) return
        const active = data.filter((m) => m.is_active)
        setModels(active)
        if (active.length > 0) {
          setSelectedModel((prev) => {
            if (prev && active.some((m) => m.id === prev)) return prev
            return active.find((m) => m.tier === 'economy')?.id || active[0].id
          })
        }
        setLoading(false)
      })
      .catch(() => {
        if (!cancelled) {
          setError('Failed to load models. Please ensure the backend is running.')
          setLoading(false)
        }
      })
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [])

  // Load threads from API on mount
  useEffect(() => {
    let cancelled = false
    api.chat
      .listThreads()
      .then((data) => {
        if (cancelled) return
        const loaded: ChatThread[] = data.conversations.map((t) => ({
          id: t.id,
          title: t.title,
          messages: [],
          lastActive: new Date(t.updated_at).getTime(),
        }))
        setThreads(loaded)
        if (loaded.length > 0) {
          setActiveThreadId(loaded[0].id)
          api.chat.getThread(loaded[0].id).then((d) => {
            if (!cancelled) {
              setMessages(
                d.messages.map((m) => ({
                  role: m.role as 'user' | 'assistant',
                  content: m.content,
                  reasoningContent: m.reasoning_content || undefined,
                  model: m.model || undefined,
                  timestamp: new Date(m.created_at).getTime(),
                  toolCalls: m.tool_calls ? (JSON.parse(m.tool_calls) as ToolCallEvent[]) : undefined,
                  usageCost: m.usage_cost || undefined,
                  billingKey: m.billing_key || undefined,
                })),
              )
            }
          })
        } else if (loaded.length === 0) {
          setLoading(false)
        }
      })
      .catch(() => {
        setError('Failed to load chat threads')
        setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
        if (!sending && selectedModel && userMessage.trim()) handleSendRef.current()
      }
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [sending, selectedModel, userMessage])

  function createNewThread() {
    setActiveThreadId(null)
    setMessages([])
    setStreamingContent('')
    setThinkingContent('')
    setToolEvents([])
    setError(null)
    setEditingThreadId(null)
    setVisibleCount(INITIAL_LOAD)
    isLoadingOlderRef.current = false
    // Thread not created until first message is sent (handleSend creates it)
  }

  async function switchThread(id: string) {
    if (sending) return
    setActiveThreadId(id)
    setMessages([])
    setStreamingContent('')
    setThinkingContent('')
    setToolEvents([])
    setEditingThreadId(null)
    setVisibleCount(INITIAL_LOAD)
    isLoadingOlderRef.current = false
    try {
      const { messages: msgs } = await api.chat.getThread(id)
      setMessages(
        msgs.map((m) => ({
          role: m.role as 'user' | 'assistant',
          content: m.content,
          reasoningContent: m.reasoning_content || undefined,
          model: m.model || undefined,
          timestamp: new Date(m.created_at).getTime(),
          toolCalls: m.tool_calls ? (JSON.parse(m.tool_calls) as ToolCallEvent[]) : undefined,
          usageCost: m.usage_cost || undefined,
          billingKey: m.billing_key || undefined,
        })),
      )
    } catch {
      setError('Failed to load messages')
      setMessages([])
    }
  }

  async function deleteThread(id: string, e: React.MouseEvent) {
    e.stopPropagation()
    try {
      await api.chat.deleteThread(id)
      setThreads((prev) => {
        const updated = prev.filter((t) => t.id !== id)
        if (activeThreadId === id) {
          if (updated.length > 0) {
            setActiveThreadId(updated[0].id)
            switchThread(updated[0].id)
          } else {
            setActiveThreadId(null)
            setMessages([])
          }
        }
        return updated
      })
    } catch {
      setError('Failed to delete thread')
    }
  }

  function startRename(id: string, currentTitle: string, e: React.MouseEvent) {
    e.stopPropagation()
    setEditingThreadId(id)
    setEditTitle(currentTitle)
  }

  function confirmRename(id: string) {
    const title = editTitle.trim() || 'New Chat'
    api.chat.updateThread(id, { title }).catch((e) => console.warn('Failed to rename thread:', e))
    setThreads((prev) => prev.map((t) => (t.id === id ? { ...t, title } : t)))
    setEditingThreadId(null)
    setEditTitle('')
  }

  function cancelRename() {
    setEditingThreadId(null)
    setEditTitle('')
  }

  function appendMessageToThread(threadId: string, msgs: Message[], newMsg: Message) {
    setMessages((prev) => [...prev, newMsg])
    setThreads((prev) => {
      const exists = prev.find((t) => t.id === threadId)
      const updatedMsgs = [...msgs, newMsg]
      if (exists) {
        return prev.map((t) =>
          t.id === threadId
            ? {
                ...t,
                messages: updatedMsgs,
                lastActive: Date.now(),
                title:
                  t.title === 'New Chat' && updatedMsgs.length >= 2
                    ? msgs[0].content.slice(0, 40) + (msgs[0].content.length > 40 ? '…' : '')
                    : t.title,
              }
            : t,
        )
      }
      return [
        {
          id: threadId,
          title: msgs[0].content.slice(0, 40) + (msgs[0].content.length > 40 ? '…' : ''),
          messages: updatedMsgs,
          lastActive: Date.now(),
        },
        ...prev,
      ]
    })
  }

  async function handleSend(contentOverride?: string, historyOverride?: Message[]) {
    const content = (contentOverride ?? userMessage).trim()
    if (!content || sending || !selectedModel) return

    const userMsg: Message = { role: 'user', content, timestamp: Date.now() }
    const updated = [...(historyOverride ?? messages), userMsg]
    setMessages(updated)
    setUserMessage('')
    setSending(true)
    setStreamingContent('')
    streamedContentRef.current = ''
    setThinkingContent('')
    thinkingContentRef.current = ''
    setToolEvents([])
    toolEventsRef.current = []
    setResponseModel(null)
    responseModelRef.current = null
    setError(null)

    let threadId = activeThreadId
    if (!threadId) {
      try {
        const { conversation } = await api.chat.createThread()
        threadId = conversation.id
        setActiveThreadId(threadId)
        setThreads((prev) => [
          {
            id: conversation.id,
            title: conversation.title,
            messages: [],
            lastActive: Date.now(),
          },
          ...prev,
        ])
      } catch {
        setError('Failed to create thread')
        return
      }
    }

    // Save user message to DB (fire-and-forget, don't block SSE)
    const currentThreadId = threadId
    api.chat
      .addMessage(currentThreadId, {
        role: 'user',
        content,
      })
      .catch((e) => console.warn('Failed to save user message:', e))

    const selectedModelObj = models.find((m) => m.id === selectedModel)
    const abort = new AbortController()
    abortRef.current = abort
    try {
      const res = await fetch('/api/chat/completions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Accept: 'text/event-stream', ...getAuthHeaders() },
        body: JSON.stringify({
          model: selectedModelObj?.model || selectedModel,
          messages: [
            ...(systemPrompt ? [{ role: 'system', content: stripRawXML(stripMarkers(systemPrompt)) }] : []),
            ...[...messages, userMsg].map((m) => ({ role: m.role, content: stripRawXML(stripMarkers(m.content)) })),
          ],
          stream: true,
          temperature,
          max_tokens: maxTokens,
          top_p: topP,
        }),
        signal: abort.signal,
      })
      if (!res.ok) {
        let m = `API error: ${res.status}`
        try {
          const b = await res.json()
          m = b.error?.message ?? b.message ?? m
        } catch {
          /* keep default */
        }
        throw new Error(m)
      }
      const actualModelFromHeader = res.headers.get('X-Ilter-Model-Actual')
      if (actualModelFromHeader) {
        responseModelRef.current = actualModelFromHeader
        setResponseModel(actualModelFromHeader)
      }
      const reader = res.body?.getReader()
      if (!reader) throw new Error('No response body')
      const decoder = new TextDecoder()
      let buffer = ''
      let currentEvent = ''
      let usageData: { ilter_cost?: number; ilter_billing_key?: string } | null = null
      // eslint-disable-next-line no-constant-condition
      readLoop: while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        const lines = buffer.split('\n')
        buffer = lines.pop() || ''
        for (const line of lines) {
          if (line.startsWith('event: ')) {
            currentEvent = line.slice(7).trim()
          } else if (line.startsWith('data: ')) {
            const data = line.slice(6).trim()
            if (currentEvent === 'ilter.tool_calls') {
              currentEvent = ''
              try {
                const calls = JSON.parse(data)
                const newEvents = calls.map((tc: Record<string, unknown>) => {
                  let args = '{}'
                  try {
                    args = JSON.stringify(
                      JSON.parse(((tc.function as Record<string, unknown>)?.arguments as string) || '{}'),
                      null,
                      2,
                    )
                  } catch (e) {
                    if (e instanceof SyntaxError) {
                      logger.error('Failed to parse tool call arguments:', e)
                    }
                  }
                  return {
                    id: tc.id as string,
                    name: ((tc.function as Record<string, unknown>)?.name as string) || 'unknown',
                    args,
                    status: 'calling' as const,
                  }
                })
                const existing = toolEventsRef.current || []
                const merged = [...existing]
                for (const tc of newEvents) {
                  if (!merged.find((e) => e.id === tc.id)) {
                    merged.push(tc)
                  }
                }
                toolEventsRef.current = merged
                setToolEvents(merged)
              } catch (e) {
                console.warn('ilter.tool_calls parse failed:', e)
              }
            } else if (currentEvent === 'ilter.tool_result') {
              currentEvent = ''
              try {
                const result = JSON.parse(data)
                const targetID = result.call_id || result.tool_call_id
                const isErr = Boolean(result.is_error || result.isError)
                const updated = (toolEventsRef.current || []).map((tc: ToolCallEvent) =>
                  tc.id === targetID || (!targetID && tc.status === 'calling')
                    ? {
                        ...tc,
                        status: isErr ? ('failed' as const) : ('completed' as const),
                        result: typeof result.content === 'string' ? result.content : JSON.stringify(result.content),
                      }
                    : tc,
                )

                toolEventsRef.current = updated
                setToolEvents(updated)
              } catch (e) {
                console.warn('ilter.tool_result parse failed:', e)
              }
            } else if (data === '[DONE]') {
              break readLoop
            } else {
              try {
                const parsed = JSON.parse(data)
                // Upstream chunk model unreliable; header from finalRoute is authoritative
                // Capture final usage chunk (has usage but no choices content)
                if (
                  parsed.usage &&
                  (!parsed.choices ||
                    parsed.choices.length === 0 ||
                    parsed.choices.every((c: Record<string, unknown>) => !c.delta))
                ) {
                  usageData = parsed.usage
                }
                const delta = parsed.choices?.[0]?.delta?.content || ''
                const reasoning = parsed.choices?.[0]?.delta?.reasoning_content || ''
                const toolCalls = parsed.choices?.[0]?.delta?.tool_calls || null
                if (reasoning) {
                  thinkingContentRef.current += reasoning
                  setThinkingContent(thinkingContentRef.current)
                }
                if (delta) {
                  streamedContentRef.current += delta
                  setStreamingContent(streamedContentRef.current)
                }
                // Fallback: only use raw delta.tool_calls when the backend didn't
                // already send an authoritative ilter.tool_calls event (native tool
                // call models are handled server-side in tool_injection.go).
                // Derived check: ref populated means ilter.tool_calls was received.
                if (toolCalls && toolCalls.length > 0 && !(toolEventsRef.current && toolEventsRef.current.length > 0)) {
                  const calls = toolCalls.map((tc: Record<string, unknown>) => {
                    let args = '{}'
                    try {
                      args = JSON.stringify(
                        JSON.parse(((tc.function as Record<string, unknown>)?.arguments as string) || '{}'),
                        null,
                        2,
                      )
                    } catch (e) {
                      if (e instanceof SyntaxError) {
                        logger.error('Failed to parse delta tool call arguments:', e)
                      }
                    }
                    return {
                      id: (tc.id as string) || `call_${Date.now()}`,
                      name: ((tc.function as Record<string, unknown>)?.name as string) || 'unknown',
                      args,
                      status: 'calling' as const,
                    }
                  })
                  toolEventsRef.current = calls
                  setToolEvents(calls)
                  // Inject position markers so tool cards render inline with content
                  // (only when markers aren't already present from backend processing)
                  if (!streamedContentRef.current.includes('ilter:tool:')) {
                    const markers = calls.map((_: unknown, i: number) => `ilter:tool:${i}`).join('')
                    streamedContentRef.current += markers
                    setStreamingContent(streamedContentRef.current)
                  }
                }
              } catch (e) {
                console.warn('SSE data chunk parse failed:', e)
              }
            }
          }
        }
      }
      const finalContent = streamedContentRef.current
      const finalReasoning = thinkingContentRef.current || undefined
      const savedToolCalls =
        toolEventsRef.current.length > 0
          ? toolEventsRef.current.map((tc) => ({
              ...tc,
              status: tc.status === 'calling' ? ('completed' as const) : tc.status,
            }))
          : undefined

      const savedUsageCost = usageData?.ilter_cost
      const savedBillingKey = usageData?.ilter_billing_key
      setThinkingContent('')
      setToolEvents([])
      if (finalContent || finalReasoning) {
        appendMessageToThread(threadId, updated, {
          role: 'assistant',
          content: finalContent,
          reasoningContent: finalReasoning,
          model: responseModelRef.current || selectedModel,
          timestamp: Date.now(),
          toolCalls: savedToolCalls,
          usageCost: savedUsageCost,
          billingKey: savedBillingKey,
        })
      } else if (savedToolCalls) {
        appendMessageToThread(threadId, updated, {
          role: 'assistant',
          content: '',
          toolCalls: savedToolCalls,
          model: responseModelRef.current || selectedModel,
          timestamp: Date.now(),
          usageCost: savedUsageCost,
          billingKey: savedBillingKey,
        })
      }

      // Save assistant message to DB
      api.chat
        .addMessage(currentThreadId, {
          role: 'assistant',
          content: finalContent || '',
          model: responseModelRef.current || selectedModel,
          cost: savedUsageCost,
          reasoning_content: finalReasoning,
          tool_calls: savedToolCalls ? JSON.stringify(savedToolCalls) : undefined,
          usage_cost: savedUsageCost,
          billing_key: savedBillingKey,
        })
        .catch((e) => console.warn('Failed to save assistant message:', e))

      // Auto-update title if first user message
      const firstUser = updated.find((m) => m.role === 'user')
      if (firstUser) {
        const title = firstUser.content.slice(0, 40) + (firstUser.content.length > 40 ? '…' : '')
        api.chat.updateThread(currentThreadId, { title }).catch(() => {})
      }
    } catch (err: unknown) {
      if ((err as Error)?.name === 'AbortError') return
      const msg = (err as Error)?.message || 'Failed to send message'
      setToolEvents([])
      setThinkingContent('')
      setError(msg)
      setMessages((prev) => {
        const last = prev[prev.length - 1]
        if (last?.role === 'assistant' && last.content === '') {
          return prev.slice(0, -1)
        }
        return prev
      })
      return
    } finally {
      setSending(false)
      streamedContentRef.current = ''
      thinkingContentRef.current = ''
      setThinkingContent('')
      setStreamingContent('')
      abortRef.current = null
    }
  }

  function stopGeneration() {
    abortRef.current?.abort()
    abortRef.current = null
  }

  function regenerateLast() {
    if (sending) return
    const lastUserIdx = messages.map((m) => m.role).lastIndexOf('user')
    if (lastUserIdx === -1) return
    const lastUserContent = messages[lastUserIdx].content
    handleSend(lastUserContent, messages.slice(0, lastUserIdx))
  }

  function handleVariableResolve(values: Record<string, string>) {
    if (!templateForFill) return
    let content = templateForFill.content
    for (const [key, value] of Object.entries(values)) {
      const escapedKey = key.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
      content = content.replace(
        new RegExp(`\\{\\{\\s*\\.?\\s*${escapedKey}\\s*\\}\\}|\\{\\s*\\.?\\s*${escapedKey}\\s*\\}`, 'g'),
        value,
      )
    }
    setTemplateForFill(null)
    setUserMessage(content)
    handleSend(content)
  }

  return (
    <QueryProvider>
      <div className="flex flex-1 min-h-0 overflow-hidden bg-white">
        <aside
          className={cn(
            'flex flex-col border-r border-surface-200 bg-surface-50 transition-all duration-200 flex-shrink-0',
            sidebarOpen ? 'w-64' : 'w-0 overflow-hidden',
          )}
        >
          <div className="flex items-center justify-between px-3 py-2.5 border-b border-surface-200">
            <span className="text-xs font-semibold text-surface-500 uppercase tracking-wider">Threads</span>
            <button
              onClick={() => setSidebarOpen(false)}
              className="p-1 rounded-md text-surface-400 hover:text-surface-600 hover:bg-surface-200 transition-colors"
              title="Close sidebar"
            >
              <PanelLeftClose size={14} />
            </button>
          </div>

          <div className="p-2">
            <Button
              variant="outline"
              size="sm"
              className="w-full justify-start gap-2 text-sm"
              onClick={createNewThread}
            >
              <Plus size={14} />
              New Chat
            </Button>
          </div>

          <nav className="flex-1 overflow-y-auto px-2 pb-2 space-y-0.5">
            {threads.length === 0 && <p className="text-xs text-surface-400 text-center py-8">No threads yet</p>}
            {threads.map((thread) => (
              <div
                key={thread.id}
                onClick={() => switchThread(thread.id)}
                className={cn(
                  'group flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg cursor-pointer text-sm transition-colors',
                  activeThreadId === thread.id
                    ? 'bg-brand-50 text-brand-700'
                    : 'text-surface-600 hover:bg-surface-100 hover:text-surface-800',
                )}
              >
                {editingThreadId === thread.id ? (
                  <div className="flex items-center gap-1 flex-1 min-w-0" onClick={(e) => e.stopPropagation()}>
                    <input
                      value={editTitle}
                      onChange={(e) => setEditTitle(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') confirmRename(thread.id)
                        if (e.key === 'Escape') cancelRename()
                      }}
                      className="flex-1 min-w-0 rounded border border-brand-300 bg-white px-1.5 py-0.5 text-xs text-surface-900 outline-none focus:ring-1 focus:ring-brand-500"
                    />
                    <button
                      onClick={() => confirmRename(thread.id)}
                      className="p-0.5 text-brand-500 hover:text-brand-700"
                    >
                      <Check size={12} />
                    </button>
                    <button onClick={cancelRename} className="p-0.5 text-surface-400 hover:text-surface-600">
                      <X size={12} />
                    </button>
                  </div>
                ) : (
                  <>
                    <span className="flex-1 truncate text-xs">{thread.title}</span>
                    <div className="hidden group-hover:flex items-center gap-0.5">
                      <button
                        onClick={(e) => startRename(thread.id, thread.title, e)}
                        className="p-0.5 rounded text-surface-400 hover:text-surface-600 hover:bg-surface-200 transition-colors"
                      >
                        <Edit3 size={11} />
                      </button>
                      <button
                        onClick={(e) => deleteThread(thread.id, e)}
                        className="p-0.5 rounded text-surface-400 hover:text-red-500 hover:bg-red-50 transition-colors"
                      >
                        <Trash2 size={11} />
                      </button>
                    </div>
                  </>
                )}
              </div>
            ))}
          </nav>
        </aside>

        <main className="flex-1 flex flex-col min-w-0 relative">
          {!sidebarOpen && (
            <button
              onClick={() => setSidebarOpen(true)}
              className="absolute top-2 left-2 z-10 p-1.5 rounded-md text-surface-400 hover:text-surface-600 hover:bg-surface-100 transition-colors"
              title="Open sidebar"
            >
              <PanelRightOpen size={16} />
            </button>
          )}
          {messages.length === 0 ? (
            <div className="flex-1 flex flex-col items-center justify-center px-6 text-center">
              <div className="w-12 h-12 rounded-xl bg-brand-50 text-brand-600 flex items-center justify-center mb-4">
                <Bot size={24} />
              </div>
              <h2 className="text-lg font-semibold text-surface-900 mb-1">Start a conversation</h2>
              <p className="text-sm text-surface-500 max-w-sm">
                Select a model below and type your message to begin. Your chat threads are saved locally.
              </p>
              <hr className="w-full max-w-sm my-6 border-surface-200" />
            </div>
          ) : (
            <div
              ref={messagesContainerRef}
              onScroll={handleScroll}
              className="flex-1 overflow-y-auto px-4 py-4 space-y-3"
            >
              {hasOlderMessages && (
                <div className="flex justify-center">
                  <button
                    onClick={loadOlder}
                    className="text-xs text-brand-600 hover:text-brand-700 font-medium px-3 py-1.5 rounded-lg bg-brand-50 hover:bg-brand-100 transition-colors flex items-center gap-1.5"
                  >
                    <ChevronUp size={12} />
                    {messages.length - visibleCount} older messages — scroll up or click
                  </button>
                </div>
              )}
              {visibleMessages.map((msg, i) => {
                const realIndex = messages.length - visibleMessages.length + i
                return (
                  <ChatMessage
                    key={realIndex}
                    message={msg}
                    isLast={realIndex === messages.length - 1}
                    isStreaming={false}
                    onRegenerate={regenerateLast}
                  />
                )
              })}
              {(sending || streamingContent) && (
                <StreamingMessage
                  content={streamingContent}
                  thinkingContent={thinkingContent}
                  model={responseModel || selectedModel}
                  toolEvents={toolEvents}
                />
              )}
              <div ref={messagesEndRef} />
            </div>
          )}

          {error && (
            <div className="mx-4 mb-2 flex items-center gap-2 rounded-lg bg-red-50 border border-red-200 px-3 py-2 text-sm text-red-700">
              <AlertTriangle size={14} className="shrink-0" />
              <span className="flex-1">{error}</span>
              <button onClick={() => setError(null)} className="p-0.5 rounded text-red-400 hover:text-red-600">
                <X size={13} />
              </button>
            </div>
          )}

          {loading && (
            <div className="flex items-center justify-center gap-2 py-2 text-sm text-surface-400">
              <Loader2 size={14} className="animate-spin" />
              Loading models...
            </div>
          )}

          {templateForFill && (
            <VariableFillDialog
              templateName={templateForFill.name}
              variables={templateForFill.variables}
              onResolve={handleVariableResolve}
              onClose={() => setTemplateForFill(null)}
            />
          )}

          <div className="relative border-t border-surface-200 bg-white px-4 py-3">
            <div className="flex items-center gap-2 mb-2">
              <ModelSelector
                models={models}
                value={selectedModel}
                onChange={(id) => {
                  setSelectedModel(id)
                  localStorage.setItem(MODEL_KEY, id)
                }}
                disabled={sending || loading}
                side="top"
              />
              <PromptSelector onSelect={selectTemplateFromShortcut} disabled={sending} />
              <button
                onClick={() => setShowParams(!showParams)}
                className={cn(
                  'p-1.5 rounded-md transition-colors',
                  showParams
                    ? 'bg-brand-100 text-brand-600'
                    : 'text-surface-400 hover:text-surface-600 hover:bg-surface-100',
                )}
                title="Parameters"
              >
                <Settings size={16} />
              </button>
            </div>

            {showParams && (
              <div className="rounded-xl border border-surface-200 bg-surface-50 p-4 space-y-3 text-sm mb-3">
                <div>
                  <label className="block text-xs font-medium text-surface-600 mb-1">System Prompt</label>
                  <textarea
                    value={systemPrompt}
                    onChange={(e) => setSystemPrompt(e.target.value)}
                    placeholder="You are a helpful assistant..."
                    rows={2}
                    className="w-full resize-none rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-900 placeholder:text-surface-400 outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500"
                  />
                </div>
                <div>
                  <div className="flex justify-between items-center mb-1">
                    <label className="text-xs font-medium text-surface-600">Temperature</label>
                    <span className="text-xs text-surface-500">{temperature}</span>
                  </div>
                  <input
                    type="range"
                    min={0}
                    max={2}
                    step={0.1}
                    value={temperature}
                    onChange={(e) => setTemperature(parseFloat(e.target.value))}
                    className="w-full accent-brand-600"
                  />
                </div>
                <div>
                  <div className="flex justify-between items-center mb-1">
                    <label className="text-xs font-medium text-surface-600">Max Tokens</label>
                    <span className="text-xs text-surface-500">{maxTokens}</span>
                  </div>
                  <input
                    type="range"
                    min={64}
                    max={32768}
                    step={64}
                    value={maxTokens}
                    onChange={(e) => setMaxTokens(parseInt(e.target.value, 10))}
                    className="w-full accent-brand-600"
                  />
                </div>
                <div>
                  <div className="flex justify-between items-center mb-1">
                    <label className="text-xs font-medium text-surface-600">Top P</label>
                    <span className="text-xs text-surface-500">{topP}</span>
                  </div>
                  <input
                    type="range"
                    min={0}
                    max={1}
                    step={0.05}
                    value={topP}
                    onChange={(e) => setTopP(parseFloat(e.target.value))}
                    className="w-full accent-brand-600"
                  />
                </div>
              </div>
            )}

            <div className="flex items-end gap-2">
              <textarea
                ref={textareaRef}
                value={userMessage}
                onChange={(e) => setUserMessage(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && !e.shiftKey) {
                    e.preventDefault()
                    handleSend()
                  }
                }}
                placeholder="Type a message... (Shift+Enter for new line)"
                rows={2}
                disabled={sending || loading}
                className="flex-1 resize-none rounded-lg border border-surface-300 bg-surface-50 px-3 py-2 text-sm text-surface-900 placeholder:text-surface-400 outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500 disabled:opacity-50 min-h-[64px] max-h-[120px]"
                onInput={(e) => {
                  const el = e.currentTarget
                  el.style.height = 'auto'
                  el.style.height = `${Math.min(el.scrollHeight, 120)}px`
                }}
              />

              {sending ? (
                <Button
                  variant="destructive"
                  onClick={stopGeneration}
                  className="shrink-0 gap-2 h-16 px-8 text-base font-medium"
                >
                  <Square size={18} />
                  Stop
                </Button>
              ) : (
                <Button
                  variant="default"
                  onClick={() => handleSend()}
                  disabled={!userMessage.trim() || !selectedModel || loading}
                  className="shrink-0 gap-2 h-16 px-8 text-base font-medium"
                >
                  <Send size={18} />
                  Send
                </Button>
              )}
            </div>
          </div>
        </main>
      </div>
    </QueryProvider>
  )
}
