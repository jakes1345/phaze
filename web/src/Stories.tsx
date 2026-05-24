/* eslint-disable react-hooks/set-state-in-effect */
import { useEffect, useMemo, useRef, useState } from 'react'
import './stories.css'

interface Story {
  id: number
  author: string
  media_url: string
  media_kind: 'image' | 'video'
  caption?: string
  created_at: string
  expires_at: string
  views?: number
}

interface Props {
  me: string
  sessionToken: string
}

// Stories: thin horizontal ring above the friends/chat panel showing every
// user with active (last-24h) media. Click a ring to open the viewer.
// Authored posts get an upload-attached "+" tile up front.
export default function Stories({ me, sessionToken }: Props) {
  const [stories, setStories] = useState<Story[]>([])
  const [viewing, setViewing] = useState<string | null>(null) // username
  const [openIndex, setOpenIndex] = useState(0)
  const [uploading, setUploading] = useState(false)
  const [replyDraft, setReplyDraft] = useState('')
  const [replyOk, setReplyOk] = useState(false)
  const fileRef = useRef<HTMLInputElement | null>(null)

  const refresh = useMemo(() => async () => {
    if (!sessionToken) return
    const r = await fetch('/api/v1/stories', { headers: { Authorization: `Bearer ${sessionToken}` } })
    if (!r.ok) return
    const data = await r.json()
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setStories(data)
  }, [sessionToken])

  useEffect(() => {
    void refresh()
    const t = setInterval(() => { void refresh() }, 60_000)
    return () => clearInterval(t)
  }, [refresh])

  // Group by author, preserve insertion order.
  const byAuthor = useMemo(() => {
    const m = new Map<string, Story[]>()
    for (const s of stories) {
      const arr = m.get(s.author) ?? []
      arr.push(s)
      m.set(s.author, arr)
    }
    return m
  }, [stories])
  const authors = Array.from(byAuthor.keys())

  const handleAdd = async (file: File) => {
    if (!sessionToken) return
    setUploading(true)
    try {
      const fd = new FormData()
      fd.append('file', file)
      const up = await fetch('/api/v1/upload', {
        method: 'POST',
        headers: { Authorization: `Bearer ${sessionToken}` },
        body: fd,
      })
      if (!up.ok) throw new Error(`upload ${up.status}`)
      const att = await up.json() as { url: string; mime: string }
      const kind: 'image' | 'video' = att.mime.startsWith('video/') ? 'video' : 'image'
      const post = await fetch('/api/v1/stories', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${sessionToken}` },
        body: JSON.stringify({ media_url: att.url, media_kind: kind }),
      })
      if (!post.ok) throw new Error(`post ${post.status}`)
      await refresh()
    } catch (e) {
      console.warn('[stories] add failed', e)
    } finally {
      setUploading(false)
    }
  }

  const open = (author: string) => {
    const list = byAuthor.get(author) ?? []
    if (list.length === 0) return // nothing to show
    setViewing(author)
    setOpenIndex(0)
    void fetch(`/api/v1/stories/${list[0].id}/view`, {
      method: 'POST', headers: { Authorization: `Bearer ${sessionToken}` },
    })
  }

  const advance = () => {
    if (!viewing) return
    const list = byAuthor.get(viewing) ?? []
    const next = openIndex + 1
    if (next >= list.length) {
      setViewing(null)
      return
    }
    setOpenIndex(next)
    void fetch(`/api/v1/stories/${list[next].id}/view`, {
      method: 'POST', headers: { Authorization: `Bearer ${sessionToken}` },
    })
  }

  return (
    <div className="stories-bar">
      <button
        type="button"
        className="story-ring add"
        title="Add to your story"
        disabled={uploading}
        onClick={() => fileRef.current?.click()}
      >
        <span className="ring-inner">＋</span>
        <span className="story-label">Your story</span>
      </button>
      <input
        ref={fileRef}
        type="file"
        accept="image/*,video/*"
        style={{ display: 'none' }}
        onChange={(e) => {
          const f = e.target.files?.[0]
          if (f) void handleAdd(f)
          e.target.value = ''
        }}
      />
      {authors.map((a) => (
        <button key={a} type="button" className={`story-ring ${a === me ? 'self' : ''}`} onClick={() => open(a)}>
          <span className="ring-inner">{a[0]?.toUpperCase() ?? '?'}</span>
          <span className="story-label">{a === me ? 'You' : a}</span>
        </button>
      ))}

      {viewing && (() => {
        const list = byAuthor.get(viewing) ?? []
        const s = list[openIndex]
        if (!s) return null
        const isMine = s.author === me
        return (
          <div className="story-viewer" role="presentation">
            <div className="story-progress" onClick={advance}>
              {list.map((_, i) => (
                <span key={i} className={`progress-bar ${i < openIndex ? 'done' : i === openIndex ? 'active' : ''}`} />
              ))}
            </div>
            <div className="story-header">
              <span className="story-author">{viewing}</span>
              <button
                type="button"
                className="story-close"
                onClick={(e) => { e.stopPropagation(); setViewing(null) }}
                aria-label="Close"
              >×</button>
            </div>
            <div className="story-stage" onClick={advance}>
              {s.media_kind === 'image' ? (
                <img className="story-media" src={s.media_url} alt="" />
              ) : (
                <video className="story-media" src={s.media_url} autoPlay playsInline controls />
              )}
            </div>
            {s.caption && <div className="story-caption">{s.caption}</div>}
            {isMine ? (
              <div className="story-meta">{s.views ?? 0} view{(s.views ?? 0) === 1 ? '' : 's'}</div>
            ) : (
              <form
                className="story-reply"
                onClick={(e) => e.stopPropagation()}
                onSubmit={async (e) => {
                  e.preventDefault()
                  const text = replyDraft.trim()
                  if (!text) return
                  await fetch(`/api/v1/stories/${s.id}/reply`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${sessionToken}` },
                    body: JSON.stringify({ body: text }),
                  })
                  setReplyDraft('')
                  setReplyOk(true)
                  setTimeout(() => setReplyOk(false), 1800)
                }}
              >
                <input
                  type="text"
                  placeholder={`Reply to ${viewing}…`}
                  value={replyDraft}
                  onChange={(e) => setReplyDraft(e.target.value)}
                />
                <button type="submit" disabled={!replyDraft.trim()}>➤</button>
                {replyOk && <span className="story-reply-ok">Sent</span>}
              </form>
            )}
          </div>
        )
      })()}
    </div>
  )
}
