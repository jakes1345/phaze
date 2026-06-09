import { useEffect, useMemo, useRef, useState } from 'react'
import type { ChannelInfo, ChannelMsg, NexusMessage, ServerSummary, TurnConfig } from './nexusTypes'
import VoiceRoom from './VoiceRoom'
import './spaces.css'

interface FileAttachment {
  url: string
  name: string
  mime: string
  size: number
}

interface Props {
  me: string
  /** sends a NexusMessage on the parent socket. */
  send: (m: NexusMessage) => void
  /** lets us subscribe to every inbound message (for server_ / channel_ types). */
  subscribe: (handler: (m: NexusMessage) => void) => () => void
  /** TURN config from the parent; passed through to voice rooms. */
  turn?: TurnConfig | null
  /** Opens the parent app's user-profile modal for the given username. */
  onUserClick?: (username: string) => void
  /** Uploads a file via the parent's session token; returns attachment or null. */
  uploadAttachment?: (file: File) => Promise<FileAttachment | null>
}

const FILE_PREFIX = 'phaze-file'
function decodeFile(text: string): FileAttachment | null {
  if (!text.startsWith(FILE_PREFIX)) return null
  try {
    const a = JSON.parse(text.slice(FILE_PREFIX.length)) as FileAttachment
    if (a && typeof a.url === 'string' && typeof a.name === 'string') return a
    return null
  } catch { return null }
}
function isImage(mime: string, name: string): boolean {
  if (mime?.startsWith('image/')) return true
  return /\.(png|jpe?g|gif|webp|bmp)$/i.test(name)
}
function isVideo(mime: string, name: string): boolean {
  if (mime?.startsWith('video/')) return true
  return /\.(mp4|mov|webm|mkv)$/i.test(name)
}
const CHANNEL_EMOJIS = ['😀','😂','😍','😎','🤔','😢','😡','👍','👎','❤️','🔥','🎉','🙏','👀','💯','✨','😅','🥹','😴','🤝','🚀','👋','🤣','😭','🥲','😏','💀','🤡','🫡','🫶']

function fmtBytes(b: number): string {
  if (b < 1024) return `${b} B`
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} KB`
  return `${(b / 1024 / 1024).toFixed(1)} MB`
}

interface Toast {
  id: number
  body: string
  tone: 'info' | 'error' | 'ok'
}

let toastSeq = 0

function containsMention(text: string, username: string): boolean {
  return new RegExp(`@${username}\\b`, 'i').test(text)
}

function RichText({ text, me }: { text: string; me: string }) {
  const parts = text.split(/(@\w+)/g)
  return (
    <>
      {parts.map((p, i) => {
        if (p.startsWith('@')) {
          const name = p.slice(1)
          const isMe = name.toLowerCase() === me.toLowerCase()
          return <span key={i} className={`msg-mention ${isMe ? 'me' : ''}`}>{p}</span>
        }
        const urlRe = /(https?:\/\/[^\s]+)/g
        const segs = p.split(urlRe)
        return segs.map((s, j) =>
          urlRe.test(s) ? <a key={`${i}-${j}`} href={s} target="_blank" rel="noopener noreferrer" className="msg-link">{s}</a> : <span key={`${i}-${j}`}>{s}</span>
        )
      })}
    </>
  )
}

export default function Spaces({ me, send, subscribe, turn = null, onUserClick, uploadAttachment }: Props) {
  const [servers, setServers] = useState<ServerSummary[]>([])
  const [activeServer, setActiveServer] = useState<string | null>(null)
  const [channelsByServer, setChannelsByServer] = useState<Record<string, ChannelInfo[]>>({})
  const [activeChannel, setActiveChannel] = useState<string | null>(null)
  const [messagesByChannel, setMessagesByChannel] = useState<Record<string, ChannelMsg[]>>({})
  const [membersByServer, setMembersByServer] = useState<Record<string, string[]>>({})
  const [draft, setDraft] = useState('')
  const [composerOpen, setComposerOpen] = useState<null | 'create' | 'join' | 'discover'>(null)
  const [discoverList, setDiscoverList] = useState<ServerSummary[]>([])
  const [discoverLoading, setDiscoverLoading] = useState(false)
  const [newName, setNewName] = useState('')
  const [newTopic, setNewTopic] = useState('')
  const [newVisibility, setNewVisibility] = useState<'public' | 'private'>('private')
  const [joinCode, setJoinCode] = useState('')
  const [membersOpen, setMembersOpen] = useState(false)
  const [newChannelOpen, setNewChannelOpen] = useState(false)
  const [newChannelName, setNewChannelName] = useState('')
  const [toasts, setToasts] = useState<Toast[]>([])
  const chatBottomRef = useRef<HTMLDivElement | null>(null)
  const fileInputRef = useRef<HTMLInputElement | null>(null)
  const draftInputRef = useRef<HTMLTextAreaElement | null>(null)
  const [emojiOpen, setEmojiOpen] = useState(false)
  const [searchOpen, setSearchOpen] = useState(false)
  const [search, setSearch] = useState('')
  const [pinsOpen, setPinsOpen] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [editDraft, setEditDraft] = useState('')
  const [mentionQuery, setMentionQuery] = useState<string | null>(null)
  const [mentionIdx, setMentionIdx] = useState(0)

  const members = activeServer ? (membersByServer[activeServer] ?? []) : []
  const mentionMatches = useMemo(() => {
    if (mentionQuery === null) return []
    const q = mentionQuery.toLowerCase()
    return members.filter((u) => u.toLowerCase().includes(q) && u !== me).slice(0, 8)
  }, [mentionQuery, members, me])

  const completeMention = (name: string) => {
    if (mentionQuery === null) return
    const atPos = draft.lastIndexOf('@')
    if (atPos < 0) return
    setDraft(draft.slice(0, atPos) + '@' + name + ' ')
    setMentionQuery(null)
    setMentionIdx(0)
    draftInputRef.current?.focus()
  }

  const handleDraftChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    const val = e.target.value
    setDraft(val)
    const cursor = e.target.selectionStart ?? val.length
    const before = val.slice(0, cursor)
    const atMatch = before.match(/@(\w*)$/)
    if (atMatch) {
      setMentionQuery(atMatch[1])
      setMentionIdx(0)
    } else {
      setMentionQuery(null)
    }
  }

  const beginEdit = (msg: ChannelMsg) => {
    setEditingId(String(msg.id))
    setEditDraft(msg.body)
  }
  const cancelEdit = () => { setEditingId(null); setEditDraft('') }
  const saveEdit = () => {
    if (!editingId || !activeChannel) return
    const body = editDraft.trim()
    if (body) send({ type: 'channel_edit', channel_id: activeChannel, msg_id: editingId, body })
    cancelEdit()
  }

  const toast = (body: string, tone: Toast['tone'] = 'info') => {
    const id = ++toastSeq
    setToasts((t) => [...t, { id, body, tone }])
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), 4500)
  }

  const activeChannelInfo = useMemo(() => {
    if (!activeServer || !activeChannel) return null
    const chans = channelsByServer[activeServer] ?? []
    return chans.find((c) => c.id === activeChannel) ?? null
  }, [activeServer, activeChannel, channelsByServer])

  const activeServerInfo = useMemo(
    () => servers.find((s) => s.id === activeServer) ?? null,
    [servers, activeServer],
  )

  // Initial server list + subscribe to push updates.
  useEffect(() => {
    send({ type: 'server_list' })
    const unsub = subscribe((m) => {
      switch (m.type) {
        case 'server_list_result':
          if (m.status === 'ok') setServers(m.servers ?? [])
          break
        case 'server_discover_result':
          setDiscoverLoading(false)
          if (m.status === 'ok') setDiscoverList(m.servers ?? [])
          else if (m.error) toast(m.error, 'error')
          break
        case 'server_result':
          if (m.status === 'ok' && m.server_id) {
            const summary: ServerSummary = {
              id: m.server_id,
              name: m.server_name ?? 'Untitled',
              owner: me,
              role: 'owner',
              visibility: (m.visibility as 'public' | 'private') ?? 'private',
              invite_code: m.invite_code,
            }
            setServers((prev) => [...prev.filter((s) => s.id !== summary.id), summary])
            if (m.channels) setChannelsByServer((c) => ({ ...c, [summary.id]: m.channels! }))
            setActiveServer(summary.id)
            if (m.channels?.length) setActiveChannel(m.channels[0].id)
            toast(`Space created — share code ${m.invite_code}`, 'ok')
          } else if (m.error) {
            toast(m.error, 'error')
          }
          break
        case 'server_join_result':
          if (m.status === 'ok' && m.server_id) {
            send({ type: 'server_list' })
            if (m.channels) setChannelsByServer((c) => ({ ...c, [m.server_id!]: m.channels! }))
            setActiveServer(m.server_id)
            if (m.channels?.length) setActiveChannel(m.channels[0].id)
            toast(`Joined ${m.server_name ?? 'space'}`, 'ok')
          } else if (m.error) {
            toast(m.error, 'error')
          }
          break
        case 'server_leave_result':
          if (m.status === 'ok' && m.server_id) {
            setServers((prev) => prev.filter((s) => s.id !== m.server_id))
            if (activeServer === m.server_id) {
              setActiveServer(null)
              setActiveChannel(null)
            }
            toast('Left space', 'info')
          } else if (m.error) {
            toast(m.error, 'error')
          }
          break
        case 'server_info_result':
          if (m.status === 'ok' && m.server_id) {
            if (m.channels) setChannelsByServer((c) => ({ ...c, [m.server_id!]: m.channels! }))
            if (m.members) setMembersByServer((mc) => ({ ...mc, [m.server_id!]: m.members! }))
          }
          break
        case 'server_channels_updated':
          if (m.server_id && m.channels) {
            setChannelsByServer((c) => ({ ...c, [m.server_id!]: m.channels! }))
          }
          break
        case 'channel_result':
          if (m.status === 'ok' && m.channel_id) {
            setActiveChannel(m.channel_id)
            setNewChannelOpen(false)
            setNewChannelName('')
          } else if (m.error) {
            toast(m.error, 'error')
          }
          break
        case 'channel_history_result':
          if (m.status === 'ok' && m.channel_id && m.messages) {
            setMessagesByChannel((mc) => ({ ...mc, [m.channel_id!]: m.messages! }))
          }
          break
        case 'channel_msg_in':
          if (m.channel_id && m.messages && m.messages[0]) {
            const incoming = m.messages[0]
            setMessagesByChannel((mc) => {
              const existing = mc[m.channel_id!] ?? []
              if (existing.some((x) => x.id === incoming.id)) return mc
              return { ...mc, [m.channel_id!]: [...existing, incoming] }
            })
          }
          break
        case 'channel_msg_result':
          if (m.error) toast(m.error, 'error')
          break
        case 'channel_react_in':
        case 'channel_edit_in':
        case 'channel_delete_in':
        case 'channel_pin_in': {
          if (!m.channel_id || !m.messages?.[0]) break
          const upd = m.messages[0]
          setMessagesByChannel((mc) => {
            const list = mc[m.channel_id!] ?? []
            return {
              ...mc,
              [m.channel_id!]: list.map((x) => x.id === upd.id ? { ...x, ...upd } : x),
            }
          })
          break
        }
      }
    })
    return unsub
  }, [me, send, subscribe, activeServer])

  // Load channels + history when active server/channel changes.
  useEffect(() => {
    if (!activeServer) return
    if (!channelsByServer[activeServer]) {
      send({ type: 'server_info', server_id: activeServer })
    }
  }, [activeServer, channelsByServer, send])

  useEffect(() => {
    if (!activeChannel) return
    if (!messagesByChannel[activeChannel]) {
      send({ type: 'channel_history', channel_id: activeChannel })
    }
  }, [activeChannel, messagesByChannel, send])

  // Auto-scroll on new messages.
  useEffect(() => {
    if (!chatBottomRef.current) return
    chatBottomRef.current.scrollIntoView({ behavior: 'smooth', block: 'end' })
  }, [activeChannel, messagesByChannel])

  const submitMessage = () => {
    if (!activeChannel) return
    const body = draft.trim()
    if (!body) return
    send({ type: 'channel_msg', channel_id: activeChannel, body })
    setDraft('')
  }

  const submitFile = async (file: File) => {
    if (!activeChannel || !uploadAttachment) return
    if (file.size > 25 * 1024 * 1024) { toast('File exceeds 25 MB', 'error'); return }
    const att = await uploadAttachment(file)
    if (!att) { toast('Upload failed', 'error'); return }
    send({ type: 'channel_msg', channel_id: activeChannel, body: FILE_PREFIX + JSON.stringify(att) })
  }

  const createServer = () => {
    if (!newName.trim()) {
      toast('Pick a name', 'error')
      return
    }
    send({
      type: 'server_create',
      server_name: newName.trim(),
      topic: newTopic.trim(),
      visibility: newVisibility,
    })
    setComposerOpen(null)
    setNewName('')
    setNewTopic('')
  }

  const joinServer = () => {
    if (!joinCode.trim()) {
      toast('Paste an invite code', 'error')
      return
    }
    send({ type: 'server_join', invite_code: joinCode.trim() })
    setComposerOpen(null)
    setJoinCode('')
  }

  const openDiscover = () => {
    setDiscoverList([])
    setDiscoverLoading(true)
    setComposerOpen('discover')
    send({ type: 'server_discover' })
  }

  const joinPublic = (id: string) => {
    send({ type: 'server_join', server_id: id })
    setComposerOpen(null)
  }

  const createChannel = () => {
    if (!activeServer || !newChannelName.trim()) return
    send({
      type: 'channel_create',
      server_id: activeServer,
      channel_name: newChannelName.trim().toLowerCase().replace(/\s+/g, '-'),
      kind: 'text',
    })
  }

  const leaveServer = () => {
    if (!activeServer) return
    if (!confirm('Leave this space?')) return
    send({ type: 'server_leave', server_id: activeServer })
  }

  const copyInvite = async () => {
    if (!activeServerInfo?.invite_code) return
    try {
      await navigator.clipboard.writeText(activeServerInfo.invite_code)
      toast('Invite code copied', 'ok')
    } catch {
      toast(`Invite: ${activeServerInfo.invite_code}`, 'info')
    }
  }

  return (
    <div className="spaces-root">
      <aside className="server-rail">
        <button
          className="server-icon home"
          aria-label="Direct messages — back to chat"
          title="(future: jump back to DMs)"
        >
          <SkypeMark />
        </button>
        <div className="rail-divider" />
        {servers.map((s) => (
          <button
            key={s.id}
            type="button"
            className={`server-icon ${activeServer === s.id ? 'active' : ''}`}
            onClick={() => {
              setActiveServer(s.id)
              const chans = channelsByServer[s.id] ?? []
              if (chans.length) setActiveChannel(chans[0].id)
            }}
            title={s.name}
          >
            <span className="server-icon-letter">{initials(s.name)}</span>
            <span className="server-tooltip">{s.name}</span>
          </button>
        ))}
        <button
          className="server-icon add"
          type="button"
          onClick={() => setComposerOpen('create')}
          title="Create a new space"
          aria-label="Create space"
        >
          +
        </button>
        <button
          className="server-icon add ghost"
          type="button"
          onClick={() => setComposerOpen('join')}
          title="Join with invite code"
          aria-label="Join space"
        >
          ⤵
        </button>
        <button
          className="server-icon add ghost"
          type="button"
          onClick={openDiscover}
          title="Discover public spaces"
          aria-label="Discover spaces"
        >
          🌐
        </button>
      </aside>

      <section className="channel-pane">
        {activeServerInfo ? (
          <>
            <header className="channel-pane-head">
              <div className="server-name-block">
                <h2>{activeServerInfo.name}</h2>
                <span className={`vis-pill ${activeServerInfo.visibility}`}>
                  {activeServerInfo.visibility}
                </span>
              </div>
              {activeServerInfo.invite_code && (
                <button type="button" className="ghost-btn" onClick={copyInvite}>
                  Invite · {activeServerInfo.invite_code}
                </button>
              )}
            </header>
            <div className="channel-list">
              {(channelsByServer[activeServer!] ?? []).map((c) => (
                <button
                  key={c.id}
                  type="button"
                  className={`channel-row ${activeChannel === c.id ? 'active' : ''} kind-${c.kind}`}
                  onClick={() => setActiveChannel(c.id)}
                >
                  <span className="channel-hash">{c.kind === 'voice' ? '🎙' : '#'}</span>
                  <span className="channel-name">{c.name}</span>
                </button>
              ))}
              {(activeServerInfo.role === 'owner' || activeServerInfo.role === 'admin') && (
                <>
                  {newChannelOpen ? (
                    <div className="new-channel-row">
                      <input
                        autoFocus
                        placeholder="channel-name"
                        value={newChannelName}
                        onChange={(e) => setNewChannelName(e.target.value)}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter') createChannel()
                          if (e.key === 'Escape') {
                            setNewChannelOpen(false)
                            setNewChannelName('')
                          }
                        }}
                      />
                      <button type="button" className="mini-btn" onClick={createChannel}>
                        Add
                      </button>
                    </div>
                  ) : (
                    <button
                      type="button"
                      className="channel-row add"
                      onClick={() => setNewChannelOpen(true)}
                    >
                      <span className="channel-hash">+</span>
                      <span className="channel-name">Add channel</span>
                    </button>
                  )}
                </>
              )}
            </div>
            <footer className="channel-pane-foot">
              {activeServerInfo.role !== 'owner' && (
                <button type="button" className="leave-btn" onClick={leaveServer}>
                  Leave space
                </button>
              )}
            </footer>
          </>
        ) : (
          <div className="empty-pane">
            <p className="empty-title">No space selected</p>
            <p className="empty-hint">
              Create your first <strong>Space</strong> or paste an invite code to join one.
            </p>
            <div className="empty-cta">
              <button type="button" onClick={() => setComposerOpen('create')}>
                + Create a space
              </button>
              <button type="button" className="ghost-btn" onClick={() => setComposerOpen('join')}>
                Join with code
              </button>
            </div>
          </div>
        )}
      </section>

      <section className="chat-pane">
        {activeChannelInfo && activeChannelInfo.kind === 'voice' ? (
          <VoiceRoom
            key={activeChannelInfo.id}
            me={me}
            channelId={activeChannelInfo.id}
            channelName={activeChannelInfo.name}
            send={send}
            subscribe={subscribe}
            turn={turn}
          />
        ) : activeChannelInfo ? (
          <>
            <header className="chat-head">
              <h2>
                <span className="hash">{activeChannelInfo.kind === 'voice' ? '🎙' : '#'}</span>
                {activeChannelInfo.name}
              </h2>
              {activeChannelInfo.topic && <p className="chat-topic">{activeChannelInfo.topic}</p>}
              <div className="chat-head-actions">
                <button type="button" className="chat-head-btn" title="Search" onClick={() => setSearchOpen((v) => !v)}>🔍</button>
                <button type="button" className="chat-head-btn" title="Pinned messages" onClick={() => setPinsOpen((v) => !v)}>
                  📌{(() => { const pins = (messagesByChannel[activeChannel!] ?? []).filter((m) => m.pinned); return pins.length > 0 ? <span className="head-badge">{pins.length}</span> : null })()}
                </button>
                <button type="button" className={`chat-head-btn ${membersOpen ? 'active' : ''}`} title="Members" onClick={() => setMembersOpen((v) => !v)}>
                  👥{members.length > 0 ? <span className="head-badge">{members.length}</span> : null}
                </button>
              </div>
            </header>

            {searchOpen && (
              <div className="spaces-search-bar">
                <input
                  autoFocus
                  placeholder="Search in channel…"
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  onKeyDown={(e) => { if (e.key === 'Escape') { setSearchOpen(false); setSearch('') } }}
                />
                {search && (
                  <span className="muted small">{(messagesByChannel[activeChannel!] ?? []).filter((m) => !m.deleted && m.body.toLowerCase().includes(search.toLowerCase())).length} matches</span>
                )}
                <button type="button" className="ghost-btn" onClick={() => { setSearch(''); setSearchOpen(false) }}>Close</button>
              </div>
            )}

            {pinsOpen && (() => {
              const pins = (messagesByChannel[activeChannel!] ?? []).filter((m) => m.pinned)
              return pins.length > 0 ? (
                <div className="spaces-pin-strip">
                  <div className="pin-strip-title">📌 Pinned</div>
                  {pins.map((m) => (
                    <div key={m.id} className="pin-strip-item">
                      <span className="muted small">{m.sender}:</span> {decodeFile(m.body) ? `📎 ${decodeFile(m.body)!.name}` : m.body.slice(0, 100)}
                    </div>
                  ))}
                </div>
              ) : null
            })()}

            {editingId && (
              <div className="spaces-edit-banner">
                <span>Editing message</span>
                <button type="button" className="ghost-btn" onClick={cancelEdit}>Cancel</button>
              </div>
            )}

            <div className="chat-stream">
              {(() => {
                const allMsgs = messagesByChannel[activeChannel!] ?? []
                const q = search.trim().toLowerCase()
                const view = q ? allMsgs.filter((m) => !m.deleted && m.body.toLowerCase().includes(q)) : allMsgs
                return view.map((m, idx, arr) => {
                const prev = idx > 0 ? arr[idx - 1] : null
                const groupHead = !prev || prev.sender !== m.sender || gapMins(prev.created_at, m.created_at) > 5
                const mentionsMe = containsMention(m.body, me)
                return (
                  <div
                    key={m.id}
                    className={`chat-msg ${m.sender === me ? 'me' : ''} ${groupHead ? 'head' : 'cont'} ${mentionsMe ? 'mentions-me' : ''}`}
                  >
                    {groupHead && (
                      <div className="chat-msg-meta">
                        <span
                          className={`avatar ${onUserClick ? 'clickable' : ''}`}
                          data-initial={initials(m.sender)}
                          onClick={() => onUserClick?.(m.sender)}
                          role={onUserClick ? 'button' : undefined}
                        />
                        <span
                          className={`sender ${onUserClick ? 'clickable' : ''}`}
                          onClick={() => onUserClick?.(m.sender)}
                        >{m.sender}</span>
                        <span className="ts">{formatTs(m.created_at)}</span>
                      </div>
                    )}
                    <div className={`chat-msg-body ${m.pinned ? 'pinned' : ''} ${m.deleted ? 'deleted' : ''}`}>
                      {m.deleted ? (
                        <span className="msg-deleted">message deleted</span>
                      ) : (() => {
                        const f = decodeFile(m.body)
                        if (!f) return <><RichText text={m.body} me={me} />{m.edited && <span className="edited-tag"> (edited)</span>}</>
                        if (isImage(f.mime, f.name)) {
                          return (
                            <a href={f.url} target="_blank" rel="noopener noreferrer">
                              <img src={f.url} alt={f.name} loading="lazy" className="chat-img" />
                            </a>
                          )
                        }
                        if (isVideo(f.mime, f.name)) {
                          return <video controls preload="metadata" src={f.url} className="chat-video" />
                        }
                        return (
                          <a href={f.url} target="_blank" rel="noopener noreferrer" className="chat-file">
                            📎 {f.name} <span className="chat-file-size">({fmtBytes(f.size)})</span>
                          </a>
                        )
                      })()}
                    </div>
                    {m.reactions && Object.keys(m.reactions).length > 0 && (
                      <div className="msg-reactions">
                        {Object.entries(m.reactions).map(([emoji, users]) => (
                          <button
                            key={emoji}
                            type="button"
                            className={`msg-react-chip ${users.includes(me) ? 'mine' : ''}`}
                            onClick={() => send({ type: 'channel_react', channel_id: activeChannel!, msg_id: String(m.id), reaction: emoji })}
                            title={users.join(', ')}
                          >{emoji} {users.length}</button>
                        ))}
                      </div>
                    )}
                    {!m.deleted && (
                      <div className="msg-actions">
                        {['👍','❤️','😂','🔥','😢'].map((e) => (
                          <button key={e} type="button" className="msg-act"
                            onClick={() => send({ type: 'channel_react', channel_id: activeChannel!, msg_id: String(m.id), reaction: e })}
                            title={`React ${e}`}>{e}</button>
                        ))}
                        <button type="button" className="msg-act"
                          onClick={() => send({ type: 'channel_pin', channel_id: activeChannel!, msg_id: String(m.id) })}
                          title={m.pinned ? 'Unpin' : 'Pin'}>{m.pinned ? '📍' : '📌'}</button>
                        {m.sender === me && !decodeFile(m.body) && (
                          <button type="button" className="msg-act"
                            onClick={() => beginEdit(m)}
                            title="Edit">✏️</button>
                        )}
                        {m.sender === me && (
                          <button type="button" className="msg-act"
                            onClick={() => {
                              if (confirm('Delete this message?')) {
                                send({ type: 'channel_delete', channel_id: activeChannel!, msg_id: String(m.id) })
                              }
                            }}
                            title="Delete">🗑</button>
                        )}
                      </div>
                    )}
                  </div>
                )
              })
              })()}
              <div ref={chatBottomRef} />
            </div>
            <footer className="chat-composer">
              {uploadAttachment && (
                <>
                  <input
                    ref={fileInputRef}
                    type="file"
                    style={{ display: 'none' }}
                    onChange={(e) => {
                      const f = e.target.files?.[0]
                      if (f) void submitFile(f)
                      e.target.value = ''
                    }}
                  />
                  <button
                    type="button"
                    onClick={() => fileInputRef.current?.click()}
                    className="attach-btn"
                    aria-label="Attach file"
                    title="Attach file"
                  >📎</button>
                </>
              )}
              <button
                type="button"
                onClick={() => setEmojiOpen((v) => !v)}
                className="attach-btn"
                aria-label="Insert emoji"
                title="Emoji"
              >😀</button>
              {emojiOpen && (
                <div className="emoji-picker">
                  {CHANNEL_EMOJIS.map((e) => (
                    <button
                      key={e}
                      type="button"
                      className="emoji-cell"
                      onClick={() => { setDraft((d) => d + e); setEmojiOpen(false) }}
                    >{e}</button>
                  ))}
                </div>
              )}
              <div className="composer-wrap">
                <textarea
                  ref={draftInputRef}
                  placeholder={editingId ? 'Edit message…' : `Message #${activeChannelInfo.name}  ·  @ to mention`}
                  value={editingId ? editDraft : draft}
                  onChange={(e) => {
                    if (editingId) setEditDraft(e.target.value)
                    else handleDraftChange(e)
                  }}
                  onKeyDown={(e) => {
                    if (mentionMatches.length > 0 && mentionQuery !== null) {
                      if (e.key === 'ArrowDown') { e.preventDefault(); setMentionIdx((i) => (i + 1) % mentionMatches.length); return }
                      if (e.key === 'ArrowUp') { e.preventDefault(); setMentionIdx((i) => (i - 1 + mentionMatches.length) % mentionMatches.length); return }
                      if (e.key === 'Tab' || e.key === 'Enter') { e.preventDefault(); completeMention(mentionMatches[mentionIdx]); return }
                      if (e.key === 'Escape') { setMentionQuery(null); return }
                    }
                    if (e.key === 'Enter' && !e.shiftKey) {
                      e.preventDefault()
                      if (editingId) saveEdit()
                      else submitMessage()
                    }
                    if (e.key === 'Escape' && editingId) cancelEdit()
                  }}
                  rows={1}
                />
                {mentionMatches.length > 0 && mentionQuery !== null && (
                  <div className="mention-pop">
                    {mentionMatches.map((u, i) => (
                      <button
                        type="button"
                        key={u}
                        className={`mention-row ${i === mentionIdx ? 'on' : ''}`}
                        onMouseDown={(e) => { e.preventDefault(); completeMention(u) }}
                        onMouseEnter={() => setMentionIdx(i)}
                      >
                        <span className="avatar small" data-initial={u[0]?.toUpperCase()} />
                        <span>{u}</span>
                      </button>
                    ))}
                  </div>
                )}
              </div>
              <button
                type="button"
                onClick={() => { if (editingId) saveEdit(); else submitMessage() }}
                disabled={editingId ? !editDraft.trim() : !draft.trim()}
                className="send-btn"
                aria-label={editingId ? 'Save' : 'Send'}
              >
                {editingId ? '✓' : '➤'}
              </button>
            </footer>
          </>
        ) : (
          <div className="empty-pane lg">
            <p className="empty-title">Pick a channel</p>
            <p className="empty-hint">Channels in this space appear in the middle pane.</p>
          </div>
        )}
      </section>

      {membersOpen && activeServer && (
        <aside className="members-sidebar">
          <h3 className="members-title">Members — {members.length}</h3>
          <ul className="members-list">
            {members.map((u) => (
              <li key={u} className="member-row" onClick={() => onUserClick?.(u)}>
                <span className="avatar small" data-initial={u[0]?.toUpperCase()} />
                <span className="member-name">{u}{u === me ? ' (you)' : ''}</span>
              </li>
            ))}
            {members.length === 0 && <li className="member-empty">No members loaded</li>}
          </ul>
        </aside>
      )}

      {composerOpen === 'create' && (
        <div className="modal-scrim" onClick={() => setComposerOpen(null)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <h3>Create a space</h3>
            <p className="modal-hint">Spaces are persistent group chats with channels.</p>
            <input
              placeholder="Space name (e.g. Bruh Mode)"
              value={newName}
              autoFocus
              onChange={(e) => setNewName(e.target.value)}
            />
            <input
              placeholder="What's it about? (optional)"
              value={newTopic}
              onChange={(e) => setNewTopic(e.target.value)}
            />
            <div className="vis-row">
              {(['private', 'public'] as const).map((v) => (
                <button
                  key={v}
                  type="button"
                  className={`vis-btn ${newVisibility === v ? 'on' : ''}`}
                  onClick={() => setNewVisibility(v)}
                >
                  {v === 'private' ? '🔒 Private' : '🌐 Public'}
                </button>
              ))}
            </div>
            <div className="modal-actions">
              <button type="button" className="ghost-btn" onClick={() => setComposerOpen(null)}>
                Cancel
              </button>
              <button type="button" onClick={createServer}>
                Create
              </button>
            </div>
          </div>
        </div>
      )}

      {composerOpen === 'join' && (
        <div className="modal-scrim" onClick={() => setComposerOpen(null)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <h3>Join a space</h3>
            <p className="modal-hint">Paste an invite code from a space owner.</p>
            <input
              placeholder="Invite code"
              value={joinCode}
              autoFocus
              onChange={(e) => setJoinCode(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && joinServer()}
            />
            <div className="modal-actions">
              <button type="button" className="ghost-btn" onClick={() => setComposerOpen(null)}>
                Cancel
              </button>
              <button type="button" onClick={joinServer}>
                Join
              </button>
            </div>
          </div>
        </div>
      )}

      {composerOpen === 'discover' && (
        <div className="modal-scrim" onClick={() => setComposerOpen(null)}>
          <div className="modal discover-modal" onClick={(e) => e.stopPropagation()}>
            <h3>Discover public spaces</h3>
            <p className="modal-hint">Browse and join open communities — no invite needed.</p>
            <div className="discover-list">
              {discoverLoading && <p className="modal-hint">Loading…</p>}
              {!discoverLoading && discoverList.length === 0 && (
                <p className="modal-hint">No public spaces yet. Create one and set it to public!</p>
              )}
              {discoverList.map((s) => (
                <div className="discover-row" key={s.id}>
                  <span className="discover-icon">{initials(s.name)}</span>
                  <div className="discover-meta">
                    <span className="discover-name">{s.name}</span>
                    <span className="discover-sub">
                      {(s.member_count ?? 0)} member{(s.member_count ?? 0) === 1 ? '' : 's'}
                      {s.description ? ` · ${s.description}` : ''}
                    </span>
                  </div>
                  {s.is_member ? (
                    <button type="button" className="ghost-btn" disabled>
                      Joined
                    </button>
                  ) : (
                    <button type="button" onClick={() => joinPublic(s.id)}>
                      Join
                    </button>
                  )}
                </div>
              ))}
            </div>
            <div className="modal-actions">
              <button type="button" className="ghost-btn" onClick={() => setComposerOpen(null)}>
                Close
              </button>
            </div>
          </div>
        </div>
      )}

      <div className="toast-stack" aria-live="polite">
        {toasts.map((t) => (
          <div key={t.id} className={`toast t-${t.tone}`}>
            {t.body}
          </div>
        ))}
      </div>
    </div>
  )
}

function initials(name: string): string {
  if (!name) return '?'
  const parts = name.trim().split(/\s+/)
  if (parts.length === 1) return name.slice(0, 2).toUpperCase()
  return (parts[0][0] + parts[1][0]).toUpperCase()
}

function gapMins(a: string, b: string): number {
  const ta = Date.parse(a)
  const tb = Date.parse(b)
  if (Number.isNaN(ta) || Number.isNaN(tb)) return 0
  return Math.abs(tb - ta) / 60000
}

function formatTs(iso: string): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return iso
  const d = new Date(t)
  const now = new Date()
  const sameDay = d.toDateString() === now.toDateString()
  const yesterday = new Date(now.getTime() - 86400000).toDateString() === d.toDateString()
  const hhmm = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  if (sameDay) return hhmm
  if (yesterday) return `Yesterday ${hhmm}`
  return `${d.toLocaleDateString([], { month: 'short', day: 'numeric' })} ${hhmm}`
}

function SkypeMark() {
  return (
    <svg viewBox="0 0 32 32" width="22" height="22" aria-hidden="true">
      <defs>
        <radialGradient id="phzCore" cx="50%" cy="40%" r="60%">
          <stop offset="0%" stopColor="#9be8ff" />
          <stop offset="55%" stopColor="#00aff0" />
          <stop offset="100%" stopColor="#005d99" />
        </radialGradient>
      </defs>
      <circle cx="16" cy="16" r="13" fill="url(#phzCore)" />
      <path d="M11 11c2.5-1.5 7-1.5 9 0 2 1.5 1.5 4-1 4.5l-3 .5c-1.5.3-2 1-1 1.7 1 .8 4 .6 5.5-.6"
            stroke="#fff" strokeWidth="2" fill="none" strokeLinecap="round" />
    </svg>
  )
}
