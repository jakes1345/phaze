import { useEffect, useRef, useState } from 'react'
import type { NexusMessage, TurnConfig } from './nexusTypes'
import './live.css'

interface Props {
  me: string
  send: (m: NexusMessage) => void
  subscribe: (handler: (m: NexusMessage) => void) => () => void
  turn: TurnConfig | null
}

interface LiveStream {
  host: string
  title: string
}

interface ViewerPC {
  pc: RTCPeerConnection
}

// LivePage gives users a "Twitch-lite" experience: a Go-Live button to start
// broadcasting your camera+mic, and a list of currently-live broadcasters you
// can click to watch. Mesh broadcast — works up to ~10-15 viewers per stream
// before the broadcaster's uplink becomes the bottleneck. Swap in an SFU
// (LiveKit/mediasoup) when that ceiling becomes real.
export default function LivePage({ me, send, subscribe, turn }: Props) {
  const [streams, setStreams] = useState<LiveStream[]>([])
  const [broadcasting, setBroadcasting] = useState(false)
  const [streamTitle, setStreamTitle] = useState('')
  const [watching, setWatching] = useState<string | null>(null) // broadcaster username
  const [viewerCount, setViewerCount] = useState(0)
  const [err, setErr] = useState('')

  // Refs that close over the latest handlers so the subscribe effect below
  // can call them without creating a TDZ on the function declarations.
  const onViewerJoinedRef = useRef<(viewer: string) => void>(() => {})
  const closeViewerRef = useRef<(viewer: string) => void>(() => {})
  const onSignalRef = useRef<(m: NexusMessage) => void>(() => {})
  const tearDownViewerRef = useRef<() => void>(() => {})
  const watchingRef = useRef<string | null>(null)
  useEffect(() => { watchingRef.current = watching }, [watching])

  // Broadcaster-side: localStream + per-viewer pc map.
  const localStreamRef = useRef<MediaStream | null>(null)
  const [localStream, setLocalStream] = useState<MediaStream | null>(null)
  const viewersRef = useRef<Map<string, ViewerPC>>(new Map())

  // Viewer-side: single pc to the broadcaster + their remote stream.
  const viewerPCRef = useRef<RTCPeerConnection | null>(null)
  const [remoteStream, setRemoteStream] = useState<MediaStream | null>(null)

  const turnRef = useRef<TurnConfig | null>(turn)
  useEffect(() => { turnRef.current = turn }, [turn])

  const iceServers = (): RTCIceServer[] => {
    const t = turnRef.current
    if (t) return [{ urls: t.url, username: t.username, credential: t.password }]
    return [{ urls: 'stun:stun.l.google.com:19302' }]
  }

  // Initial stream list pull + push subscription.
  useEffect(() => {
    send({ type: 'stream_list' })
    const unsub = subscribe((m) => {
      switch (m.type) {
        case 'stream_list_result': {
          const list: LiveStream[] = []
          const r = m.results ?? []
          for (let i = 0; i + 1 < r.length; i += 2) list.push({ host: r[i], title: r[i + 1] })
          setStreams(list)
          break
        }
        case 'stream_viewer_join':
          if (m.sender && localStreamRef.current) onViewerJoinedRef.current(m.sender)
          break
        case 'stream_viewer_leave':
          if (m.sender) closeViewerRef.current(m.sender)
          break
        case 'stream_signal':
          onSignalRef.current(m)
          break
        case 'stream_ended':
          if (watchingRef.current && m.sender === watchingRef.current) {
            tearDownViewerRef.current()
            setWatching(null)
            setErr('Stream ended')
          }
          break
      }
    })
    return () => { unsub() }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // ── Broadcaster paths ──────────────────────────────────────────────
  const onViewerJoined = async (viewer: string) => {
    const local = localStreamRef.current
    if (!local) return
    const pc = new RTCPeerConnection({ iceServers: iceServers() })
    local.getTracks().forEach((t) => pc.addTrack(t, local))
    pc.onicecandidate = (e) => {
      if (e.candidate) send({ type: 'stream_signal', recipient: viewer, body: 'ice', candidate: JSON.stringify(e.candidate) })
    }
    viewersRef.current.set(viewer, { pc })
    setViewerCount(viewersRef.current.size)
    const offer = await pc.createOffer()
    await pc.setLocalDescription(offer)
    send({ type: 'stream_signal', recipient: viewer, body: 'offer', sdp: offer.sdp })
  }

  const closeViewer = (viewer: string) => {
    const v = viewersRef.current.get(viewer)
    if (v) { v.pc.close(); viewersRef.current.delete(viewer); setViewerCount(viewersRef.current.size) }
  }

  const goLive = async () => {
    setErr('')
    let stream: MediaStream
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: true, video: true })
    } catch {
      setErr('Camera/microphone permission denied')
      return
    }
    localStreamRef.current = stream
    setLocalStream(stream)
    setBroadcasting(true)
    send({ type: 'stream_start', body: streamTitle.trim() || `${me}'s stream` })
  }

  const stopLive = () => {
    send({ type: 'stream_stop' })
    viewersRef.current.forEach((v) => v.pc.close())
    viewersRef.current.clear()
    localStreamRef.current?.getTracks().forEach((t) => t.stop())
    localStreamRef.current = null
    setLocalStream(null)
    setBroadcasting(false)
  }

  // ── Viewer paths ───────────────────────────────────────────────────
  const watch = (host: string) => {
    if (broadcasting || watching) return
    setErr('')
    setWatching(host)
    send({ type: 'stream_join', recipient: host })
    // The broadcaster will respond with an SDP offer via stream_signal.
  }

  const onSignal = async (m: NexusMessage) => {
    const sender = m.sender
    if (!sender) return

    // Broadcaster receiving an answer/ice from a specific viewer.
    if (broadcasting) {
      const v = viewersRef.current.get(sender)
      if (!v) return
      if (m.body === 'answer') await v.pc.setRemoteDescription({ type: 'answer', sdp: m.sdp })
      else if (m.body === 'ice' && m.candidate) {
        try { await v.pc.addIceCandidate(JSON.parse(m.candidate)) } catch { /* stale */ }
      }
      return
    }

    // Viewer receiving offer/ice from the broadcaster.
    if (watching && sender === watching) {
      if (m.body === 'offer') {
        const pc = new RTCPeerConnection({ iceServers: iceServers() })
        viewerPCRef.current = pc
        pc.ontrack = (e) => { if (e.streams[0]) setRemoteStream(e.streams[0]) }
        pc.onicecandidate = (e) => {
          if (e.candidate) send({ type: 'stream_signal', recipient: sender, body: 'ice', candidate: JSON.stringify(e.candidate) })
        }
        await pc.setRemoteDescription({ type: 'offer', sdp: m.sdp })
        const answer = await pc.createAnswer()
        await pc.setLocalDescription(answer)
        send({ type: 'stream_signal', recipient: sender, body: 'answer', sdp: answer.sdp })
      } else if (m.body === 'ice' && m.candidate) {
        try { await viewerPCRef.current?.addIceCandidate(JSON.parse(m.candidate)) } catch { /* stale */ }
      }
    }
  }

  const tearDownViewer = () => {
    viewerPCRef.current?.close()
    viewerPCRef.current = null
    setRemoteStream(null)
  }

  useEffect(() => {
    onViewerJoinedRef.current = (v) => { void onViewerJoined(v) }
    closeViewerRef.current = closeViewer
    onSignalRef.current = (m) => { void onSignal(m) }
    tearDownViewerRef.current = tearDownViewer
  })

  const stopWatching = () => {
    if (watching) send({ type: 'stream_leave', recipient: watching })
    tearDownViewer()
    setWatching(null)
  }

  // Tear everything down on unmount.
  useEffect(() => () => {
    if (broadcasting) send({ type: 'stream_stop' })
    if (watching) send({ type: 'stream_leave', recipient: watching })
    viewersRef.current.forEach((v) => v.pc.close())
    viewersRef.current.clear()
    viewerPCRef.current?.close()
    localStreamRef.current?.getTracks().forEach((t) => t.stop())
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <div className="live-page">
      <header className="live-head">
        <h1>🔴 Live</h1>
        <p className="muted small">Go live to broadcast your camera + mic. Click a stream to watch.</p>
      </header>

      {/* ── Viewer mode ────────────────────────────────── */}
      {watching ? (
        <div className="live-viewer">
          <div className="live-stage">
            <video
              autoPlay
              playsInline
              className="live-video-remote"
              ref={(el) => { if (el && remoteStream) el.srcObject = remoteStream }}
            />
            {!remoteStream && <div className="live-loading">Connecting to {watching}…</div>}
          </div>
          <div className="live-meta">
            <span className="live-host">{watching}</span>
            <button type="button" className="live-stop-btn" onClick={stopWatching}>Leave stream</button>
          </div>
        </div>
      ) : broadcasting ? (
        <div className="live-broadcaster">
          <div className="live-stage">
            <video
              autoPlay
              playsInline
              muted
              className="live-video-local"
              ref={(el) => { if (el && localStream) el.srcObject = localStream }}
            />
            <div className="live-on-air">🔴 LIVE — {viewerCount} watching</div>
          </div>
          <div className="live-meta">
            <button type="button" className="live-stop-btn" onClick={stopLive}>End stream</button>
          </div>
        </div>
      ) : (
        <>
          <div className="live-golive">
            <input
              type="text"
              placeholder="Stream title (optional)"
              value={streamTitle}
              onChange={(e) => setStreamTitle(e.target.value)}
              maxLength={80}
            />
            <button type="button" className="live-golive-btn" onClick={() => void goLive()}>📹 Go Live</button>
          </div>

          <h2 className="live-section">Currently live</h2>
          {streams.length === 0 ? (
            <p className="muted">Nobody is live right now. Be the first.</p>
          ) : (
            <div className="live-grid">
              {streams.map((s) => (
                <button key={s.host} type="button" className="live-card" onClick={() => watch(s.host)}>
                  <div className="live-card-thumb"><span>🔴</span></div>
                  <div className="live-card-title">{s.title}</div>
                  <div className="live-card-host">{s.host}</div>
                </button>
              ))}
            </div>
          )}
        </>
      )}

      {err && <p className="live-err">{err}</p>}
    </div>
  )
}
