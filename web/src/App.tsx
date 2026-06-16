import { lazy, Suspense, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, startTransition } from 'react'
import faviconUrl from '/icon-192.png'
import jsQR from 'jsqr'
import type { NexusMessage, TurnConfig } from './nexusTypes'
import {
  decryptFromPeer,
  decodePublicKeyField,
  encryptForPeer,
  encodePublicKeyB64,
  fingerprint,
  generateKeyPair,
} from './e2ee'
import { loadPins, savePins } from './keyPins'
import { decryptKeypair as decryptKeyBackup, encryptKeypair as encryptKeyBackup } from './keyBackup'
import { playPhazeSound } from './phazeSounds'
const Spaces = lazy(() => import('./Spaces'))
const LivePage = lazy(() => import('./LivePage'))
const VoiceRoom = lazy(() => import('./VoiceRoom'))
const Stories = lazy(() => import('./Stories'))
const Onboarding = lazy(() => import('./Onboarding'))
const RemoteControl = lazy(() => import('./RemoteControl'))
import UserProfile from './UserProfile'
import SupportBubble from './SupportBubble'
import SupportForm from './SupportForm'
import Settings from './Settings'
import DesktopTitleBar from './DesktopTitleBar'
import './App.css'

// Wails desktop bridge — only present when running inside the Wails desktop app.
const wails = (() => {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const g = window as any
  if (typeof g?.go?.main?.App?.Notify === 'function') return g.go.main.App as {
    Notify: (title: string, body: string) => void
    SetUnread: (count: number) => void
    WindowMinimise: () => void
    WindowToggleMaximise: () => void
    WindowClose: () => void
  }
  return null
})()

const SESSION_KEY = 'phaze_session_token_v1'
const KEYS_KEY = 'phaze_nacl_keys_v1'
const MUTED_PEERS_KEY = 'phaze_muted_peers_v1'

function loadMutedPeers(): Set<string> {
  try {
    const raw = localStorage.getItem(MUTED_PEERS_KEY)
    if (raw) return new Set(JSON.parse(raw) as string[])
  } catch { /* ignore */ }
  return new Set()
}
function saveMutedPeers(s: Set<string>) {
  try { localStorage.setItem(MUTED_PEERS_KEY, JSON.stringify([...s])) } catch { /* ignore */ }
}
function isPeerMuted(peer: string): boolean {
  return loadMutedPeers().has(peer)
}
const THEME_KEY = 'phaze_theme_v1'
const SNOW_KEY = 'phaze_snow_v1'

/** Pure-CSS seasonal snow overlay (zero deps). */
function Snowflakes() {
  const flakes = Array.from({ length: 40 }, (_, i) => {
    const left = Math.random() * 100
    const dur = 6 + Math.random() * 8
    const delay = -Math.random() * 14
    const size = 0.6 + Math.random() * 1.1
    return (
      <i key={i} style={{ left: `${left}vw`, animationDuration: `${dur}s`, animationDelay: `${delay}s`, fontSize: `${size}rem` }}>❄</i>
    )
  })
  return <div className="snow-layer" aria-hidden="true">{flakes}</div>
}
const BACKUP_NAG_KEY = 'phaze_backup_nag_dismissed_at_v1'
const BACKUP_NAG_COOLDOWN_MS = 7 * 24 * 60 * 60 * 1000
const HISTORY_LIMIT = 500
const historyKey = (me: string, peer: string) => `phaze_chat_${me}_${peer}_v1`
const unreadKey = (me: string) => `phaze_unread_${me}_v1`

const EMOJIS = ['😀','😂','😍','😎','🤔','😢','😡','👍','👎','❤️','🔥','🎉','🙏','👀','💯','✨','😅','🥹','😴','🤝','🚀','👋','🤣','😭']

function avatarColor(name: string): string {
  let h = 0
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) >>> 0
  const hue = h % 360
  return `hsl(${hue} 65% 50%)`
}

function formatTime(ts: number): string {
  const d = new Date(ts)
  const now = new Date()
  const sameDay = d.toDateString() === now.toDateString()
  if (sameDay) return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  const week = 7 * 24 * 60 * 60 * 1000
  if (now.getTime() - ts < week) return d.toLocaleDateString([], { weekday: 'short' }) + ' ' + d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  return d.toLocaleDateString([], { month: 'short', day: 'numeric' })
}

function loadHistory(me: string, peer: string): ChatLine[] {
  try {
    const raw = localStorage.getItem(historyKey(me, peer))
    if (!raw) return []
    const arr = JSON.parse(raw) as ChatLine[]
    return Array.isArray(arr) ? arr : []
  } catch { return [] }
}

function saveHistory(me: string, peer: string, lines: ChatLine[]) {
  try {
    const trimmed = lines.slice(-HISTORY_LIMIT)
    localStorage.setItem(historyKey(me, peer), JSON.stringify(trimmed))
  } catch { /* quota */ }
}

function loadUnread(me: string): Record<string, number> {
  try {
    const raw = localStorage.getItem(unreadKey(me))
    return raw ? (JSON.parse(raw) as Record<string, number>) : {}
  } catch { return {} }
}

function saveUnread(me: string, u: Record<string, number>) {
  try { localStorage.setItem(unreadKey(me), JSON.stringify(u)) } catch { /* quota */ }
}

async function registerPush(send: (m: NexusMessage) => void) {
  if (!('serviceWorker' in navigator) || !('PushManager' in window)) return
  try {
    const reg = await navigator.serviceWorker.register('/web/sw.js', { scope: '/web/' })
    const perm = await Notification.requestPermission()
    if (perm !== 'granted') return
    const resp = await fetch('/api/v1/vapid-key')
    if (!resp.ok) return
    const { publicKey } = await resp.json() as { publicKey: string }
    if (!publicKey) return
    const sub = await reg.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: publicKey,
    })
    const json = sub.toJSON()
    send({
      type: 'subscribe_push',
      body: JSON.stringify({
        endpoint: json.endpoint,
        p256dh: json.keys?.p256dh,
        auth: json.keys?.auth,
      }),
    })
  } catch (e) {
    console.warn('[push] registration failed', e)
  }
}

type FileAttachment = { url: string; name: string; mime: string; size: number }
type ChatLine = {
  id: string
  from: string
  text: string
  me: boolean
  ts: number
  edited?: boolean
  deleted?: boolean
  reactions?: Record<string, string[]> // emoji -> users
  file?: FileAttachment
  seen?: boolean // peer has opened the conversation since this message was sent
}

const FILE_PREFIX = 'phaze-file'

function encodeFileBody(att: FileAttachment): string {
  return FILE_PREFIX + JSON.stringify(att)
}

function decodeFileBody(text: string): FileAttachment | null {
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

function isAudio(mime: string, name: string): boolean {
  if (mime?.startsWith('audio/')) return true
  return /\.(mp3|m4a|ogg|wav|opus)$/i.test(name)
}

function isVideo(mime: string, name: string): boolean {
  if (mime?.startsWith('video/')) return true
  return /\.(mp4|mov|webm|mkv|avi)$/i.test(name)
}

function fmtDuration(s: number): string {
  const m = Math.floor(s / 60)
  const sec = Math.floor(s % 60)
  return `${m}:${sec.toString().padStart(2, '0')}`
}

function newMsgId(): string {
  const a = new Uint8Array(12)
  crypto.getRandomValues(a)
  return Array.from(a, (b) => b.toString(16).padStart(2, '0')).join('')
}

function fmtBytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / (1024 * 1024)).toFixed(1)} MB`
}

const REACTION_EMOJIS = ['👍','❤️','😂','😮','😢','🔥']

interface SlashCmd {
  cmd: string
  desc: string
  // run returns the replacement text to send. Empty string means do nothing.
  run: (arg: string) => string | { local: string }
}

const SLASH_COMMANDS: SlashCmd[] = [
  { cmd: '/me', desc: 'Send a third-person action message', run: (a) => `*${a}*` },
  { cmd: '/shrug', desc: 'Append ¯\\_(ツ)_/¯', run: (a) => `${a} ¯\\_(ツ)_/¯`.trim() },
  { cmd: '/tableflip', desc: 'Flip a table', run: (a) => `${a} (╯°□°）╯︵ ┻━┻`.trim() },
  { cmd: '/unflip', desc: 'Put the table back', run: (a) => `${a} ┬─┬ ノ( ゜-゜ノ)`.trim() },
  { cmd: '/lenny', desc: 'Send a lenny face', run: (a) => `${a} ( ͡° ͜ʖ ͡°)`.trim() },
  { cmd: '/clear', desc: 'Clear the conversation view (local only)', run: () => ({ local: 'clear' }) },
  { cmd: '/help', desc: 'List available slash commands', run: () => ({ local: 'help' }) },
]

function relTime(ts: number): string {
  const diff = Date.now() - ts
  if (diff < 60_000) return 'now'
  if (diff < 60 * 60_000) return `${Math.floor(diff / 60_000)}m`
  if (diff < 24 * 60 * 60_000) return `${Math.floor(diff / (60 * 60_000))}h`
  const d = new Date(ts)
  const days = Math.floor(diff / (24 * 60 * 60_000))
  if (days < 7) return `${days}d`
  return d.toLocaleDateString([], { month: 'short', day: 'numeric' })
}

function lastLineFor(me: string | null, peer: string): { text: string; ts: number } | null {
  if (!me) return null
  try {
    const raw = localStorage.getItem(`phaze_chat_${me}_${peer}_v1`)
    if (!raw) return null
    const arr = JSON.parse(raw) as { text: string; ts: number; file?: { name?: string }; deleted?: boolean; me?: boolean }[]
    if (!arr.length) return null
    const last = arr[arr.length - 1]
    let text = last.text || ''
    if (last.deleted) text = '[deleted]'
    else if (last.file?.name) text = `📎 ${last.file.name}`
    if (last.me) text = `You: ${text}`
    return { text, ts: last.ts }
  } catch { return null }
}

const MENTION_RE = /@([A-Za-z0-9_]{2,32})/g

const pinKey = (me: string, peer: string) => `phaze_pins_${me}_${peer}_v1`

function loadPinned(me: string, peer: string): string[] {
  try {
    const raw = localStorage.getItem(pinKey(me, peer))
    return raw ? JSON.parse(raw) as string[] : []
  } catch { return [] }
}

function savePinned(me: string, peer: string, ids: string[]) {
  try { localStorage.setItem(pinKey(me, peer), JSON.stringify(ids)) } catch { /* quota */ }
}

type Segment = { kind: 'text' | 'url' | 'mention'; value: string }

function tokenize(text: string): Segment[] {
  if (!text) return []
  const out: Segment[] = []
  // Combined regex preserves order across both URL and @mention matches.
  const combined = /(https?:\/\/[^\s<>"']+[^\s<>"'.,!?:;)])|@([A-Za-z0-9_]{2,32})/g
  let last = 0
  let m: RegExpExecArray | null
  while ((m = combined.exec(text)) !== null) {
    if (m.index > last) out.push({ kind: 'text', value: text.slice(last, m.index) })
    if (m[1]) out.push({ kind: 'url', value: m[1] })
    else if (m[2]) out.push({ kind: 'mention', value: m[2] })
    last = m.index + m[0].length
  }
  if (last < text.length) out.push({ kind: 'text', value: text.slice(last) })
  return out
}

function RichText({ text, me }: { text: string; me: string | null }) {
  const segs = useMemo(() => tokenize(text), [text])
  return (
    <>
      {segs.map((s, i) => {
        if (s.kind === 'url') {
          return <a key={i} href={s.value} target="_blank" rel="noopener noreferrer" className="msg-link">{s.value}</a>
        }
        if (s.kind === 'mention') {
          const isMe = me === s.value
          return <span key={i} className={`msg-mention ${isMe ? 'me' : ''}`}>@{s.value}</span>
        }
        return <span key={i}>{s.value}</span>
      })}
    </>
  )
}

function containsMention(text: string, who: string): boolean {
  if (!text || !who) return false
  MENTION_RE.lastIndex = 0
  let m: RegExpExecArray | null
  while ((m = MENTION_RE.exec(text)) !== null) {
    if (m[1] === who) return true
  }
  return false
}
type CallState = {
  peer: string
  type: 'audio' | 'video'
  status: 'ringing' | 'active'
  direction: 'outgoing' | 'incoming'
}

function defaultWsUrl(): string {
  const u = import.meta.env.VITE_NEXUS_WS as string | undefined
  if (u) return u
  const { protocol, hostname, port } = window.location
  const p = protocol === 'https:' ? 'wss:' : 'ws:'
  const h = port && protocol !== 'https:' ? `${hostname}:${port}` : hostname
  return `${p}//${h}/ws`
}

function loadOrCreateKeys(): { publicKey: Uint8Array; secretKey: Uint8Array } {
  try {
    const raw = localStorage.getItem(KEYS_KEY)
    if (raw) {
      const j = JSON.parse(raw) as { pub: string; sec: string }
      const pub = Uint8Array.from(atob(j.pub), (c) => c.charCodeAt(0))
      const sec = Uint8Array.from(atob(j.sec), (c) => c.charCodeAt(0))
      if (pub.length === 32 && sec.length === 32) return { publicKey: pub, secretKey: sec }
    }
  } catch {
    /* fallthrough */
  }
  const kp = generateKeyPair()
  localStorage.setItem(
    KEYS_KEY,
    JSON.stringify({
      pub: btoa(String.fromCharCode(...kp.publicKey)),
      sec: btoa(String.fromCharCode(...kp.secretKey)),
    }),
  )
  return kp
}

function statusColor(st: string): string {
  if (st === 'Online') return '#22c55e'
  if (st === 'Away' || st === 'away') return '#f59e0b'
  if (st === 'Do Not Disturb' || st === 'dnd') return '#ef4444'
  return '#94a3b8'
}

export default function App() {
  const wsUrl = useMemo(() => defaultWsUrl(), [])
  const [conn, setConn] = useState<'off' | 'connecting' | 'open'>('off')
  const [wsRetry, setWsRetry] = useState(0)
  const wsRetryDelay = useRef(1000)
  const [me, setMe] = useState<string | null>(null)
  const [err, setErr] = useState('')
  const [log, setLog] = useState<ChatLine[]>([])
  const [friends, setFriends] = useState<Record<string, string>>({})
  const [selected, setSelected] = useState<string | null>(null)
  const [pending, setPending] = useState<string[]>([])
  const [draft, setDraft] = useState('')
  const [turn, setTurn] = useState<TurnConfig | null>(null)
  const [e2eReady, setE2eReady] = useState(false)
  const [typingPeers, setTypingPeers] = useState<Set<string>>(new Set())
  const [callState, setCallState] = useState<CallState | null>(null)
  const [callSeconds, setCallSeconds] = useState(0)
  const [callMuted, setCallMuted] = useState(false)
  const callTimerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  // Auth + registration UI state hoisted above WS handler (ESLint no-use-before-define)
  const [loginUser, setLoginUser] = useState('')
  const [loginPass, setLoginPass] = useState('')
  const [loginTotp, setLoginTotp] = useState('')
  const [needsTotp, setNeedsTotp] = useState(false)
  const [addFriend, setAddFriend] = useState('')
  const [profileUser, setProfileUser] = useState<string | null>(null)
  const [onboardingOpen, setOnboardingOpen] = useState<boolean>(() => {
    try { return localStorage.getItem('phaze_onboarded') !== '1' } catch { return false }
  })
  const [mutedPeers, setMutedPeers] = useState<Set<string>>(() => loadMutedPeers())
  const [bmcUrl, setBmcUrl] = useState('https://buymeacoffee.com/phazeworld')
  const [showSupport, setShowSupport] = useState(false)
  useEffect(() => {
    fetch('/api/v1/config')
      .then((r) => r.ok ? r.json() : null)
      .then((c: { bmc_url?: string } | null) => { if (c?.bmc_url) setBmcUrl(c.bmc_url) })
      .catch(() => { /* keep default */ })
  }, [])
  const togglePeerMute = (peer: string) => {
    setMutedPeers((prev) => {
      const next = new Set(prev)
      if (next.has(peer)) next.delete(peer); else next.add(peer)
      saveMutedPeers(next)
      return next
    })
  }
  const inviteCode = useMemo(() => new URLSearchParams(window.location.search).get('invite'), [])
  const [mode, setMode] = useState<'login' | 'register' | 'link' | 'forgot'>(() => (new URLSearchParams(window.location.search).get('invite') ? 'register' : 'login'))
  const [forgotEmail, setForgotEmail] = useState('')
  const [linkInput, setLinkInput] = useState('')
  const [linkBusy, setLinkBusy] = useState(false)
  const [cameraActive, setCameraActive] = useState(false)
  const videoRef = useRef<HTMLVideoElement | null>(null)
  const streamRef = useRef<MediaStream | null>(null)
  const animationFrameIdRef = useRef<number | null>(null)

  useEffect(() => {
    return () => {
      if (streamRef.current) {
        streamRef.current.getTracks().forEach(track => track.stop())
      }
      if (animationFrameIdRef.current) {
        cancelAnimationFrame(animationFrameIdRef.current)
      }
    }
  }, [])

  const startCamera = async () => {
    try {
      setErr('')
      setCameraActive(true)
      const stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: 'environment' } })
      streamRef.current = stream
      if (videoRef.current) {
        videoRef.current.srcObject = stream
        videoRef.current.setAttribute('playsinline', 'true')
        videoRef.current.play()
        animationFrameIdRef.current = requestAnimationFrame(tick)
      }
    } catch (err) {
      setCameraActive(false)
      setErr('Camera access failed: ' + (err instanceof Error ? err.message : String(err)))
    }
  }

  const stopCamera = () => {
    setCameraActive(false)
    if (streamRef.current) {
      streamRef.current.getTracks().forEach(track => track.stop())
      streamRef.current = null
    }
    if (animationFrameIdRef.current) {
      cancelAnimationFrame(animationFrameIdRef.current)
      animationFrameIdRef.current = null
    }
  }

  const tick = () => {
    if (!videoRef.current || videoRef.current.readyState !== videoRef.current.HAVE_ENOUGH_DATA) {
      animationFrameIdRef.current = requestAnimationFrame(tick)
      return
    }

    const canvas = document.createElement('canvas')
    const ctx = canvas.getContext('2d')
    if (ctx) {
      canvas.width = videoRef.current.videoWidth
      canvas.height = videoRef.current.videoHeight
      ctx.drawImage(videoRef.current, 0, 0, canvas.width, canvas.height)
      const imageData = ctx.getImageData(0, 0, canvas.width, canvas.height)
      const code = jsQR(imageData.data, imageData.width, imageData.height, {
        inversionAttempts: 'dontInvert',
      })
      if (code && code.data) {
        handleScannedToken(code.data)
        return
      }
    }
    animationFrameIdRef.current = requestAnimationFrame(tick)
  }

  const handleScannedToken = (val: string) => {
    let tok = val.trim()
    if (tok.includes('token=')) {
      tok = tok.split('token=')[1].split('&')[0]
    }
    if (!/^[a-f0-9]{32,128}$/.test(tok)) { setErr('Invalid QR code format'); return }
    setLinkInput(tok)
    stopCamera()
    setErr('✓ Token scanned: ' + tok)
    setLinkBusy(true)
    const poll = setInterval(() => sendRef.current({ type: 'link_check', token: tok }), 2500)
    sendRef.current({ type: 'link_check', token: tok })
    setTimeout(() => clearInterval(poll), 5 * 60 * 1000)
  }

  const handleFileUpload = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    const reader = new FileReader()
    reader.onload = (event) => {
      const img = new Image()
      img.onload = () => {
        const canvas = document.createElement('canvas')
        const ctx = canvas.getContext('2d')
        if (ctx) {
          canvas.width = img.width
          canvas.height = img.height
          ctx.drawImage(img, 0, 0)
          const imageData = ctx.getImageData(0, 0, canvas.width, canvas.height)
          const code = jsQR(imageData.data, imageData.width, imageData.height)
          if (code && code.data) {
            handleScannedToken(code.data)
          } else {
            setErr('No QR code found in the selected image.')
          }
        }
      }
      img.src = event.target?.result as string
    }
    reader.readAsDataURL(file)
  }
  const [regStep, setRegStep] = useState<'form' | 'verify' | 'done'>('form')
  const [regUser, setRegUser] = useState('')
  const [regEmail, setRegEmail] = useState('')
  const [regPass, setRegPass] = useState('')
  const [regCode, setRegCode] = useState('')

  const [view, setView] = useState<'dms' | 'spaces' | 'live'>('dms')
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [remoteOpen, setRemoteOpen] = useState(false)
  const [groupCallRoom, setGroupCallRoom] = useState<string | null>(null)
  const [groupCallInvite, setGroupCallInvite] = useState<{ from: string; room: string } | null>(null)
  const [globalNotice, setGlobalNotice] = useState<{ from: string; msg: string } | null>(null)
  const [changelogSeen, setChangelogSeen] = useState(() => localStorage.getItem('phaze_changelog_v') === '2025-05-25')
  const [changelogOpen, setChangelogOpen] = useState(false)
  const [changelogSlide, setChangelogSlide] = useState(0)
  const changelogFeatures = [
    { icon: '🖥', title: 'Remote Control', desc: 'Share your screen and let friends take control — like TeamViewer, but encrypted and built right into Phaze. No extra apps needed.', color: '#7c3aed' },
    { icon: '👥', title: 'Group Calls', desc: 'Start a group voice or video call with multiple friends at once. Just click the group call button in any chat.', color: '#2563eb' },
    { icon: '🌐', title: 'Spaces Upgrade', desc: '@mentions with autocomplete, in-channel search, pinned messages, and inline editing. Spaces just got serious.', color: '#059669' },
    { icon: '🔴', title: 'Live Streaming', desc: 'Go live with your camera or broadcast your screen. Anyone on Phaze can tune in and watch.', color: '#dc2626' },
    { icon: '🎁', title: 'Invite Friends', desc: 'Share your personal invite link or send branded email invitations. The more friends you bring, the better Phaze gets.', color: '#d97706' },
    { icon: '📞', title: 'Better Calls', desc: 'Screen sharing in any call, self-hosted TURN server for reliable connections, and improved audio quality.', color: '#0891b2' },
    { icon: '✨', title: 'Redesigned', desc: 'True-black dark mode, premium glass effects, and a brand-new landing page. Phaze looks like it feels — premium.', color: '#a855f7' },
  ]
  const [sessionToken, setSessionToken] = useState<string | null>(() => localStorage.getItem(SESSION_KEY))
  const [theme, setTheme] = useState<'light' | 'dark' | 'skype7'>(() => (localStorage.getItem(THEME_KEY) as 'light' | 'dark' | 'skype7') || 'dark')
  const [snow, setSnow] = useState<boolean>(() => localStorage.getItem(SNOW_KEY) === '1')
  const [unread, setUnread] = useState<Record<string, number>>({})
  const [emojiOpen, setEmojiOpen] = useState(false)
  const unreadRef = useRef<Record<string, number>>({})
  const chatScrollRef = useRef<HTMLDivElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const draftInputRef = useRef<HTMLInputElement>(null)
  const [restoreBackup, setRestoreBackup] = useState<import('./nexusTypes').KeyBackup | null>(null)
  const [restorePin, setRestorePin] = useState('')
  const [restoreBusy, setRestoreBusy] = useState(false)
  const restoreCheckedRef = useRef(false)
  const [showBackupNag, setShowBackupNag] = useState(false)
  const [paletteOpen, setPaletteOpen] = useState(false)
  const [paletteQuery, setPaletteQuery] = useState('')
  const [globalSearchResults, setGlobalSearchResults] = useState<string[]>([])
  const globalSearchTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const [paletteIdx, setPaletteIdx] = useState(0)
  const [slashIdx, setSlashIdx] = useState(0)
  const [recording, setRecording] = useState(false)
  const [recDuration, setRecDuration] = useState(0)
  const recorderRef = useRef<MediaRecorder | null>(null)
  const recStartRef = useRef<number>(0)
  const recTimerRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const recChunksRef = useRef<Blob[]>([])
  const [settingsInitialTab, setSettingsInitialTab] = useState<'profile' | 'security' | 'devices' | 'privacy' | 'sessions' | 'danger' | 'notifications'>('profile')
  const [reportTarget, setReportTarget] = useState<string | null>(null)
  const [reportReason, setReportReason] = useState('')
  const [reportSent, setReportSent] = useState(false)
  const [pinnedIds, setPinnedIds] = useState<string[]>([])
  const [pinsOpen, setPinsOpen] = useState(false)
  const [mentionQuery, setMentionQuery] = useState<string | null>(null)
  const [mentionIdx, setMentionIdx] = useState(0)
  const [search, setSearch] = useState('')
  const [searchOpen, setSearchOpen] = useState(false)

  useEffect(() => {
    localStorage.setItem(THEME_KEY, theme)
    document.documentElement.dataset.theme = theme
    // Sync theme preference to server when logged in.
    if (meRef.current) {
      sendRef.current({ type: 'settings_set', sender: meRef.current, body: JSON.stringify({ theme }) })
    }
  }, [theme])

  useEffect(() => {
    localStorage.setItem(SNOW_KEY, snow ? '1' : '0')
  }, [snow])

  useEffect(() => {
    const el = chatScrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [log])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey
      if (mod && (e.key === 'k' || e.key === 'K')) {
        e.preventDefault()
        setPaletteOpen((v) => !v)
        setPaletteQuery('')
        setPaletteIdx(0)
        return
      }
      if (mod && e.key === '/') {
        e.preventDefault()
        draftInputRef.current?.focus()
        return
      }
      if (e.key === 'Escape') {
        if (paletteOpen) setPaletteOpen(false)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [paletteOpen])

  const subscribersRef = useRef(new Set<(m: NexusMessage) => void>())
  const subscribe = useCallback((handler: (m: NexusMessage) => void) => {
    subscribersRef.current.add(handler)
    return () => { subscribersRef.current.delete(handler) }
  }, [])

  const wsRef = useRef<WebSocket | null>(null)
  const keysRef = useRef(loadOrCreateKeys())
  const peerKeysRef = useRef<Record<string, Uint8Array>>({})
  const pinsRef = useRef(loadPins())
  const meRef = useRef<string | null>(null)
  const selectedRef = useRef<string | null>(null)
  const sendRef = useRef<(m: NexusMessage) => void>(() => {})
  const callStateRef = useRef<CallState | null>(null)
  const turnRef = useRef<TurnConfig | null>(null)
  const typingTimersRef = useRef<Record<string, ReturnType<typeof setTimeout>>>({})
  const outTypingTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // WebRTC refs
  const ringTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const pcRef = useRef<RTCPeerConnection | null>(null)
  const localStreamRef = useRef<MediaStream | null>(null)
  const incomingCallSdpRef = useRef<string | null>(null)
  const screenStreamRef = useRef<MediaStream | null>(null)
  const cameraTrackRef = useRef<MediaStreamTrack | null>(null)
  const ingestDMHistoryRef = useRef<(peer: string, rows: import('./nexusTypes').DMMessage[]) => void>(() => {})
  const localVideoRef = useRef<HTMLVideoElement>(null)
  const remoteVideoRef = useRef<HTMLVideoElement>(null)

  useEffect(() => {
    meRef.current = me
    if (me) {
      const u = loadUnread(me)
      unreadRef.current = u
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setUnread(u)
    } else {
      unreadRef.current = {}
      setUnread({})
    }
  }, [me])
  useEffect(() => { selectedRef.current = selected }, [selected])
  useEffect(() => { callStateRef.current = callState }, [callState])
  useEffect(() => { turnRef.current = turn }, [turn])

  const appendLog = useCallback((from: string, text: string, isMe: boolean, opts?: { id?: string; file?: FileAttachment }) => {
    const id = opts?.id || newMsgId()
    const ts = Date.now()
    const file = opts?.file || decodeFileBody(text) || undefined
    const line: ChatLine = { id, from, text: file ? '' : text, me: isMe, ts, file }
    const my = meRef.current
    const peer = isMe ? selectedRef.current : from
    if (my && peer) {
      const existing = loadHistory(my, peer)
      saveHistory(my, peer, [...existing, line])
    }
    if (peer && peer === selectedRef.current) {
      setLog((prev) => [...prev, line])
    }
    if (!isMe && my && peer && peer !== selectedRef.current) {
      const bump = containsMention(line.text, my) ? 2 : 1
      const next = { ...unreadRef.current, [peer]: (unreadRef.current[peer] || 0) + bump }
      unreadRef.current = next
      setUnread(next)
      saveUnread(my, next)
    }
  }, [])

  const mutateMessage = useCallback((peer: string, msgId: string, fn: (l: ChatLine) => ChatLine) => {
    const my = meRef.current
    if (!my) return
    const stored = loadHistory(my, peer)
    let changed = false
    const updated = stored.map((l) => {
      if (l.id !== msgId) return l
      changed = true
      return fn(l)
    })
    if (!changed) return
    saveHistory(my, peer, updated)
    if (selectedRef.current === peer) {
      setLog((prev) => prev.map((l) => (l.id === msgId ? fn(l) : l)))
    }
  }, [])

  const acceptPeerKey = useCallback((peer: string, pk: Uint8Array, fpHint: string) => {
    void (async () => {
      const fp = await fingerprint(pk)
      if (fpHint && fpHint !== fp) {
        setErr(`Key fingerprint mismatch for ${peer}`)
        return
      }
      const prev = pinsRef.current[peer]
      if (prev && prev.fingerprint !== fp) {
        setErr(`Possible MITM: ${peer} key changed (pinned ${prev.fingerprint}, now ${fp})`)
        return
      }
      if (!prev) {
        pinsRef.current[peer] = { fingerprint: fp, publicKeyB64: encodePublicKeyB64(pk) }
        savePins(pinsRef.current)
      }
      const hadKey = !!peerKeysRef.current[peer]
      peerKeysRef.current[peer] = pk
      if (peer === selectedRef.current) {
        setE2eReady(true)
        // If we just learned this peer's key, re-pull server history so we
        // can decrypt rows that came in before the handshake.
        if (!hadKey && meRef.current) {
          sendRef.current({ type: 'dm_history', sender: meRef.current, recipient: peer })
        }
      }
    })()
  }, [])

  const unwrap = useCallback((msg: NexusMessage): NexusMessage => {
    const sender = msg.sender ?? ''
    if (!sender) return msg
    const pk = peerKeysRef.current[sender]
    const sk = keysRef.current.secretKey
    const out = { ...msg }
    // Only decrypt body for chat messages — call signaling is not encrypted
    if (out.body && pk && msg.type === 'msg') out.body = decryptFromPeer(out.body, pk, sk)
    return out
  }, [])

  const [sharingScreen, setSharingScreen] = useState(false)

  const tearDownCall = useCallback(() => {
    if (ringTimerRef.current) { clearTimeout(ringTimerRef.current); ringTimerRef.current = null }
    if (callTimerRef.current) { clearInterval(callTimerRef.current); callTimerRef.current = null }
    pcRef.current?.close()
    pcRef.current = null
    localStreamRef.current?.getTracks().forEach((t) => t.stop())
    localStreamRef.current = null
    screenStreamRef.current?.getTracks().forEach((t) => t.stop())
    screenStreamRef.current = null
    cameraTrackRef.current = null
    incomingCallSdpRef.current = null
    setSharingScreen(false)
    setCallSeconds(0)
    setCallMuted(false)
    setCallState(null)
  }, [])

  const toggleScreenShareRef = useRef<() => void>(() => {})
  const toggleScreenShare = useCallback(async () => {
    const pc = pcRef.current
    if (!pc) return

    if (screenStreamRef.current) {
      const videoSender = pc.getSenders().find((s) => s.track?.kind === 'video')
      const cam = cameraTrackRef.current
      if (videoSender && cam) await videoSender.replaceTrack(cam)
      else if (videoSender) pc.removeTrack(videoSender)
      screenStreamRef.current.getTracks().forEach((t) => t.stop())
      screenStreamRef.current = null
      if (localVideoRef.current && localStreamRef.current) {
        localVideoRef.current.srcObject = localStreamRef.current
      }
      setSharingScreen(false)
      return
    }

    let display: MediaStream
    try {
      display = await navigator.mediaDevices.getDisplayMedia({ video: true, audio: false })
    } catch {
      return
    }
    const screenTrack = display.getVideoTracks()[0]
    if (!screenTrack) return

    const videoSender = pc.getSenders().find((s) => s.track?.kind === 'video')
    if (videoSender) {
      cameraTrackRef.current = videoSender.track ?? null
      await videoSender.replaceTrack(screenTrack)
    } else {
      cameraTrackRef.current = null
      pc.addTrack(screenTrack, display)
    }
    screenStreamRef.current = display
    if (localVideoRef.current) localVideoRef.current.srcObject = display
    screenTrack.onended = () => { toggleScreenShareRef.current() }
    setSharingScreen(true)
  }, [])
  useEffect(() => { toggleScreenShareRef.current = () => { void toggleScreenShare() } }, [toggleScreenShare])

  const toggleMute = useCallback(() => {
    const stream = localStreamRef.current
    if (!stream) return
    const audio = stream.getAudioTracks()[0]
    if (!audio) return
    audio.enabled = !audio.enabled
    setCallMuted(!audio.enabled)
  }, [])

  const hangUp = useCallback(() => {
    const cs = callStateRef.current
    if (cs) {
      const type = cs.status === 'ringing' ? 'call_reject' : 'call_end'
      sendRef.current({ type, recipient: cs.peer })
    }
    tearDownCall()
  }, [tearDownCall])

  const makePC = useCallback((recipient: string): RTCPeerConnection => {
    const iceServers: RTCIceServer[] = turnRef.current
      ? [{ urls: turnRef.current.url, username: turnRef.current.username, credential: turnRef.current.password }]
      : [{ urls: 'stun:stun.l.google.com:19302' }]
    const pc = new RTCPeerConnection({ iceServers })
    pc.ontrack = (e) => {
      if (remoteVideoRef.current && e.streams[0]) remoteVideoRef.current.srcObject = e.streams[0]
    }
    pc.onicecandidate = (e) => {
      if (e.candidate) {
        sendRef.current({ type: 'ice_candidate', recipient, candidate: JSON.stringify(e.candidate) })
      }
    }
    return pc
  }, [])

  const startCall = useCallback(async (type: 'audio' | 'video') => {
    const recipient = selectedRef.current
    if (!recipient || !meRef.current) return
    let stream: MediaStream
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true }, video: type === 'video' })
    } catch {
      setErr('Microphone/camera access denied — click the lock icon in the address bar, reset the permission, and try again.')
      return
    }
    const pc = makePC(recipient)
    pcRef.current = pc
    localStreamRef.current = stream
    stream.getTracks().forEach((t) => pc.addTrack(t, stream))
    if (localVideoRef.current && type === 'video') localVideoRef.current.srcObject = stream
    // Prefer Opus codec for high-quality voice
    if ('getCapabilities' in RTCRtpSender) {
      const caps = RTCRtpSender.getCapabilities('audio')
      if (caps) {
        const opus = caps.codecs.filter(c => c.mimeType === 'audio/opus')
        const rest = caps.codecs.filter(c => c.mimeType !== 'audio/opus')
        try {
          pc.getTransceivers().forEach(t => {
            if (t.sender.track?.kind === 'audio') t.setCodecPreferences([...opus, ...rest])
          })
        } catch { /* not supported */ }
      }
    }
    const offer = await pc.createOffer()
    await pc.setLocalDescription(offer)
    sendRef.current({ type: 'call_offer', recipient, sdp: offer.sdp, body: type })
    setCallState({ peer: recipient, type, status: 'ringing', direction: 'outgoing' })
    // Ring timeout — auto-hangup after 60 seconds if unanswered
    ringTimerRef.current = setTimeout(() => {
      if (callStateRef.current?.status === 'ringing') {
        hangUp()
        setErr('No answer')
      }
    }, 60000)
  }, [makePC, hangUp])

  const acceptCall = useCallback(async () => {
    const cs = callStateRef.current
    const sdp = incomingCallSdpRef.current
    if (!cs || !sdp) return
    let stream: MediaStream
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true }, video: cs.type === 'video' })
    } catch {
      setErr('Microphone/camera access denied — click the lock icon in the address bar, reset the permission, and try again.')
      hangUp()
      return
    }
    const pc = makePC(cs.peer)
    pcRef.current = pc
    localStreamRef.current = stream
    stream.getTracks().forEach((t) => pc.addTrack(t, stream))
    if (localVideoRef.current && cs.type === 'video') localVideoRef.current.srcObject = stream
    // Prefer Opus codec for high-quality voice
    if ('getCapabilities' in RTCRtpSender) {
      const caps = RTCRtpSender.getCapabilities('audio')
      if (caps) {
        const opus = caps.codecs.filter(c => c.mimeType === 'audio/opus')
        const rest = caps.codecs.filter(c => c.mimeType !== 'audio/opus')
        try {
          pc.getTransceivers().forEach(t => {
            if (t.sender.track?.kind === 'audio') t.setCodecPreferences([...opus, ...rest])
          })
        } catch { /* not supported */ }
      }
    }
    await pc.setRemoteDescription({ type: 'offer', sdp })
    const answer = await pc.createAnswer()
    await pc.setLocalDescription(answer)
    sendRef.current({ type: 'call_answer', recipient: cs.peer, sdp: answer.sdp })
    incomingCallSdpRef.current = null
    setCallState({ ...cs, status: 'active' })
    setCallSeconds(0)
    callTimerRef.current = setInterval(() => setCallSeconds((s) => s + 1), 1000)
  }, [makePC, hangUp])

  const onMessageRef = useRef<(raw: NexusMessage) => void>(() => {})

  useLayoutEffect(() => {
    sendRef.current = (m: NexusMessage) => {
      const w = wsRef.current
      if (!w || w.readyState !== WebSocket.OPEN) {
        setErr('Not connected')
        return
      }
      w.send(JSON.stringify(m))
    }
  })

  useLayoutEffect(() => {
    onMessageRef.current = (raw: NexusMessage) => {
      const msg = unwrap(raw)

      switch (msg.type) {
        case 'auth_result':
          if (msg.status === 'ok' && msg.qr_token) {
            localStorage.setItem(SESSION_KEY, msg.qr_token)
            setSessionToken(msg.qr_token)
            setMe(msg.sender ?? null)
            setErr('')
            if (msg.turn_config) setTurn(msg.turn_config)
            playPhazeSound('Login.wav')
            sendRef.current({
              type: 'presence',
              sender: msg.sender,
              status: 'Online',
              public_key: encodePublicKeyB64(keysRef.current.publicKey),
            })
            registerPush(sendRef.current)
            // Load server-side settings to sync preferences across devices.
            sendRef.current({ type: 'settings_get', sender: msg.sender })
            // On first login of this browser session, ask the server whether
            // this account has a PIN-encrypted key backup waiting. If yes
            // (and we don't already have the same keys), we'll prompt the
            // user to restore.
            if (!restoreCheckedRef.current) {
              restoreCheckedRef.current = true
              sendRef.current({ type: 'key_backup_get' })
            }
          } else {
            localStorage.removeItem(SESSION_KEY)
            if (msg.status === 'totp_required') { setNeedsTotp(true); setErr('2FA required: enter TOTP code.') }
            else setErr(msg.error || msg.status || 'Auth failed')
          }
          break

        case 'friend_status':
          if (msg.sender) {
            setFriends((f) => ({ ...f, [msg.sender!]: msg.status || 'Offline' }))
            if (msg.status === 'Offline' && callStateRef.current?.peer === msg.sender) {
              tearDownCall()
            }
          }
          break

        case 'pending_requests':
          setPending(msg.results ?? [])
          break

        case 'friend_request':
          if (msg.sender) setPending((p) => (p.includes(msg.sender!) ? p : [...p, msg.sender!]))
          break

        case 'friend_accepted':
          if (msg.sender) {
            setFriends((f) => ({ ...f, [msg.sender!]: msg.status || 'Online' }))
            appendLog('system', `${msg.sender} accepted your friend request`, false)
          }
          break

        case 'register_result':
          if (msg.status === 'ok') {
            // Anonymous registration — no email, no verify step. Sign in straight away.
            setErr('Account created. Sign in below.')
            setRegStep('done')
            setMode('login')
            setLoginUser(regUser)
          } else if (msg.status === 'pending_verification' || msg.status === 'verification_sent') {
            setErr('Account created. Check your email for a 6-digit code, enter it below.')
            setRegStep('verify')
          } else {
            setErr(msg.error || 'Registration failed')
          }
          break

        case 'verify_result':
          if (msg.status === 'ok') {
            setErr('Email verified. You can sign in now.')
            setRegStep('done')
            setMode('login')
          } else {
            setErr(msg.error || 'Verification failed — double-check the code')
          }
          break

        case 'search_results':
          setGlobalSearchResults(msg.results ?? [])
          break
        case 'presence': {
          const pk = decodePublicKeyField(msg.public_key as string | number[] | undefined)
          if (msg.sender && pk && pk.length === 32) acceptPeerKey(msg.sender, pk, msg.key_fingerprint || '')
          if (msg.sender && msg.status) setFriends((f) => ({ ...f, [msg.sender!]: msg.status || 'Online' }))
          break
        }

        case 'key_request':
          if (msg.sender) {
            const my = meRef.current
            if (my) {
              void fingerprint(keysRef.current.publicKey).then((fp) => {
                sendRef.current({
                  type: 'presence',
                  sender: my,
                  recipient: msg.sender,
                  status: 'Online',
                  public_key: encodePublicKeyB64(keysRef.current.publicKey),
                  key_fingerprint: fp,
                })
              })
            }
          }
          break

        case 'msg':
          if (msg.sender && msg.body !== undefined) {
            const my = meRef.current
            const incomingId = msg.msg_id
            // Suppress duplicate when the server echoes a message we already
            // wrote optimistically (sender === me path).
            if (incomingId && msg.sender === my) {
              const peer = selectedRef.current
              if (peer && loadHistory(my!, peer).some((l) => l.id === incomingId)) break
            }
            appendLog(msg.sender, msg.body || '[empty]', msg.sender === my, { id: incomingId })
            // Suppress notification sound + browser notification for muted peers.
            const senderIsMuted = msg.sender ? isPeerMuted(msg.sender) : false
            if (msg.sender !== my && !senderIsMuted) {
              playPhazeSound('MessageReceived.wav')
              if (wails && document.hidden) {
                const preview = (msg.body || '').startsWith('phaze-file')
                  ? '📎 Attachment' : (msg.body || '').slice(0, 60)
                wails.Notify(msg.sender, preview)
              }
            }
          }
          break

        case 'dm_history':
          if (msg.recipient && msg.dm_history) {
            ingestDMHistoryRef.current(msg.recipient, msg.dm_history)
          }
          break

        case 'link_check':
          if (msg.status === 'approved' && msg.qr_token) {
            // Server returned a fresh session token for the linked device.
            localStorage.setItem(SESSION_KEY, msg.qr_token)
            setSessionToken(msg.qr_token)
            setLinkBusy(false)
            setErr('')
            sendRef.current({ type: 'session_auth', qr_token: msg.qr_token, device_info: `web/${window.location.hostname}` })
          }
          break

        case 'key_backup_result':
          if (msg.status === 'ok' && msg.key_backup) {
            // Server returned a backup blob. Prompt restore only if the current
            // device doesn't already have the same public key (i.e. fresh session).
            setRestoreBackup(msg.key_backup)
          } else if (msg.status === 'not_found') {
            // No backup on server — nag them to set a PIN. Respect 7-day cooldown.
            const dismissedAt = Number(localStorage.getItem(BACKUP_NAG_KEY) || '0')
            if (Date.now() - dismissedAt > BACKUP_NAG_COOLDOWN_MS) {
              setShowBackupNag(true)
            }
          } else if (msg.status === 'stored') {
            setErr('✓ Recovery PIN saved')
            setShowBackupNag(false)
            localStorage.setItem(BACKUP_NAG_KEY, String(Date.now()))
          } else if (msg.status === 'deleted') {
            setErr('Recovery backup removed')
          } else if (msg.error) {
            setErr(`Backup error: ${msg.error}`)
          }
          break

        case 'msg_edit':
          if (msg.sender && msg.msg_id && msg.body !== undefined) {
            mutateMessage(msg.sender, msg.msg_id, (l) => ({ ...l, text: msg.body || '', edited: true, deleted: false }))
          }
          break

        case 'msg_status':
          if (msg.error) setErr(msg.error)
          break

        case 'msg_edit_result':
          if (msg.error) setErr(msg.error)
          break

        case 'msg_delete':
          if (msg.sender && msg.msg_id) {
            mutateMessage(msg.sender, msg.msg_id, (l) => ({ ...l, text: '', deleted: true, file: undefined }))
          }
          break

        case 'msg_react':
          if (msg.sender && msg.msg_id && msg.reaction) {
            const reactor = msg.sender
            const emoji = msg.reaction
            mutateMessage(msg.sender, msg.msg_id, (l) => {
              const r = { ...(l.reactions || {}) }
              const users = new Set(r[emoji] || [])
              if (users.has(reactor)) users.delete(reactor)
              else users.add(reactor)
              if (users.size === 0) delete r[emoji]
              else r[emoji] = [...users]
              return { ...l, reactions: r }
            })
          }
          break

        case 'read_receipt':
          // Mark all our sent messages in this conversation as seen (in-memory + localStorage)
          if (msg.sender && msg.sender !== meRef.current) {
            setLog((prev) => prev.map((l) => l.me && !l.seen ? { ...l, seen: true } : l))
            const my = meRef.current; const peer = msg.sender
            if (my && peer) {
              const stored = loadHistory(my, peer)
              if (stored.some((l) => l.me && !l.seen)) {
                saveHistory(my, peer, stored.map((l) => l.me && !l.seen ? { ...l, seen: true } : l))
              }
            }
          }
          break

        case 'typing':
          if (msg.sender && msg.sender !== meRef.current) {
            const peer = msg.sender
            setTypingPeers((p) => new Set([...p, peer]))
            clearTimeout(typingTimersRef.current[peer])
            typingTimersRef.current[peer] = setTimeout(() => {
              setTypingPeers((p) => { const n = new Set(p); n.delete(peer); return n })
            }, 3000)
          }
          break

        case 'call_offer':
          if (msg.sender && msg.sdp) {
            incomingCallSdpRef.current = msg.sdp
            setCallState({ peer: msg.sender, type: (msg.body as 'audio' | 'video') || 'audio', status: 'ringing', direction: 'incoming' })
          }
          break

        case 'call_answer':
          if (msg.sdp && pcRef.current) {
            if (ringTimerRef.current) { clearTimeout(ringTimerRef.current); ringTimerRef.current = null }
            pcRef.current.setRemoteDescription({ type: 'answer', sdp: msg.sdp }).catch((e: unknown) => setErr('Call setup failed: ' + String(e)))
            setCallState((prev) => prev ? { ...prev, status: 'active' } : null)
            setCallSeconds(0)
            if (callTimerRef.current) clearInterval(callTimerRef.current)
            callTimerRef.current = setInterval(() => setCallSeconds((s) => s + 1), 1000)
          }
          break

        case 'ice_candidate':
          if (msg.candidate && pcRef.current) {
            try { void pcRef.current.addIceCandidate(JSON.parse(msg.candidate)) } catch { /* stale candidate */ }
          }
          break

        case 'call_reject':
        case 'call_end':
          tearDownCall()
          break

        case 'call_busy':
          tearDownCall()
          setErr(`${msg.sender || 'User'} is already in a call.`)
          break

        case 'call_error':
          tearDownCall()
          setErr(msg.error || msg.body || 'Call failed — user may be offline.')
          break

        case 'call_invite':
          if (msg.sender && msg.channel_id) {
            setGroupCallInvite({ from: msg.sender, room: msg.channel_id })
          }
          break

        case 'global_notice':
          if (msg.body) {
            setGlobalNotice({ from: msg.sender || 'Phaze', msg: msg.body })
          }
          break

        case 'kicked':
          localStorage.removeItem(SESSION_KEY)
          peerKeysRef.current = {}
          setFriends({})
          setPending([])
          setSelected(null)
          setLog([])
          setMe(null)
          setErr(msg.body || 'Signed in from another location.')
          break

        case 'delete_account_result':
          if (msg.status === 'ok') {
            localStorage.removeItem(SESSION_KEY)
            localStorage.removeItem(KEYS_KEY)
            peerKeysRef.current = {}
            pinsRef.current = {}
            try {
              localStorage.removeItem('phaze_key_pins_v1')
              const my = meRef.current
              if (my) {
                localStorage.removeItem(unreadKey(my))
                for (let i = localStorage.length - 1; i >= 0; i--) {
                  const k = localStorage.key(i)
                  if (k && k.startsWith(`phaze_chat_${my}_`)) localStorage.removeItem(k)
                }
              }
            } catch { /* fine */ }
            setMe(null)
            setFriends({})
            setPending([])
            setSelected(null)
            setLog([])
            setErr('Account deleted. All your data has been erased.')
          } else {
            setErr(msg.error || 'Delete failed')
          }
          break

        case 'block_result':
          if (msg.status === 'blocked' && msg.recipient) {
            setErr(`Blocked ${msg.recipient}.`)
          }
          break

        case 'report_result':
          setReportSent(true)
          break

        case 'settings_result':
          // Server-side settings loaded — apply theme if set
          if (msg.body) {
            try {
              const saved = JSON.parse(msg.body) as Record<string, unknown>
              if (saved.theme === 'dark' || saved.theme === 'light' || saved.theme === 'skype7') {
                setTheme(saved.theme as 'light' | 'dark' | 'skype7')
              }
            } catch { /* ignore */ }
          }
          break

        case 'purge_email_result':
          if (msg.status === 'ok') setErr('Email removed from your account.')
          else setErr(msg.error || 'Failed to purge email')
          break

        default:
          if (import.meta.env.DEV) console.warn('[nexus] unknown message type:', msg.type)
          break
      }

      subscribersRef.current.forEach((sub) => {
        try { sub(msg) } catch { /* swallow */ }
      })
    }
  }, [unwrap, appendLog, acceptPeerKey, tearDownCall])

  useEffect(() => {
    const w = new WebSocket(wsUrl)
    wsRef.current = w
    startTransition(() => {
      setConn('connecting')
      setErr('')
    })

    w.onopen = () => {
      setConn('open')
      wsRetryDelay.current = 1000
      const tok = localStorage.getItem(SESSION_KEY)
      const host = window.location.hostname
      if (tok) {
        sendRef.current({ type: 'session_auth', qr_token: tok, device_info: `web/${host}` })
      }
    }

    w.onmessage = (e: MessageEvent) => {
      try {
        onMessageRef.current(JSON.parse(e.data as string) as NexusMessage)
      } catch { /* malformed */ }
    }

    w.onclose = () => {
      setConn('off')
      wsRef.current = null
      const delay = Math.min(wsRetryDelay.current, 30000)
      wsRetryDelay.current = Math.min(delay * 2, 30000)
      setTimeout(() => setWsRetry((n) => n + 1), delay)
    }

    w.onerror = () => {}

    return () => {
      w.close()
      wsRef.current = null
    }
  }, [wsUrl, wsRetry])

  const send = useCallback((m: NexusMessage) => { sendRef.current(m) }, [])

  const doAuth = (username: string, password: string, totp: string) => {
    if (!username && !password) { setErr('Username and password are required.'); return }
    if (!username) { setErr('Username is required.'); return }
    if (!password) { setErr('Password is required.'); return }
    setErr('')
    send({ type: 'auth', sender: username, body: password, totp_code: totp || undefined, device_info: `web/${window.location.hostname}` })
  }

  const sendFriendRequest = (to: string) => {
    send({ type: 'friend_request', sender: me ?? undefined, recipient: to })
  }

  const acceptFriend = (from: string) => {
    send({ type: 'friend_accept', recipient: from })
    setPending((p) => p.filter((x) => x !== from))
  }

  const ingestDMHistory = useCallback((peer: string, rows: import('./nexusTypes').DMMessage[]) => {
    const my = meRef.current
    if (!my || !peer || !rows?.length) return
    const peerKey = peerKeysRef.current[peer]
    const mySec = keysRef.current.secretKey
    const local = loadHistory(my, peer)
    const byId = new Map<string, ChatLine>(local.map((l) => [l.id, l]))
    for (const r of rows) {
      const isMe = r.sender === my
      let text = r.body || ''
      // For E2EE bodies we only know how to decrypt if we have the peer key.
      // If the peer key isn't loaded yet, leave as-is; the next presence
      // exchange will provide it and a later refresh will resolve.
      if (text && peerKey) {
        try { text = decryptFromPeer(text, peerKey, mySec) } catch { text = '[Encrypted]' }
      }
      const file = decodeFileBody(text) || undefined
      const ts = Date.parse(r.created_at + 'Z') || Date.now()
      const existing = byId.get(r.msg_id)
      const line: ChatLine = {
        id: r.msg_id,
        from: r.sender,
        text: file ? '' : (r.deleted ? '' : text),
        me: isMe,
        ts: existing?.ts ?? ts,
        edited: r.edited || existing?.edited,
        deleted: r.deleted || existing?.deleted,
        reactions: r.reactions || existing?.reactions,
        file: file || existing?.file,
      }
      byId.set(r.msg_id, line)
    }
    const merged = Array.from(byId.values()).sort((a, b) => a.ts - b.ts)
    saveHistory(my, peer, merged)
    if (selectedRef.current === peer) {
      setLog(merged)
    }
  }, [])
  useEffect(() => { ingestDMHistoryRef.current = ingestDMHistory }, [ingestDMHistory])

  const openChat = (name: string) => {
    setSelected(name)
    setEditingId(null)
    setDraft('')
    setSearch('')
    setSearchOpen(false)
    setMentionQuery(null)
    setPinsOpen(false)
    if (me) {
      setLog(loadHistory(me, name))
      setPinnedIds(loadPinned(me, name))
      if (unreadRef.current[name]) {
        const next = { ...unreadRef.current, [name]: 0 }
        unreadRef.current = next
        setUnread(next)
        saveUnread(me, next)
      }
      // Pull durable history from the server so messages survive a localStorage
      // wipe, a new browser, or a fresh device. Server stores E2EE ciphertext.
      send({ type: 'dm_history', sender: me, recipient: name })
      // Notify peer we've read their messages
      send({ type: 'read_receipt', sender: me, recipient: name, body: name })
    } else {
      setLog([])
      setPinnedIds([])
    }
    setE2eReady(!!peerKeysRef.current[name])
    if (!peerKeysRef.current[name]) {
      send({ type: 'key_request', sender: me ?? undefined, recipient: name })
    }
  }

  const togglePin = useCallback((line: ChatLine) => {
    if (!selected || !me) return
    setPinnedIds((prev) => {
      const next = prev.includes(line.id) ? prev.filter((x) => x !== line.id) : [...prev, line.id]
      savePinned(me, selected, next)
      return next
    })
  }, [selected, me])

  const scrollToMessage = useCallback((id: string) => {
    const el = document.querySelector(`[data-msg-id="${id}"]`)
    if (el) {
      el.scrollIntoView({ behavior: 'smooth', block: 'center' })
      el.classList.add('highlight-flash')
      setTimeout(() => el.classList.remove('highlight-flash'), 1600)
    }
  }, [])

  const sendChat = () => {
    if (!selected || !me || !draft.trim()) return
    if (editingId) {
      submitEdit()
      return
    }
    let plaintext = draft.trim()

    // Slash commands run client-side. "Local" actions (e.g. /clear, /help)
    // don't produce an outgoing message — they manipulate the UI directly.
    if (plaintext.startsWith('/')) {
      const space = plaintext.indexOf(' ')
      const head = space === -1 ? plaintext : plaintext.slice(0, space)
      const tail = space === -1 ? '' : plaintext.slice(space + 1)
      const cmd = SLASH_COMMANDS.find((c) => c.cmd === head)
      if (cmd) {
        const out = cmd.run(tail)
        setDraft('')
        if (typeof out === 'string') {
          if (!out) return
          plaintext = out
        } else if (out.local === 'clear') {
          setLog([])
          return
        } else if (out.local === 'help') {
          const helpLines = SLASH_COMMANDS.map((c) => `${c.cmd} — ${c.desc}`).join('\n')
          appendLog('system', `Available commands:\n${helpLines}`, false, { id: newMsgId() })
          return
        }
      }
    }

    const peer = peerKeysRef.current[selected]
    const body = peer ? encryptForPeer(plaintext, peer, keysRef.current.secretKey) : plaintext
    const msgId = newMsgId()
    send({ type: 'msg', sender: me, recipient: selected, body, msg_id: msgId })
    appendLog(me, plaintext, true, { id: msgId })
    playPhazeSound('MessageOutgoing.wav')
    setDraft('')
    setEmojiOpen(false)
  }

  const slashMatches = useMemo(() => {
    if (!draft.startsWith('/')) return [] as SlashCmd[]
    const head = draft.split(' ')[0].toLowerCase()
    return SLASH_COMMANDS.filter((c) => c.cmd.startsWith(head)).slice(0, 6)
  }, [draft])

  const paletteMatches = useMemo(() => {
    const q = paletteQuery.trim().toLowerCase()
    const friendList = Object.entries(friends)
    const friendMatches = q ? friendList.filter(([u]) => u.toLowerCase().includes(q)) : friendList.slice(0, 12)
    const friendNames = new Set(friendMatches.map(([u]) => u))
    const globalExtras: [string, string][] = globalSearchResults
      .filter((u) => !friendNames.has(u) && u !== me)
      .map((u) => [u, 'unknown'] as [string, string])
    return [...friendMatches, ...globalExtras].slice(0, 20)
  }, [paletteQuery, friends, globalSearchResults, me])

  useEffect(() => {
    if (globalSearchTimer.current) clearTimeout(globalSearchTimer.current)
    const q = paletteQuery.trim()
    if (q.length >= 2) {
      globalSearchTimer.current = setTimeout(() => {
        send({ type: 'search', body: q })
      }, 300)
    } else {
      setGlobalSearchResults([])
    }
  }, [paletteQuery, send])

  const sendFile = useCallback(async (file: File) => {
    if (!selected || !me || !sessionToken) {
      setErr('Sign in to send files')
      return
    }
    if (file.size > 25 * 1024 * 1024) {
      setErr('File exceeds 25 MB')
      return
    }
    try {
      const fd = new FormData()
      fd.append('file', file)
      const resp = await fetch('/api/v1/upload', {
        method: 'POST',
        headers: { Authorization: `Bearer ${sessionToken}` },
        body: fd,
      })
      if (!resp.ok) {
        setErr(`Upload failed: ${resp.status}`)
        return
      }
      const att = await resp.json() as FileAttachment
      const peerKey = peerKeysRef.current[selected]
      const plaintext = encodeFileBody(att)
      const body = peerKey ? encryptForPeer(plaintext, peerKey, keysRef.current.secretKey) : plaintext
      const msgId = newMsgId()
      send({ type: 'msg', sender: me, recipient: selected, body, msg_id: msgId })
      appendLog(me, '', true, { id: msgId, file: att })
      playPhazeSound('MessageOutgoing.wav')
    } catch (e) {
      setErr(`Upload error: ${(e as Error).message}`)
    }
  }, [selected, me, sessionToken, send, appendLog])

  const stopRecording = useCallback((cancel = false) => {
    const r = recorderRef.current
    if (!r) return
    recorderRef.current = null
    if (recTimerRef.current) { clearInterval(recTimerRef.current); recTimerRef.current = null }
    setRecording(false)
    if (cancel) {
      try { r.ondataavailable = null; r.onstop = null; r.stream.getTracks().forEach((t) => t.stop()) } catch { /* noop */ }
      try { r.stop() } catch { /* noop */ }
      recChunksRef.current = []
      return
    }
    // Real stop — the onstop handler will do the upload.
    try { r.stop() } catch { /* noop */ }
  }, [])

  const startRecording = useCallback(async () => {
    if (!selected || !sessionToken) {
      setErr('Sign in and open a chat to record voice')
      return
    }
    if (recording) { stopRecording(false); return }
    let stream: MediaStream
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: true })
    } catch {
      setErr('Microphone access denied')
      return
    }
    const mimeCandidates = ['audio/webm;codecs=opus', 'audio/webm', 'audio/ogg;codecs=opus', 'audio/mp4']
    let mimeType = ''
    for (const m of mimeCandidates) {
      if (typeof MediaRecorder !== 'undefined' && MediaRecorder.isTypeSupported(m)) { mimeType = m; break }
    }
    const rec = mimeType ? new MediaRecorder(stream, { mimeType }) : new MediaRecorder(stream)
    recorderRef.current = rec
    recChunksRef.current = []
    rec.ondataavailable = (e) => { if (e.data && e.data.size > 0) recChunksRef.current.push(e.data) }
    rec.onstop = () => {
      stream.getTracks().forEach((t) => t.stop())
      const dur = Math.max(1, Math.round((Date.now() - recStartRef.current) / 1000))
      const blob = new Blob(recChunksRef.current, { type: rec.mimeType || 'audio/webm' })
      recChunksRef.current = []
      // Wrap into a File so it lands cleanly on the upload endpoint.
      const ext = (rec.mimeType || '').includes('mp4') ? 'm4a' : (rec.mimeType || '').includes('ogg') ? 'ogg' : 'webm'
      const file = new File([blob], `voice-${Date.now()}-${dur}s.${ext}`, { type: rec.mimeType || 'audio/webm' })
      void sendFile(file)
    }
    recStartRef.current = Date.now()
    setRecDuration(0)
    setRecording(true)
    recTimerRef.current = setInterval(() => {
      setRecDuration(Math.round((Date.now() - recStartRef.current) / 1000))
    }, 250)
    // Auto-stop at 2 minutes to keep uploads reasonable
    rec.start()
    setTimeout(() => { if (recorderRef.current === rec) stopRecording(false) }, 2 * 60 * 1000)
  }, [selected, sessionToken, recording, stopRecording, sendFile])

  const reactTo = useCallback((line: ChatLine, emoji: string) => {
    if (!selected || !me) return
    send({ type: 'msg_react', sender: me, recipient: selected, msg_id: line.id, reaction: emoji })
    mutateMessage(selected, line.id, (l) => {
      const r = { ...(l.reactions || {}) }
      const users = new Set(r[emoji] || [])
      if (users.has(me)) users.delete(me)
      else users.add(me)
      if (users.size === 0) delete r[emoji]
      else r[emoji] = [...users]
      return { ...l, reactions: r }
    })
  }, [selected, me, send, mutateMessage])

  const [editingId, setEditingId] = useState<string | null>(null)
  const beginEdit = (line: ChatLine) => {
    setEditingId(line.id)
    setDraft(line.text)
  }
  const cancelEdit = () => { setEditingId(null); setDraft('') }
  const submitEdit = () => {
    if (!selected || !me || !editingId) return
    const text = draft.trim()
    if (!text) return
    const peer = peerKeysRef.current[selected]
    const body = peer ? encryptForPeer(text, peer, keysRef.current.secretKey) : text
    send({ type: 'msg_edit', sender: me, recipient: selected, msg_id: editingId, body })
    mutateMessage(selected, editingId, (l) => ({ ...l, text, edited: true }))
    setEditingId(null)
    setDraft('')
  }
  const deleteMessage = (line: ChatLine) => {
    if (!selected || !me) return
    if (!confirm('Delete this message for both of you?')) return
    send({ type: 'msg_delete', sender: me, recipient: selected, msg_id: line.id })
    mutateMessage(selected, line.id, (l) => ({ ...l, text: '', deleted: true, file: undefined }))
  }

  const handleDraftChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const v = e.target.value
    setDraft(v)
    const caret = e.target.selectionStart ?? v.length
    const upto = v.slice(0, caret)
    const m = /(?:^|\s)@([A-Za-z0-9_]{0,32})$/.exec(upto)
    if (m) {
      setMentionQuery(m[1].toLowerCase())
      setMentionIdx(0)
    } else {
      setMentionQuery(null)
    }
    if (selectedRef.current && meRef.current) {
      if (!outTypingTimerRef.current) {
        sendRef.current({ type: 'typing', recipient: selectedRef.current })
      }
      clearTimeout(outTypingTimerRef.current ?? undefined)
      outTypingTimerRef.current = setTimeout(() => { outTypingTimerRef.current = null }, 2000)
    }
  }

  const mentionMatches = useMemo(() => {
    if (mentionQuery === null) return [] as string[]
    const q = mentionQuery
    return Object.keys(friends).filter((u) => u.toLowerCase().startsWith(q)).slice(0, 6)
  }, [mentionQuery, friends])

  const completeMention = useCallback((username: string) => {
    const inp = draftInputRef.current
    const caret = inp?.selectionStart ?? draft.length
    const before = draft.slice(0, caret).replace(/@([A-Za-z0-9_]{0,32})$/, `@${username} `)
    const after = draft.slice(caret)
    const next = before + after
    setDraft(next)
    setMentionQuery(null)
    queueMicrotask(() => {
      const el = draftInputRef.current
      if (el) {
        const pos = before.length
        el.focus()
        el.setSelectionRange(pos, pos)
      }
    })
  }, [draft])

  const doRegister = () => {
    setErr('')
    if (regUser.length < 3 || regUser.length > 32) { setErr('Username must be 3–32 characters'); return }
    if (regPass.length < 8) { setErr('Password must be at least 8 characters'); return }
    if (!regEmail.includes('@')) { setErr('Enter a valid email'); return }
    send({ type: 'register', sender: regUser, body: regPass, email: regEmail, token: inviteCode ?? undefined })
  }

  const doVerify = () => {
    setErr('')
    if (!/^\d{6}$/.test(regCode.trim())) { setErr('Enter the 6-digit code from your email'); return }
    send({ type: 'verify_email', sender: regUser, body: regCode.trim() })
  }

  const doRestore = useCallback(async () => {
    if (!restoreBackup || !restorePin) return
    setRestoreBusy(true)
    try {
      const { publicKey, secretKey } = await decryptKeyBackup(restoreBackup, restorePin)
      keysRef.current = { publicKey, secretKey }
      localStorage.setItem(
        KEYS_KEY,
        JSON.stringify({
          pub: btoa(String.fromCharCode(...publicKey)),
          sec: btoa(String.fromCharCode(...secretKey)),
        }),
      )
      const my = meRef.current
      if (my) {
        sendRef.current({
          type: 'presence',
          sender: my,
          status: 'Online',
          public_key: encodePublicKeyB64(publicKey),
        })
      }
      setRestoreBackup(null)
      setRestorePin('')
      setErr('✓ Keys restored from backup')
    } catch (e) {
      setErr((e as Error).message || 'Restore failed')
    } finally {
      setRestoreBusy(false)
    }
  }, [restoreBackup, restorePin])

  const totalUnread = useMemo(() => Object.values(unread).reduce((a, b) => a + b, 0), [unread])
  useEffect(() => {
    const base = 'Phaze'
    document.title = totalUnread > 0 ? `(${totalUnread}) ${base}` : base
    wails?.SetUnread(totalUnread)
  }, [totalUnread])

  return (
    <div className={`app theme-${theme}${wails ? ' desktop-app' : ''}`}>
      {wails && (
        <DesktopTitleBar
          onMinimise={() => wails.WindowMinimise()}
          onMaximise={() => wails.WindowToggleMaximise()}
          onClose={() => wails.WindowClose()}
        />
      )}
      {snow && <Snowflakes />}
      <header className="top">
        <div className="brand">
          <h1>Phaze</h1>
        </div>
        {me && (
          <button className="palette-hint" title="Quick switcher (⌘K)" onClick={() => { setPaletteOpen(true); setPaletteQuery(''); setPaletteIdx(0) }}>
            <span>Search friends…</span>
            <kbd>⌘K</kbd>
          </button>
        )}
        <span className={`pill ${conn === 'open' ? 'ok' : ''}`}>{conn}</span>
        <button
          className="settings-gear"
          title={`Theme: ${theme} — click to cycle (dark · light · Skype 7)`}
          onClick={() => setTheme(theme === 'dark' ? 'light' : theme === 'light' ? 'skype7' : 'dark')}
        >{theme === 'dark' ? '☀' : theme === 'light' ? '🎨' : '💙'}</button>
        <button
          className="settings-gear"
          title={snow ? 'Turn off snow' : 'Let it snow'}
          onClick={() => setSnow((s) => !s)}
        >{snow ? '🌨' : '❄'}</button>
        {me && (
          <button className="settings-gear" title="Remote Control" onClick={() => setRemoteOpen(true)}>🖥</button>
        )}
        {me && (
          <button className="settings-gear" title="Settings" onClick={() => setSettingsOpen(true)}>⚙</button>
        )}
        {me && <span className="me">@{me}</span>}
      </header>

      {/* ── Floating bottom nav ─────────────────────────────────── */}
      {me && (
        <nav className="floating-nav">
          <button type="button" className={view === 'dms' ? 'on' : ''} onClick={() => setView('dms')}>
            <span className="nav-icon">💬</span>
            <span className="nav-label">Home</span>
          </button>
          <button type="button" className={view === 'spaces' ? 'on' : ''} onClick={() => setView('spaces')}>
            <span className="nav-icon">🌐</span>
            <span className="nav-label">Spaces</span>
          </button>
          <button type="button" className={view === 'live' ? 'on' : ''} onClick={() => setView('live')}>
            <span className="nav-icon">🔴</span>
            <span className="nav-label">Live</span>
          </button>
          <button type="button" onClick={() => setSettingsOpen(true)}>
            <span className="nav-icon">👤</span>
            <span className="nav-label">Profile</span>
          </button>
        </nav>
      )}

      {err && <div className="banner">{err}</div>}

      {restoreBackup && (
        <div className="banner" style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap', background: 'var(--panel)', border: '1px solid var(--brand)', borderRadius: 10, padding: '10px 16px', margin: '8px 16px' }}>
          <span style={{ fontSize: '0.85rem' }}>🔑 Recovery backup found.</span>
          <input
            type="password"
            placeholder="PIN"
            value={restorePin}
            onChange={(e) => setRestorePin(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') void doRestore() }}
            style={{ width: 100, padding: '4px 8px', borderRadius: 6, border: '1px solid var(--input-border)', background: 'var(--input-bg)', color: 'var(--text)', fontSize: '0.85rem' }}
          />
          <div style={{ display: 'flex', gap: 6 }}>
            <button type="button" onClick={() => void doRestore()} disabled={restoreBusy} style={{ padding: '4px 12px', borderRadius: 6, background: 'var(--brand)', color: '#fff', border: 'none', fontSize: '0.8rem', cursor: 'pointer' }}>{restoreBusy ? '...' : 'Restore'}</button>
            <button type="button" onClick={() => { setRestoreBackup(null); setRestorePin('') }} style={{ padding: '4px 12px', borderRadius: 6, background: 'transparent', color: 'var(--muted)', border: '1px solid var(--input-border)', fontSize: '0.8rem', cursor: 'pointer' }}>Skip</button>
          </div>
        </div>
      )}

      {settingsOpen && me && (
        <Settings
          me={me}
          sessionToken={sessionToken}
          send={send}
          subscribe={subscribe}
          onClose={() => { setSettingsOpen(false); setSettingsInitialTab('profile') }}
          onSignOut={() => {
            localStorage.removeItem(SESSION_KEY)
            setSessionToken(null)
            setMe(null)
            setSettingsOpen(false)
            setSelected(null)
            setFriends({})
            setPending([])
            setView('dms')
          }}
          initialTab={settingsInitialTab}
          onSetBackupPin={async (pin: string) => {
            const blob = await encryptKeyBackup(keysRef.current.publicKey, keysRef.current.secretKey, pin)
            send({ type: 'key_backup_put', key_backup: blob })
          }}
          onDeleteBackup={() => send({ type: 'key_backup_delete' })}
        />
      )}

      {me && remoteOpen && (
        <Suspense fallback={null}>
        <RemoteControl
          me={me}
          send={send}
          subscribe={subscribe}
          turn={turn}
          onClose={() => setRemoteOpen(false)}
        />
        </Suspense>
      )}

      {me && paletteOpen && (
        <div className="palette-overlay" onClick={() => setPaletteOpen(false)}>
          <div className="palette" onClick={(e) => e.stopPropagation()}>
            <input
              autoFocus
              placeholder="Search friends, jump to a chat…"
              value={paletteQuery}
              onChange={(e) => { setPaletteQuery(e.target.value); setPaletteIdx(0) }}
              onKeyDown={(e) => {
                if (e.key === 'ArrowDown') { e.preventDefault(); setPaletteIdx((i) => Math.min(i + 1, paletteMatches.length - 1)) }
                else if (e.key === 'ArrowUp') { e.preventDefault(); setPaletteIdx((i) => Math.max(0, i - 1)) }
                else if (e.key === 'Enter') {
                  const pick = paletteMatches[paletteIdx]
                  if (pick) { openChat(pick[0]); setPaletteOpen(false) }
                }
              }}
            />
            <div className="palette-list">
              {paletteMatches.length === 0 && (
                <div className="palette-empty">{paletteQuery.length >= 2 ? 'No users found.' : 'Type to search all Phaze users…'}</div>
              )}
              {paletteMatches.map(([u, st], i) => {
                const isFriend = u in friends
                const last = isFriend ? lastLineFor(me, u) : null
                return (
                  <button
                    key={u}
                    type="button"
                    className={`palette-row ${i === paletteIdx ? 'on' : ''}`}
                    onMouseEnter={() => setPaletteIdx(i)}
                    onClick={() => {
                      if (isFriend) { openChat(u); setPaletteOpen(false) }
                      else { sendFriendRequest(u); setPaletteOpen(false); setErr(`Friend request sent to ${u}`) }
                    }}
                  >
                    <span className="avatar" style={{ background: avatarColor(u) }}>
                      {u[0]?.toUpperCase()}
                      <span className="avatar-dot" style={{ background: isFriend ? statusColor(st) : '#555' }} />
                    </span>
                    <span className="palette-meta">
                      <span className="palette-name">{u}</span>
                      <span className="palette-preview">{isFriend ? (last?.text || st) : 'Click to send friend request'}</span>
                    </span>
                    {last && <span className="palette-time">{relTime(last.ts)}</span>}
                    {!isFriend && <span className="palette-time" style={{ color: '#863bff' }}>+ Add</span>}
                  </button>
                )
              })}
            </div>
            <div className="palette-foot">
              <span><kbd>↑↓</kbd> navigate</span>
              <span><kbd>↵</kbd> open</span>
              <span><kbd>esc</kbd> close</span>
            </div>
          </div>
        </div>
      )}

      {me && showBackupNag && !settingsOpen && (
        <div className="backup-nag">
          <span className="backup-nag-icon">🔐</span>
          <div className="backup-nag-text">
            <strong>Protect your chat history.</strong>
            <span>Set a Recovery PIN so you can sign in on a new browser or device without losing your encrypted messages.</span>
          </div>
          <button type="button" className="backup-nag-cta" onClick={() => { setSettingsInitialTab('devices'); setSettingsOpen(true) }}>Set PIN</button>
          <button type="button" className="backup-nag-dismiss" title="Remind me later" onClick={() => { setShowBackupNag(false); localStorage.setItem(BACKUP_NAG_KEY, String(Date.now())) }}>✕</button>
        </div>
      )}

      {/* ── Onboarding (first sign-in only) ─────────────────────── */}
      {me && sessionToken && onboardingOpen && (
        <Suspense fallback={null}>
          <Onboarding
            me={me}
            sessionToken={sessionToken}
            onAddFriend={(name) => sendFriendRequest(name)}
            onJump={(v) => setView(v)}
            onClose={() => setOnboardingOpen(false)}
          />
        </Suspense>
      )}

      {/* ── Report abuse dialog ──────────────────────────────────── */}
      {reportTarget && (
        <div className="restore-overlay" onClick={(e) => { if (e.target === e.currentTarget) { setReportTarget(null); setReportSent(false) } }}>
          <div className="restore-card">
            {reportSent ? (
              <>
                <h2>Report sent</h2>
                <p className="muted small">Thanks for letting us know. Our team will review this report.</p>
                <button type="button" onClick={() => { setReportTarget(null); setReportSent(false) }}>Close</button>
              </>
            ) : (
              <>
                <h2>Report {reportTarget}</h2>
                <p className="muted small">Describe what's happening so our team can review it.</p>
                <textarea
                  autoFocus
                  placeholder="Reason for report (e.g. harassment, spam…)"
                  value={reportReason}
                  onChange={(e) => setReportReason(e.target.value)}
                  rows={4}
                  style={{ width: '100%', boxSizing: 'border-box', resize: 'vertical' }}
                />
                <div className="row">
                  <button
                    type="button"
                    disabled={!reportReason.trim()}
                    onClick={() => {
                      if (!me || !reportReason.trim()) return
                      send({ type: 'report_abuse', sender: me, recipient: reportTarget!, body: reportReason.trim() })
                      setReportSent(true)
                    }}
                  >Submit report</button>
                  <button type="button" className="link-btn" onClick={() => { setReportTarget(null); setReportSent(false) }}>Cancel</button>
                </div>
              </>
            )}
          </div>
        </div>
      )}

      {/* ── Support chat bubble (always available) ───────────────── */}
      <SupportBubble sessionToken={sessionToken} me={me} />
      {showSupport && <SupportForm me={me} bmcUrl={bmcUrl} onClose={() => setShowSupport(false)} />}

      {/* ── User profile modal ───────────────────────────────────── */}
      {profileUser && me && (
        <UserProfile
          username={profileUser}
          me={me}
          friends={friends}
          send={send}
          onClose={() => setProfileUser(null)}
          onStartDM={(u) => { setView('dms'); openChat(u) }}
        />
      )}

      {/* ── Call overlay ─────────────────────────────────────────── */}
      {callState && (
        <div className="call-overlay">
          {callState.type === 'video' && (
            <div className="call-videos">
              <video ref={remoteVideoRef} autoPlay playsInline className="call-remote" />
              <video ref={localVideoRef} autoPlay playsInline muted className="call-local" />
            </div>
          )}
          <div className="call-card">
            <div className="call-avatar">{callState.peer[0].toUpperCase()}</div>
            <div className="call-peer-name">{callState.peer}</div>
            <div className="call-status-text">
              {callState.status === 'ringing' && callState.direction === 'outgoing' && 'Calling…'}
              {callState.status === 'ringing' && callState.direction === 'incoming' && `${callState.type === 'video' ? '📹' : '☎'} Incoming ${callState.type} call`}
              {callState.status === 'active' && `${String(Math.floor(callSeconds / 60)).padStart(2, '0')}:${String(callSeconds % 60).padStart(2, '0')}`}
            </div>
            <div className="call-controls">
              {callState.direction === 'incoming' && callState.status === 'ringing' ? (
                <>
                  <button className="call-btn-accept" onClick={() => void acceptCall()}>Accept</button>
                  <button className="call-btn-decline" onClick={hangUp}>Decline</button>
                </>
              ) : (
                <>
                  <button className={`call-btn-mute ${callMuted ? 'active' : ''}`} onClick={toggleMute} title={callMuted ? 'Unmute' : 'Mute'}>
                    {callMuted ? '🔇 Unmute' : '🎤 Mute'}
                  </button>
                  {callState.status === 'active' && (
                    <button className="call-btn-share" onClick={() => void toggleScreenShare()} title={sharingScreen ? 'Stop sharing' : 'Share screen'}>
                      {sharingScreen ? '🛑 Stop sharing' : '🖥 Share screen'}
                    </button>
                  )}
                  <button className="call-btn-end" onClick={hangUp}>End call</button>
                </>
              )}
            </div>
          </div>
        </div>
      )}

      {/* ── Global notice popup ────────────────────────────────── */}
      {globalNotice && (
        <div className="restore-overlay" onClick={() => setGlobalNotice(null)}>
          <div className="restore-card" onClick={(e) => e.stopPropagation()} style={{ textAlign: 'center' }}>
            <h2 style={{ color: 'var(--brand)', fontSize: '1.2rem' }}>Notice from {globalNotice.from}</h2>
            <p style={{ margin: '1rem 0', fontSize: '0.95rem', lineHeight: 1.6 }}>{globalNotice.msg}</p>
            <button type="button" onClick={() => setGlobalNotice(null)} style={{ padding: '0.6rem 2rem', borderRadius: 10, background: 'var(--brand)', color: '#fff', border: 'none', fontWeight: 700, cursor: 'pointer' }}>Got it</button>
          </div>
        </div>
      )}

      {/* ── What's New changelog popup ────────────────────────── */}
      {/* ── What's New — feature showcase carousel ────────────── */}
      {me && !changelogSeen && !changelogOpen && (
        <div className="wn-banner" onClick={() => setChangelogOpen(true)}>
          <span className="wn-banner-icon">🎉</span>
          <span className="wn-banner-text"><strong>New features dropped!</strong> Tap to see what's new.</span>
          <button type="button" className="wn-banner-dismiss" onClick={(e) => { e.stopPropagation(); setChangelogSeen(true); localStorage.setItem('phaze_changelog_v', '2025-05-25') }}>✕</button>
        </div>
      )}

      {changelogOpen && (
        <div className="wn-overlay">
          <div className="wn-modal">
            <button type="button" className="wn-close" onClick={() => { setChangelogOpen(false); setChangelogSeen(true); localStorage.setItem('phaze_changelog_v', '2025-05-25') }}>✕</button>
            <div className="wn-header">
              <img src={faviconUrl} alt="" className="wn-logo" />
              <h2>What's New</h2>
              <p>Here's everything we shipped this week.</p>
            </div>
            <div className="wn-card" style={{ borderColor: changelogFeatures[changelogSlide].color + '33' }}>
              <div className="wn-card-icon" style={{ background: changelogFeatures[changelogSlide].color + '18', color: changelogFeatures[changelogSlide].color }}>
                {changelogFeatures[changelogSlide].icon}
              </div>
              <h3 className="wn-card-title">{changelogFeatures[changelogSlide].title}</h3>
              <p className="wn-card-desc">{changelogFeatures[changelogSlide].desc}</p>
            </div>
            <div className="wn-dots">
              {changelogFeatures.map((_, i) => (
                <button key={i} type="button" className={`wn-dot ${i === changelogSlide ? 'on' : ''}`} onClick={() => setChangelogSlide(i)} style={i === changelogSlide ? { background: changelogFeatures[i].color } : {}} />
              ))}
            </div>
            <div className="wn-nav">
              {changelogSlide > 0 && (
                <button type="button" className="wn-btn secondary" onClick={() => setChangelogSlide((s) => s - 1)}>Back</button>
              )}
              <div style={{ flex: 1 }} />
              {changelogSlide < changelogFeatures.length - 1 ? (
                <button type="button" className="wn-btn primary" onClick={() => setChangelogSlide((s) => s + 1)}>Next</button>
              ) : (
                <button type="button" className="wn-btn primary" onClick={() => { setChangelogOpen(false); setChangelogSeen(true); localStorage.setItem('phaze_changelog_v', '2025-05-25') }}>Let's go</button>
              )}
            </div>
            <div className="wn-counter">{changelogSlide + 1} / {changelogFeatures.length}</div>
          </div>
        </div>
      )}

      {/* ── Group call invite banner ─────────────────────────── */}
      {groupCallInvite && (
        <div className="banner" style={{ display: 'flex', alignItems: 'center', gap: 8, justifyContent: 'space-between' }}>
          <span>👥 <strong>{groupCallInvite.from}</strong> invited you to a group call</span>
          <div style={{ display: 'flex', gap: 6 }}>
            <button type="button" style={{ padding: '4px 12px', borderRadius: 6, background: 'var(--brand)', color: '#fff', border: 'none', fontWeight: 700, cursor: 'pointer' }} onClick={() => { setGroupCallRoom(groupCallInvite.room); setGroupCallInvite(null) }}>Join</button>
            <button type="button" style={{ padding: '4px 12px', borderRadius: 6, background: 'transparent', color: 'var(--muted)', border: '1px solid var(--input-border)', cursor: 'pointer' }} onClick={() => setGroupCallInvite(null)}>Decline</button>
          </div>
        </div>
      )}

      {/* ── Group call overlay ────────────────────────────────── */}
      {me && groupCallRoom && (
        <div className="call-overlay" style={{ flexDirection: 'column', gap: '1rem', padding: '2rem' }}>
          <Suspense fallback={null}>
          <VoiceRoom
            me={me}
            channelId={groupCallRoom}
            channelName="Group Call"
            send={send}
            subscribe={subscribe}
            turn={turn}
          />
          </Suspense>
          <button
            type="button"
            className="call-btn-end"
            onClick={() => {
              send({ type: 'voice_leave', channel_id: groupCallRoom })
              setGroupCallRoom(null)
            }}
          >Leave group call</button>
        </div>
      )}

      {me && view === 'spaces' ? (
        <Suspense fallback={<div style={{ padding: '2rem', textAlign: 'center' }}>Loading Spaces…</div>}>
        <Spaces
          me={me}
          send={send}
          subscribe={subscribe}
          turn={turn}
          onUserClick={setProfileUser}
          uploadAttachment={async (file) => {
            if (!sessionToken) return null
            const fd = new FormData()
            fd.append('file', file)
            const resp = await fetch('/api/v1/upload', {
              method: 'POST',
              headers: { Authorization: `Bearer ${sessionToken}` },
              body: fd,
            })
            if (!resp.ok) return null
            return await resp.json()
          }}
        />
        </Suspense>
      ) : me && view === 'live' ? (
        <Suspense fallback={null}><LivePage me={me} send={send} subscribe={subscribe} turn={turn} /></Suspense>
      ) : (
        <>
        {/* ── Auth (not logged in) ─────────────────────────────── */}
        {!me && (
          <main className="grid">
            <div className="hub-auth">
              <div className="auth-hero">
                <img src={faviconUrl} alt="Phaze" className="auth-hero-logo" />
                <h2 className="auth-hero-title">Phaze</h2>
                <p className="auth-hero-sub">Encrypted chat for everyone. Private by default.</p>
                <div className="auth-features">
                  <div className="auth-feature">
                    <span className="auth-feature-icon">🔒</span>
                    <span className="auth-feature-label">End-to-end encrypted</span>
                  </div>
                  <div className="auth-feature">
                    <span className="auth-feature-icon">📞</span>
                    <span className="auth-feature-label">Voice & video calls</span>
                  </div>
                  <div className="auth-feature">
                    <span className="auth-feature-icon">🌐</span>
                    <span className="auth-feature-label">Public Spaces</span>
                  </div>
                  <div className="auth-feature">
                    <span className="auth-feature-icon">🎙️</span>
                    <span className="auth-feature-label">Voice messages</span>
                  </div>
                </div>
              </div>
              <section className="panel">
                <h2>Sign in to Phaze</h2>
                {mode === 'login' ? (
                  <form className="form" onSubmit={(e) => { e.preventDefault(); doAuth(loginUser.trim(), loginPass, loginTotp.trim()) }}>
                    <input placeholder="Username" value={loginUser} onChange={(e) => setLoginUser(e.target.value)} autoComplete="username" />
                    <input type="password" placeholder="Password" value={loginPass} onChange={(e) => setLoginPass(e.target.value)} autoComplete="current-password" />
                    {needsTotp && <input placeholder="TOTP code" value={loginTotp} onChange={(e) => setLoginTotp(e.target.value)} autoFocus />}
                    <button type="submit">Sign in</button>
                    <button type="button" className="link-btn" onClick={() => { setMode('register'); setErr(''); setNeedsTotp(false); setRegStep('form') }}>Create an account</button>
                    <button type="button" className="link-btn" onClick={() => { setMode('forgot'); setErr(''); setNeedsTotp(false) }}>Forgot password?</button>
                    <button type="button" className="link-btn" onClick={() => { setMode('link'); setErr(''); setNeedsTotp(false) }}>Sign in with a link code from another device</button>
                  </form>
                ) : mode === 'forgot' ? (
                  <div className="form">
                    <p className="muted small">Enter the email address on your account. We'll send a reset link.</p>
                    <input type="email" placeholder="Email address" value={forgotEmail} onChange={(e) => setForgotEmail(e.target.value)} autoFocus />
                    <button type="button" onClick={() => {
                      if (!forgotEmail.includes('@')) { setErr('Enter a valid email'); return }
                      send({ type: 'forgot_password', email: forgotEmail })
                      setErr('If an account matches, a reset link has been sent to your email.')
                    }}>Send reset link</button>
                    <button type="button" className="link-btn" onClick={() => { setMode('login'); setErr('') }}>Back to sign in</button>
                  </div>
                ) : mode === 'link' ? (
                  <div className="form">
                    <p className="muted small">Open Phaze on a device you're already signed into → Settings → 💾 Backup &amp; Devices → "Generate link code". Enter the code below or scan a QR code.</p>
                    
                    {cameraActive ? (
                      <div className="qr-scanner-container" style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', marginBottom: 12 }}>
                        <video ref={videoRef} style={{ width: '100%', maxWidth: 280, borderRadius: 8, border: '2px solid #232328', background: '#000' }} />
                        <button type="button" className="link-btn" onClick={stopCamera} style={{ marginTop: 8 }}>Stop Camera</button>
                      </div>
                    ) : (
                      <div style={{ display: 'flex', gap: 8, marginBottom: 12, width: '100%' }}>
                        <button type="button" className="settings-btn" style={{ flex: 1, fontSize: '0.8rem', padding: '8px 12px' }} onClick={startCamera}>📸 Scan with Camera</button>
                        <label className="settings-btn" style={{ flex: 1, fontSize: '0.8rem', padding: '8px 12px', textAlign: 'center', cursor: 'pointer', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
                          📂 Upload Image
                          <input type="file" accept="image/*" onChange={handleFileUpload} style={{ display: 'none' }} />
                        </label>
                      </div>
                    )}

                    <input placeholder="Link code / Scanned token" value={linkInput} onChange={(e) => setLinkInput(e.target.value.trim())} autoFocus maxLength={200} />
                    <button type="button" disabled={linkBusy || linkInput.length < 8} onClick={() => {
                      setLinkBusy(true)
                      setErr('Waiting for approval on your other device…')
                      const tok = linkInput
                      const poll = setInterval(() => sendRef.current({ type: 'link_check', token: tok }), 2500)
                      sendRef.current({ type: 'link_check', token: tok })
                      setTimeout(() => clearInterval(poll), 5 * 60 * 1000)
                    }}>{linkBusy ? 'Waiting…' : 'Sign in with code'}</button>
                    <button type="button" className="link-btn" onClick={() => { stopCamera(); setMode('login'); setLinkInput(''); setLinkBusy(false); setErr('') }}>Back to sign in</button>
                  </div>
                ) : regStep === 'form' ? (
                  <form className="form" onSubmit={(e) => { e.preventDefault(); doRegister() }}>
                    <input placeholder="Choose a username (3–32 chars)" value={regUser} onChange={(e) => setRegUser(e.target.value)} autoComplete="username" />
                    <input type="email" placeholder="Email" value={regEmail} onChange={(e) => setRegEmail(e.target.value)} autoComplete="email" required />
                    <input type="password" placeholder="Password (8+ chars)" value={regPass} onChange={(e) => setRegPass(e.target.value)} autoComplete="new-password" />
                    <button type="submit">Create account</button>
                    <button type="button" className="link-btn" onClick={() => { setMode('login'); setErr('') }}>Back to sign in</button>
                  </form>
                ) : (
                  <div className="form">
                    <p className="muted small">We sent a verification link to <strong>{regEmail}</strong>. Click it, or enter the code below.</p>
                    <input inputMode="numeric" pattern="\d{6}" maxLength={6} placeholder="123456" value={regCode} onChange={(e) => setRegCode(e.target.value)} />
                    <button type="button" onClick={doVerify}>Verify email</button>
                    <button type="button" className="link-btn" onClick={() => {
                      send({ type: 'resend_verification', sender: regUser, email: regEmail })
                      setErr('Verification code resent. Check your email.')
                    }}>Resend code</button>
                    <button type="button" className="link-btn" onClick={() => { setMode('login'); setErr(''); setRegStep('form') }}>Cancel</button>
                  </div>
                )}
              </section>
            </div>
          </main>
        )}

        {/* ── Hub view (logged in, DMs) ───────────────────────────── */}
        {me && sessionToken && <Suspense fallback={null}><Stories me={me} sessionToken={sessionToken} /></Suspense>}
        {me && (
          <main className="grid">
            <div className={`hub-content ${selected ? 'chat-open' : ''}`}>
              {/* ── Sidebar: add friend + friends list ────────────── */}
              <div className="hub-sidebar">
                <div className="hub-add-friend">
                  <div className="form">
                    <input placeholder="Add friend by username…" value={addFriend} onChange={(e) => setAddFriend(e.target.value)} />
                    <button type="button" onClick={() => { sendFriendRequest(addFriend.trim()); setAddFriend('') }}>Add</button>
                  </div>
                </div>
                <div className="hub-friends">
                  {Object.keys(friends).length === 0 && (
                    <div className="friends-empty">
                      <div className="friends-empty-icon">👋</div>
                      <p><strong>No friends yet</strong></p>
                      <p className="muted small">Add someone by their username above. Once they accept, you can chat, call, and share.</p>
                    </div>
                  )}
                  {Object.keys(friends).length > 0 && <div className="sidebar-section-label">Messages</div>}
                  <ul className="list">
                    {Object.entries(friends)
                      .map(([u, st]) => ({ u, st, last: lastLineFor(me, u) }))
                      .sort((a, b) => (b.last?.ts ?? 0) - (a.last?.ts ?? 0))
                      .map(({ u, st, last }) => (
                      <li key={u}>
                        <button type="button" className={`friend-row ${selected === u ? 'sel' : ''}`} onClick={() => openChat(u)}>
                          <span className="avatar" style={{ background: avatarColor(u) }}>
                            {u[0]?.toUpperCase()}
                            <span className="avatar-dot" data-online={st === 'Online' ? '' : undefined} style={{ background: statusColor(st) }} />
                          </span>
                          <span className="friend-meta">
                            <span className="friend-line">
                              <span className="friend-name">{u}</span>
                              {last && <span className="friend-time">{relTime(last.ts)}</span>}
                            </span>
                            <span className="friend-line">
                              <span className="friend-preview">{last?.text || st}</span>
                              {unread[u] > 0 && selected !== u && (
                                <span className="unread-badge">{unread[u] > 99 ? '99+' : unread[u]}</span>
                              )}
                            </span>
                          </span>
                        </button>
                      </li>
                    ))}
                  </ul>
                  {pending.length > 0 && (
                    <>
                      <h3>Requests</h3>
                      {pending.map((u) => (
                        <div key={u} className="row">
                          <span>{u}</span>
                          <button type="button" onClick={() => acceptFriend(u)}>Accept</button>
                        </div>
                      ))}
                    </>
                  )}
                </div>
              </div>

              {/* ── Chat view ─────────────────────────────────────── */}
              <div className="hub-chat-view">
                <section className="panel grow">
                  <div className="chat-header-bar">
                    {selected ? (
                      <>
                        <button type="button" className="chat-back-btn" onClick={() => setSelected(null)} title="Back to hub">
                          ← Back
                        </button>
                        <span className="status-dot" style={{ background: statusColor(friends[selected] ?? 'Offline') }} />
                        <span className="chat-peer-name clickable" onClick={() => setProfileUser(selected)}>{selected}</span>
                        <span className="chat-peer-status muted small">{friends[selected] ?? 'Offline'}</span>
                        <div className="chat-call-btns">
                          <button
                            type="button"
                            className="chat-call-btn"
                            title={pinnedIds.length > 0 ? `${pinnedIds.length} pinned` : 'No pinned messages'}
                            onClick={() => setPinsOpen((v) => !v)}
                          >📌{pinnedIds.length > 0 ? <span className="header-count">{pinnedIds.length}</span> : null}</button>
                          <button
                            type="button"
                            className="chat-call-btn"
                            title="Search in this chat"
                            onClick={() => setSearchOpen((v) => !v)}
                          >🔍</button>
                          <button
                            type="button"
                            className="chat-call-btn"
                            title={selected && mutedPeers.has(selected) ? 'Unmute notifications' : 'Mute notifications'}
                            onClick={() => selected && togglePeerMute(selected)}
                          >{selected && mutedPeers.has(selected) ? '🔕' : '🔔'}</button>
                          <button
                            type="button"
                            className="chat-call-btn"
                            title="Audio call"
                            onClick={() => void startCall('audio')}
                            disabled={!me}
                          >☎</button>
                          <button
                            type="button"
                            className="chat-call-btn"
                            title="Video call"
                            onClick={() => void startCall('video')}
                            disabled={!me}
                          >📹</button>
                          <button
                            type="button"
                            className="chat-call-btn"
                            title="Group call"
                            onClick={() => {
                              const room = `call_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 6)}`
                              setGroupCallRoom(room)
                              if (selected) {
                                send({ type: 'call_invite', recipient: selected, channel_id: room })
                              }
                            }}
                            disabled={!me}
                          >👥</button>
                          <button
                            type="button"
                            className="chat-call-btn"
                            title={`Block ${selected}`}
                            onClick={() => {
                              if (selected && me && confirm(`Block ${selected}? They won't be able to message you.`)) {
                                send({ type: 'block', sender: me, recipient: selected })
                              }
                            }}
                          >🚫</button>
                          <button
                            type="button"
                            className="chat-call-btn"
                            title={`Report ${selected}`}
                            onClick={() => { setReportTarget(selected); setReportReason(''); setReportSent(false) }}
                          >⚑</button>
                        </div>
                      </>
                    ) : (
                      <span className="muted small">Select a friend to chat</span>
                    )}
                  </div>

                  {selected && searchOpen && (
                    <div className="search-bar">
                      <input
                        autoFocus
                        placeholder="Find in conversation…"
                        value={search}
                        onChange={(e) => setSearch(e.target.value)}
                        onKeyDown={(e) => { if (e.key === 'Escape') { setSearchOpen(false); setSearch('') } }}
                      />
                      {search && (
                        <span className="muted small">{log.filter((l) => !l.deleted && l.text.toLowerCase().includes(search.toLowerCase())).length} matches</span>
                      )}
                      <button type="button" className="link-btn" onClick={() => { setSearch(''); setSearchOpen(false) }}>Close</button>
                    </div>
                  )}

                  {selected && pinsOpen && pinnedIds.length > 0 && (
                    <div className="pinned-strip">
                      <div className="pinned-title">📌 Pinned</div>
                      {log.filter((l) => pinnedIds.includes(l.id)).map((l) => (
                        <button
                          type="button"
                          key={l.id}
                          className="pinned-item"
                          onClick={() => { scrollToMessage(l.id) }}
                          title="Jump to message"
                        >
                          <span className="muted small">{l.from}:</span>{' '}
                          <span>{l.file ? `📎 ${l.file.name}` : (l.deleted ? '[deleted]' : l.text.slice(0, 80))}</span>
                        </button>
                      ))}
                    </div>
                  )}

                  <div className="chat" ref={chatScrollRef}>
                    {!selected && (
                      <div className="chat-empty">
                        <div className="chat-empty-art">
                          <img src={faviconUrl} alt="" />
                        </div>
                        <h3>Stay in phase.</h3>
                        <p>
                          {Object.keys(friends).length === 0
                            ? 'Add a friend by username to get started — once they accept, your conversation appears here.'
                            : 'Pick someone from your friends list to start chatting.'}
                        </p>
                        <p className="chat-empty-hints">
                          <span><kbd>⌘K</kbd> quick switcher</span>
                          <span><kbd>/</kbd> commands</span>
                          <span><kbd>@</kbd> mention</span>
                        </p>
                      </div>
                    )}
                    {(() => {
                      const q = search.trim().toLowerCase()
                      const view = q ? log.filter((l) => !l.deleted && (l.text.toLowerCase().includes(q) || l.from.toLowerCase().includes(q))) : log
                      return view.map((line, i) => {
                      const prev = view[i - 1]
                      const showGap = !prev || (line.ts - prev.ts) > 5 * 60 * 1000 || prev.me !== line.me
                      const isPinned = pinnedIds.includes(line.id)
                      const mentionsMe = !!me && containsMention(line.text, me)
                      return (
                        <div key={line.id} data-msg-id={line.id} className={`bubble-row ${line.me ? 'me' : ''}`}>
                          {!line.me && showGap && (
                            <span className="bubble-avatar" style={{ background: avatarColor(line.from) }}>
                              {line.from[0]?.toUpperCase()}
                            </span>
                          )}
                          {!line.me && !showGap && <span className="bubble-avatar-spacer" />}
                          <div className={`bubble ${line.me ? 'me' : ''} ${line.deleted ? 'deleted' : ''} ${isPinned ? 'pinned' : ''} ${mentionsMe ? 'mentions-me' : ''}`} title={new Date(line.ts).toLocaleString()}>
                            {showGap && !line.me && <span className="who clickable" onClick={() => setProfileUser(line.from)}>{line.from}</span>}
                            {line.deleted ? (
                              <span className="bubble-text deleted-text">message deleted</span>
                            ) : line.file ? (
                              isImage(line.file.mime, line.file.name) ? (
                                <a href={line.file.url} target="_blank" rel="noopener noreferrer" className="bubble-image-link">
                                  <img src={line.file.url} alt={line.file.name} className="bubble-image" loading="lazy" />
                                </a>
                              ) : isVideo(line.file.mime, line.file.name) ? (
                                <video controls preload="metadata" src={line.file.url} className="bubble-video" />
                              ) : isAudio(line.file.mime, line.file.name) ? (
                                <div className="bubble-audio">
                                  <span className="bubble-audio-icon">🎙️</span>
                                  <audio controls preload="metadata" src={line.file.url} />
                                </div>
                              ) : (
                                <a href={line.file.url} target="_blank" rel="noopener noreferrer" className="bubble-file">
                                  <span className="bubble-file-icon">📎</span>
                                  <span className="bubble-file-meta">
                                    <span className="bubble-file-name">{line.file.name}</span>
                                    <span className="bubble-file-size">{fmtBytes(line.file.size)}</span>
                                  </span>
                                </a>
                              )
                            ) : (
                              <span className="bubble-text"><RichText text={line.text} me={me} />{line.edited && <span className="edited-tag"> (edited)</span>}</span>
                            )}
                            <span className="bubble-ts">{formatTime(line.ts)}{line.me && <span className="receipt-tick" title={line.seen ? 'Seen' : 'Delivered'}>{line.seen ? ' ✓✓' : ' ✓'}</span>}</span>
                            {line.reactions && Object.keys(line.reactions).length > 0 && (
                              <div className="reactions">
                                {Object.entries(line.reactions).map(([e, users]) => (
                                  <button
                                    key={e}
                                    type="button"
                                    className={`react-chip ${me && users.includes(me) ? 'mine' : ''}`}
                                    onClick={() => reactTo(line, e)}
                                    title={users.join(', ')}
                                  >{e} {users.length}</button>
                                ))}
                              </div>
                            )}
                            {!line.deleted && (
                              <div className="bubble-actions">
                                {REACTION_EMOJIS.map((e) => (
                                  <button key={e} type="button" className="action-btn react" onClick={() => reactTo(line, e)} title={`React ${e}`}>{e}</button>
                                ))}
                                <button type="button" className="action-btn" onClick={() => togglePin(line)} title={isPinned ? 'Unpin' : 'Pin'}>{isPinned ? '📍' : '📌'}</button>
                                {line.me && !line.file && (
                                  <button type="button" className="action-btn" onClick={() => beginEdit(line)} title="Edit">✏️</button>
                                )}
                                {line.me && (
                                  <button type="button" className="action-btn" onClick={() => deleteMessage(line)} title="Delete">🗑</button>
                                )}
                              </div>
                            )}
                            {isPinned && <span className="pin-indicator" title="Pinned">📌</span>}
                          </div>
                        </div>
                      )
                    })
                    })()}
                    {selected && typingPeers.has(selected) && (
                      <div className="typing-indicator">
                        <span>{selected} is typing</span>
                        <span className="typing-dots"><span /><span /><span /></span>
                      </div>
                    )}
                  </div>

                  {selected && editingId && (
                    <div className="edit-banner">
                      <span>Editing message</span>
                      <button type="button" className="link-btn" onClick={cancelEdit}>Cancel</button>
                    </div>
                  )}

                  {selected && recording && (
                    <div className="edit-banner rec-banner">
                      <span><span className="rec-dot" /> Recording {fmtDuration(recDuration)} — press ⏹ to send, ✕ to cancel</span>
                    </div>
                  )}

                  {selected && (
                    <div className="row send">
                      <input
                        ref={fileInputRef}
                        type="file"
                        style={{ display: 'none' }}
                        onChange={(e) => {
                          const f = e.target.files?.[0]
                          if (f) void sendFile(f)
                          e.target.value = ''
                        }}
                      />
                      <button
                        type="button"
                        className="emoji-btn"
                        title="Attach file"
                        onClick={() => fileInputRef.current?.click()}
                        disabled={!!editingId || recording}
                      >📎</button>
                      <button
                        type="button"
                        className={`emoji-btn mic-btn ${recording ? 'recording' : ''}`}
                        title={recording ? `Stop & send (${fmtDuration(recDuration)})` : 'Record voice message'}
                        onClick={() => void startRecording()}
                        disabled={!!editingId}
                      >{recording ? '⏹' : '🎙️'}</button>
                      {recording && (
                        <button
                          type="button"
                          className="emoji-btn"
                          title="Cancel"
                          onClick={() => stopRecording(true)}
                        >✕</button>
                      )}
                      <div className="emoji-wrap">
                        <button type="button" className="emoji-btn" title="Emoji" onClick={() => setEmojiOpen((v) => !v)}>😊</button>
                        {emojiOpen && (
                          <div className="emoji-picker" role="dialog" aria-label="Emoji picker">
                            {EMOJIS.map((e) => (
                              <button
                                key={e}
                                type="button"
                                className="emoji-cell"
                                onClick={() => { setDraft((d) => d + e); setEmojiOpen(false) }}
                              >{e}</button>
                            ))}
                          </div>
                        )}
                      </div>
                      <div className="draft-wrap">
                        <input
                          ref={draftInputRef}
                          value={draft}
                          onChange={handleDraftChange}
                          onKeyDown={(e) => {
                            if (slashMatches.length > 0) {
                              if (e.key === 'ArrowDown') { e.preventDefault(); setSlashIdx((i) => (i + 1) % slashMatches.length); return }
                              if (e.key === 'ArrowUp') { e.preventDefault(); setSlashIdx((i) => (i - 1 + slashMatches.length) % slashMatches.length); return }
                              if (e.key === 'Tab') { e.preventDefault(); setDraft(slashMatches[slashIdx].cmd + ' '); setSlashIdx(0); return }
                            }
                            if (mentionMatches.length > 0 && mentionQuery !== null) {
                              if (e.key === 'ArrowDown') { e.preventDefault(); setMentionIdx((i) => (i + 1) % mentionMatches.length); return }
                              if (e.key === 'ArrowUp') { e.preventDefault(); setMentionIdx((i) => (i - 1 + mentionMatches.length) % mentionMatches.length); return }
                              if (e.key === 'Tab' || e.key === 'Enter') { e.preventDefault(); completeMention(mentionMatches[mentionIdx]); return }
                              if (e.key === 'Escape') { setMentionQuery(null); return }
                            }
                            if (e.key === 'Enter' && conn === 'open') sendChat()
                            else if (e.key === 'Escape' && editingId) cancelEdit()
                          }}
                          placeholder={editingId ? 'Edit message…' : (e2eReady ? 'Message  ·  / for commands  ·  @ to mention' : 'Message')}
                        />
                        {slashMatches.length > 0 && (
                          <div className="mention-pop slash-pop" role="dialog" aria-label="Slash commands">
                            {slashMatches.map((c, i) => (
                              <button
                                key={c.cmd}
                                type="button"
                                className={`mention-row ${i === slashIdx ? 'on' : ''}`}
                                onMouseDown={(e) => { e.preventDefault(); setDraft(c.cmd + ' '); setSlashIdx(0); draftInputRef.current?.focus() }}
                                onMouseEnter={() => setSlashIdx(i)}
                              >
                                <span className="slash-cmd mono">{c.cmd}</span>
                                <span className="slash-desc">{c.desc}</span>
                              </button>
                            ))}
                          </div>
                        )}
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
                                <span className="avatar small" style={{ background: avatarColor(u) }}>{u[0]?.toUpperCase()}</span>
                                <span>{u}</span>
                              </button>
                            ))}
                          </div>
                        )}
                      </div>
                      <button type="button" onClick={sendChat} disabled={conn !== 'open'} title={conn !== 'open' ? 'Reconnecting…' : undefined}>{editingId ? 'Save' : 'Send'}</button>
                    </div>
                  )}
                </section>
              </div>
            </div>
          </main>
        )}
        </>
      )}

      <footer className="foot">
        <div className="foot-inner">
          <button
            type="button"
            className="foot-cta"
            onClick={() => setShowSupport(true)}
          >☕ Support Phaze</button>
          <div className="foot-links">
            <a href="https://twitter.com/PhazeChatWorld" target="_blank" rel="noopener noreferrer">Twitter</a>
            <span className="foot-dot" />
            <a href="https://instagram.com/phazechat.world" target="_blank" rel="noopener noreferrer">Instagram</a>
            <span className="foot-dot" />
            <a href="https://github.com/nickshouse/Phaze" target="_blank" rel="noopener noreferrer">GitHub</a>
          </div>
          <span className="foot-copy">Phaze — encrypted chat for everyone</span>
        </div>
      </footer>
    </div>
  )
}
