import { useEffect, useRef, useState } from 'react'
import type { NexusMessage, TurnConfig } from './nexusTypes'

interface Props {
  me: string
  channelId: string
  channelName: string
  send: (m: NexusMessage) => void
  subscribe: (handler: (m: NexusMessage) => void) => () => void
  turn: TurnConfig | null
}

interface PeerState {
  pc: RTCPeerConnection
  audio: HTMLAudioElement
  stream: MediaStream | null
}

// Voice channel mesh: each participant maintains an RTCPeerConnection to
// every other participant in the same room. Caller side is the lexicographically
// smaller username (deterministic — no double offers).
export default function VoiceRoom({ me, channelId, channelName, send, subscribe, turn }: Props) {
  const [peers, setPeers] = useState<string[]>([])
  const [joined, setJoined] = useState(false)
  const [muted, setMuted] = useState(false)
  const [cameraOn, setCameraOn] = useState(false)
  const [hasCamera, setHasCamera] = useState(false)
  const [peerStreams, setPeerStreams] = useState<Record<string, MediaStream>>({})
  const [localStream, setLocalStream] = useState<MediaStream | null>(null)
  const [err, setErr] = useState('')

  const localStreamRef = useRef<MediaStream | null>(null)
  const peerMapRef = useRef<Map<string, PeerState>>(new Map())
  const peersRef = useRef<string[]>([])
  const channelIdRef = useRef(channelId)
  const turnRef = useRef<TurnConfig | null>(turn)

  useEffect(() => { channelIdRef.current = channelId }, [channelId])
  useEffect(() => { turnRef.current = turn }, [turn])
  useEffect(() => { peersRef.current = peers }, [peers])

  const iceServers = (): RTCIceServer[] => {
    const t = turnRef.current
    if (t) return [{ urls: t.url, username: t.username, credential: t.password }]
    return [{ urls: 'stun:stun.l.google.com:19302' }]
  }

  const closePeer = (user: string) => {
    const p = peerMapRef.current.get(user)
    if (p) {
      p.pc.close()
      p.audio.srcObject = null
      p.audio.remove()
      peerMapRef.current.delete(user)
    }
  }

  const ensurePeer = (user: string): PeerState => {
    let p = peerMapRef.current.get(user)
    if (p) return p
    const pc = new RTCPeerConnection({ iceServers: iceServers() })
    const audio = document.createElement('audio')
    audio.autoplay = true
    document.body.appendChild(audio)
    pc.ontrack = (e) => {
      if (e.streams[0]) {
        audio.srcObject = e.streams[0]
        const p2 = peerMapRef.current.get(user)
        if (p2) p2.stream = e.streams[0]
        setPeerStreams((s) => ({ ...s, [user]: e.streams[0] }))
      }
    }
    pc.onicecandidate = (e) => {
      if (e.candidate) {
        send({
          type: 'voice_signal',
          recipient: user,
          channel_id: channelIdRef.current,
          body: 'ice',
          candidate: JSON.stringify(e.candidate),
        })
      }
    }
    const local = localStreamRef.current
    if (local) local.getTracks().forEach((t) => pc.addTrack(t, local))
    p = { pc, audio, stream: null }
    peerMapRef.current.set(user, p)
    return p
  }

  const initiateOffer = async (user: string) => {
    const { pc } = ensurePeer(user)
    const offer = await pc.createOffer()
    await pc.setLocalDescription(offer)
    send({
      type: 'voice_signal',
      recipient: user,
      channel_id: channelIdRef.current,
      body: 'offer',
      sdp: offer.sdp,
    })
  }

  const handleSignal = async (m: NexusMessage) => {
    if (m.channel_id !== channelIdRef.current || !m.sender) return
    const user = m.sender
    if (m.body === 'offer') {
      const { pc } = ensurePeer(user)
      await pc.setRemoteDescription({ type: 'offer', sdp: m.sdp })
      const answer = await pc.createAnswer()
      await pc.setLocalDescription(answer)
      send({
        type: 'voice_signal',
        recipient: user,
        channel_id: channelIdRef.current,
        body: 'answer',
        sdp: answer.sdp,
      })
    } else if (m.body === 'answer') {
      const p = peerMapRef.current.get(user)
      if (p) await p.pc.setRemoteDescription({ type: 'answer', sdp: m.sdp })
    } else if (m.body === 'ice') {
      const p = peerMapRef.current.get(user)
      if (p && m.candidate) {
        try { await p.pc.addIceCandidate(JSON.parse(m.candidate)) } catch { /* stale */ }
      }
    }
  }

  const tearDown = () => {
    peerMapRef.current.forEach((_, u) => closePeer(u))
    peerMapRef.current.clear()
    localStreamRef.current?.getTracks().forEach((t) => t.stop())
    localStreamRef.current = null
    setLocalStream(null)
    setPeerStreams({})
    setHasCamera(false)
    setCameraOn(false)
    setJoined(false)
    setPeers([])
  }

  const leave = () => {
    if (joined) send({ type: 'voice_leave', channel_id: channelIdRef.current })
    tearDown()
  }

  const join = async (withVideo: boolean) => {
    setErr('')
    let stream: MediaStream
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: true, video: withVideo })
    } catch {
      if (withVideo) {
        // Fall back to audio-only if camera denied/unavailable.
        try {
          stream = await navigator.mediaDevices.getUserMedia({ audio: true })
        } catch {
          setErr('Microphone permission denied')
          return
        }
      } else {
        setErr('Microphone permission denied')
        return
      }
    }
    localStreamRef.current = stream
    setLocalStream(stream)
    const vTrack = stream.getVideoTracks()[0]
    if (vTrack) {
      setHasCamera(true)
      setCameraOn(true)
    }
    setJoined(true)
    send({ type: 'voice_join', channel_id: channelIdRef.current })
  }

  const toggleCamera = () => {
    const track = localStreamRef.current?.getVideoTracks()[0]
    if (!track) return
    const next = !cameraOn
    track.enabled = next
    setCameraOn(next)
  }

  const toggleMute = () => {
    const next = !muted
    setMuted(next)
    localStreamRef.current?.getAudioTracks().forEach((t) => { t.enabled = !next })
  }

  // Subscribe to voice protocol messages once.
  useEffect(() => {
    const unsub = subscribe((m) => {
      if (m.type === 'voice_peers' && m.channel_id === channelIdRef.current) {
        const next = m.results ?? []
        const prev = peersRef.current
        // close PCs for peers who left
        for (const u of prev) {
          if (u !== me && !next.includes(u)) closePeer(u)
        }
        // initiate offers to new peers where I'm the lexicographically smaller name
        for (const u of next) {
          if (u === me) continue
          if (!peerMapRef.current.has(u) && me < u) {
            void initiateOffer(u)
          }
        }
        setPeers(next)
      } else if (m.type === 'voice_signal') {
        void handleSignal(m)
      }
    })
    return () => { unsub() }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Leave the room when the channel changes or the component unmounts.
  useEffect(() => {
    return () => { if (joined) send({ type: 'voice_leave', channel_id: channelIdRef.current }); tearDown() }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [channelId])

  return (
    <div className="voice-room">
      <header className="voice-head">
        <h2><span className="hash">🎙</span>{channelName}</h2>
        <p className="voice-sub">{joined ? `${peers.length} connected` : 'Click Join to enter voice or video'}</p>
      </header>
      <div className="voice-peers">
        {(joined ? peers : [me]).map((u) => {
          const isMe = u === me
          const peerStream = !isMe ? peerStreams[u] ?? null : null
          const peerHasVideo = !!peerStream?.getVideoTracks().some((t) => t.readyState === 'live')
          const myVideoTrack = isMe ? localStream?.getVideoTracks()[0] ?? null : null
          const showVideo = (isMe && cameraOn && !!myVideoTrack) || (!isMe && peerHasVideo)
          return (
            <div key={u} className={`voice-peer ${isMe ? 'me' : ''} ${showVideo ? 'has-video' : ''}`}>
              {showVideo ? (
                <video
                  className="voice-video"
                  autoPlay
                  playsInline
                  muted={isMe}
                  ref={(el) => {
                    if (!el) return
                    if (isMe && localStream) el.srcObject = localStream
                    else if (!isMe && peerStream) el.srcObject = peerStream
                  }}
                />
              ) : (
                <div className="voice-avatar">{u[0]?.toUpperCase() ?? '?'}</div>
              )}
              <div className="voice-name">{u}{isMe ? ' (you)' : ''}</div>
              {isMe && muted && <span className="voice-mute-pip" title="Muted">🔇</span>}
            </div>
          )
        })}
      </div>
      <footer className="voice-controls">
        {!joined ? (
          <>
            <button type="button" className="voice-join-btn" onClick={() => void join(false)}>🎙 Join voice</button>
            <button type="button" className="voice-join-btn voice-join-video" onClick={() => void join(true)}>📹 Join with video</button>
          </>
        ) : (
          <>
            <button type="button" className="voice-mute-btn" onClick={toggleMute}>
              {muted ? '🔇 Unmute' : '🎤 Mute'}
            </button>
            {hasCamera && (
              <button type="button" className="voice-mute-btn" onClick={toggleCamera}>
                {cameraOn ? '📷 Camera off' : '📹 Camera on'}
              </button>
            )}
            <button type="button" className="voice-leave-btn" onClick={leave}>📴 Leave</button>
          </>
        )}
      </footer>
      {err && <p className="voice-err">{err}</p>}
    </div>
  )
}
