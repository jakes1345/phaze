import { useEffect, useRef, useState } from 'react'
import './support.css'

interface Msg {
  role: 'user' | 'assistant'
  content: string
}

interface Props {
  /** Auth bearer token, if user is signed in. Optional. */
  sessionToken?: string | null
  /** Authenticated username, optional — sent to the human-handoff endpoint. */
  me?: string | null
}

const STORAGE_KEY = 'phaze_support_history'

const GREETING: Msg = {
  role: 'assistant',
  content: "Hi! I'm Phaze Helper. Ask me anything about Phaze — how to make calls, join Spaces, set up Recovery PIN, anything. Or tap \"Talk to a human\" and a live agent will follow up.",
}

export default function SupportBubble({ sessionToken, me }: Props) {
  const [open, setOpen] = useState(false)
  const [msgs, setMsgs] = useState<Msg[]>(() => {
    try {
      const raw = localStorage.getItem(STORAGE_KEY)
      if (raw) {
        const parsed = JSON.parse(raw)
        if (Array.isArray(parsed)) return parsed
      }
    } catch { /* fall through */ }
    return [GREETING]
  })
  const [draft, setDraft] = useState('')
  const [busy, setBusy] = useState(false)
  const [escalated, setEscalated] = useState(false)
  const scrollRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    try { localStorage.setItem(STORAGE_KEY, JSON.stringify(msgs)) } catch { /* quota etc */ }
  }, [msgs])

  useEffect(() => {
    if (!open) return
    scrollRef.current?.scrollTo({ top: 999999, behavior: 'smooth' })
  }, [msgs, open])

  const send = async () => {
    const text = draft.trim()
    if (!text || busy) return
    const next: Msg[] = [...msgs, { role: 'user', content: text }]
    setMsgs(next)
    setDraft('')
    setBusy(true)
    try {
      const apiMessages = next.filter((m, i) => !(i === 0 && m === GREETING))
      const resp = await fetch('/api/v1/support/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ messages: apiMessages }),
      })
      if (!resp.ok) {
        const txt = await resp.text()
        setMsgs((m) => [...m, { role: 'assistant', content: `(error: ${txt || resp.status})` }])
        return
      }
      const data = (await resp.json()) as { reply: string }
      setMsgs((m) => [...m, { role: 'assistant', content: data.reply || '(no reply)' }])
    } catch (e) {
      setMsgs((m) => [...m, { role: 'assistant', content: `(network error: ${(e as Error).message})` }])
    } finally {
      setBusy(false)
    }
  }

  const escalate = async () => {
    if (escalated) return
    const note = msgs.map((m) => `${m.role === 'user' ? 'User' : 'Bot'}: ${m.content}`).slice(-8).join('\n')
    try {
      const resp = await fetch('/api/v1/support/escalate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ username: me ?? '', note }),
      })
      if (!resp.ok) {
        setMsgs((m) => [...m, { role: 'assistant', content: '(could not reach a live agent right now — try again later)' }])
        return
      }
      const data = (await resp.json()) as { agents_online: number }
      if (data.agents_online > 0) {
        setMsgs((m) => [...m, { role: 'assistant', content: `Notified ${data.agents_online} live agent${data.agents_online === 1 ? '' : 's'}. They'll follow up via DM. Keep this window open if you can.` }])
      } else {
        setMsgs((m) => [...m, { role: 'assistant', content: "No live agents are online right now. Your note has been queued — an agent will DM you when one's available." }])
      }
      setEscalated(true)
    } catch (e) {
      setMsgs((m) => [...m, { role: 'assistant', content: `(escalation failed: ${(e as Error).message})` }])
    }
  }

  const reset = () => {
    setMsgs([GREETING])
    setEscalated(false)
    try { localStorage.removeItem(STORAGE_KEY) } catch { /* ignore */ }
  }

  return (
    <>
      <button
        type="button"
        className={`support-fab ${open ? 'on' : ''}`}
        onClick={() => setOpen((v) => !v)}
        aria-label={open ? 'Close support chat' : 'Open support chat'}
        title={open ? 'Close' : 'Need help?'}
      >
        {open ? '✕' : '💬'}
      </button>
      {open && (
        <div className="support-window" role="dialog" aria-label="Phaze support">
          <header className="support-head">
            <span className="support-title">Phaze Helper</span>
            <button type="button" className="support-reset" onClick={reset} title="Reset conversation">↺</button>
          </header>
          <div className="support-body" ref={scrollRef}>
            {msgs.map((m, i) => (
              <div key={i} className={`support-msg ${m.role}`}>
                <span className="support-bubble">{m.content}</span>
              </div>
            ))}
            {busy && <div className="support-msg assistant"><span className="support-bubble typing">…</span></div>}
          </div>
          <form
            className="support-composer"
            onSubmit={(e) => { e.preventDefault(); void send() }}
          >
            <input
              type="text"
              placeholder="Type a message…"
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              disabled={busy}
              autoFocus
            />
            <button type="submit" disabled={busy || !draft.trim()} aria-label="Send">➤</button>
          </form>
          <footer className="support-foot">
            <button type="button" className="support-escalate" onClick={() => void escalate()} disabled={escalated}>
              {escalated ? '✓ Escalated to live agent' : '🙋 Talk to a human'}
            </button>
          </footer>
        </div>
      )}
    </>
  )
}
