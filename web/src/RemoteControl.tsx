import { useCallback, useEffect, useRef, useState } from 'react'
import type { NexusMessage, TurnConfig } from './nexusTypes'
import './remote.css'

interface Props {
  me: string
  send: (m: NexusMessage) => void
  subscribe: (handler: (m: NexusMessage) => void) => () => void
  turn: TurnConfig | null
  onClose: () => void
}

type Mode = 'idle' | 'hosting' | 'viewing'

function genCode(): string {
  return String(Math.floor(100000 + Math.random() * 900000))
}

interface FileTransfer {
  name: string
  size: number
  received: number
  chunks: Uint8Array[]
  done: boolean
}

export default function RemoteControl({ send, subscribe, turn, onClose }: Props) {
  const [mode, setMode] = useState<Mode>('idle')
  const [code, setCode] = useState('')
  const [joinCode, setJoinCode] = useState('')
  const [peer, setPeer] = useState<string | null>(null)
  const [err, setErr] = useState('')
  const [connected, setConnected] = useState(false)
  const [cursorPos, setCursorPos] = useState<{ x: number; y: number } | null>(null)
  const [fileProgress, setFileProgress] = useState<FileTransfer | null>(null)

  const pcRef = useRef<RTCPeerConnection | null>(null)
  const inputChannelRef = useRef<RTCDataChannel | null>(null)
  const fileChannelRef = useRef<RTCDataChannel | null>(null)
  const localStreamRef = useRef<MediaStream | null>(null)
  const videoRef = useRef<HTMLVideoElement | null>(null)
  const stageRef = useRef<HTMLDivElement | null>(null)
  const fileInputRef = useRef<HTMLInputElement | null>(null)
  const incomingFileRef = useRef<FileTransfer | null>(null)

  const turnRef = useRef(turn)
  useEffect(() => { turnRef.current = turn }, [turn])

  const iceServers = (): RTCIceServer[] => {
    const t = turnRef.current
    if (t) return [{ urls: t.url, username: t.username, credential: t.password }]
    return [{ urls: 'stun:stun.l.google.com:19302' }]
  }

  const tearDown = useCallback(() => {
    pcRef.current?.close()
    pcRef.current = null
    inputChannelRef.current = null
    fileChannelRef.current = null
    localStreamRef.current?.getTracks().forEach((t) => t.stop())
    localStreamRef.current = null
    setConnected(false)
    setCursorPos(null)
    setFileProgress(null)
    incomingFileRef.current = null
  }, [])

  const handleIncomingFile = useCallback((data: ArrayBuffer) => {
    const view = new Uint8Array(data)
    const decoder = new TextDecoder()

    if (view[0] === 0x01) {
      const meta = JSON.parse(decoder.decode(view.slice(1))) as { name: string; size: number }
      const ft: FileTransfer = { name: meta.name, size: meta.size, received: 0, chunks: [], done: false }
      incomingFileRef.current = ft
      setFileProgress({ ...ft })
      return
    }

    if (view[0] === 0x02 && incomingFileRef.current) {
      const chunk = view.slice(1)
      incomingFileRef.current.chunks.push(chunk)
      incomingFileRef.current.received += chunk.byteLength
      setFileProgress({ ...incomingFileRef.current })
      return
    }

    if (view[0] === 0x03 && incomingFileRef.current) {
      incomingFileRef.current.done = true
      setFileProgress({ ...incomingFileRef.current })
      const blob = new Blob(incomingFileRef.current.chunks.map((c) => c.buffer as ArrayBuffer))
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = incomingFileRef.current.name
      a.click()
      URL.revokeObjectURL(url)
      setTimeout(() => { incomingFileRef.current = null; setFileProgress(null) }, 2000)
    }
  }, [])

  useEffect(() => {
    const unsub = subscribe((m) => {
      switch (m.type) {
        case 'remote_lookup_result': {
          if (m.status === 'ok' && m.body) {
            const host = m.body
            setPeer(host)
            const pc = new RTCPeerConnection({ iceServers: iceServers() })
            pcRef.current = pc

            const inputCh = pc.createDataChannel('input')
            inputChannelRef.current = inputCh

            const fileCh = pc.createDataChannel('file')
            fileCh.binaryType = 'arraybuffer'
            fileChannelRef.current = fileCh
            fileCh.onmessage = (ev) => handleIncomingFile(ev.data as ArrayBuffer)

            pc.ontrack = (e) => {
              if (videoRef.current && e.streams[0]) videoRef.current.srcObject = e.streams[0]
            }
            pc.onicecandidate = (e) => {
              if (e.candidate) send({ type: 'remote_ice', recipient: host, candidate: JSON.stringify(e.candidate) })
            }
            pc.onconnectionstatechange = () => {
              if (pc.connectionState === 'connected') setConnected(true)
              if (pc.connectionState === 'disconnected' || pc.connectionState === 'failed') {
                tearDown()
                setMode('idle')
                setErr('Connection lost')
              }
            }
            void (async () => {
              const offer = await pc.createOffer()
              await pc.setLocalDescription(offer)
              send({ type: 'remote_offer', recipient: host, sdp: offer.sdp })
            })()
          } else {
            setMode('idle')
            setErr(m.error || 'Invalid code')
          }
          break
        }
        case 'remote_offer': {
          if (mode !== 'hosting' || !m.sender) break
          setPeer(m.sender)
          const pc = new RTCPeerConnection({ iceServers: iceServers() })
          pcRef.current = pc
          const stream = localStreamRef.current
          if (stream) stream.getTracks().forEach((t) => pc.addTrack(t, stream))

          pc.ondatachannel = (e) => {
            if (e.channel.label === 'input') {
              inputChannelRef.current = e.channel
              e.channel.onmessage = (ev) => {
                try {
                  const evt = JSON.parse(ev.data as string) as { type: string; x?: number; y?: number }
                  if (evt.type === 'mousemove' && evt.x != null && evt.y != null) {
                    setCursorPos({ x: evt.x, y: evt.y })
                  }
                } catch { /* ignore */ }
              }
            }
            if (e.channel.label === 'file') {
              fileChannelRef.current = e.channel
              e.channel.binaryType = 'arraybuffer'
              e.channel.onmessage = (ev) => handleIncomingFile(ev.data as ArrayBuffer)
            }
          }
          pc.onicecandidate = (e) => {
            if (e.candidate) send({ type: 'remote_ice', recipient: m.sender!, candidate: JSON.stringify(e.candidate) })
          }
          pc.onconnectionstatechange = () => {
            if (pc.connectionState === 'connected') setConnected(true)
            if (pc.connectionState === 'disconnected' || pc.connectionState === 'failed') {
              tearDown()
              setMode('idle')
              setErr('Remote session ended')
            }
          }
          void (async () => {
            await pc.setRemoteDescription({ type: 'offer', sdp: m.sdp })
            const answer = await pc.createAnswer()
            await pc.setLocalDescription(answer)
            send({ type: 'remote_answer', recipient: m.sender!, sdp: answer.sdp })
          })()
          break
        }
        case 'remote_answer': {
          if (mode !== 'viewing' || !pcRef.current) break
          void pcRef.current.setRemoteDescription({ type: 'answer', sdp: m.sdp })
          setConnected(true)
          break
        }
        case 'remote_ice': {
          if (!pcRef.current || !m.candidate) break
          void pcRef.current.addIceCandidate(JSON.parse(m.candidate)).catch(() => {})
          break
        }
        case 'remote_end': {
          tearDown()
          setMode('idle')
          setPeer(null)
          setErr('Remote session ended by peer')
          break
        }
        case 'remote_error': {
          setErr(m.error || 'Connection failed')
          break
        }
      }
    })
    return unsub
  }, [subscribe, mode, peer, handleIncomingFile, tearDown, send])

  const startHosting = async () => {
    setErr('')
    let stream: MediaStream
    try {
      stream = await navigator.mediaDevices.getDisplayMedia({ video: true, audio: true })
    } catch {
      setErr('Screen share cancelled')
      return
    }
    localStreamRef.current = stream
    const sessionCode = genCode()
    setCode(sessionCode)
    send({ type: 'remote_register', body: sessionCode })
    setMode('hosting')
    stream.getTracks().forEach((t) => {
      t.onended = () => {
        send({ type: 'remote_unregister' })
        if (peer) send({ type: 'remote_end', recipient: peer })
        tearDown()
        setMode('idle')
      }
    })
  }

  const startViewing = () => {
    setErr('')
    if (joinCode.length !== 6) { setErr('Enter a 6-digit code'); return }
    send({ type: 'remote_lookup', body: joinCode })
    setMode('viewing')
  }

  const endSession = () => {
    send({ type: 'remote_unregister' })
    if (peer) send({ type: 'remote_end', recipient: peer })
    tearDown()
    setMode('idle')
    setPeer(null)
    setCode('')
    setJoinCode('')
  }

  useEffect(() => {
    return () => {
      if (peer) send({ type: 'remote_end', recipient: peer })
      tearDown()
    }
  }, [peer, send, tearDown])

  const sendInputEvent = (payload: string) => {
    if (inputChannelRef.current && inputChannelRef.current.readyState === 'open') {
      inputChannelRef.current.send(payload)
    }
  }

  const sendMouseEvent = (e: React.MouseEvent) => {
    const rect = stageRef.current?.getBoundingClientRect()
    if (!rect || !peer) return
    const x = (e.clientX - rect.left) / rect.width
    const y = (e.clientY - rect.top) / rect.height
    sendInputEvent(JSON.stringify({ type: e.type, x, y, button: e.button }))
  }

  const sendKeyEvent = (e: React.KeyboardEvent) => {
    if (!peer) return
    sendInputEvent(JSON.stringify({ type: e.type, key: e.key, code: e.code, shift: e.shiftKey, ctrl: e.ctrlKey, alt: e.altKey }))
    e.preventDefault()
  }

  const sendFile = async (file: File) => {
    const ch = fileChannelRef.current
    if (!ch || ch.readyState !== 'open') return

    const meta = new TextEncoder().encode(JSON.stringify({ name: file.name, size: file.size }))
    const header = new Uint8Array(1 + meta.length)
    header[0] = 0x01
    header.set(meta, 1)
    ch.send(header)

    const CHUNK = 16384
    const buf = await file.arrayBuffer()
    for (let off = 0; off < buf.byteLength; off += CHUNK) {
      const slice = new Uint8Array(buf.slice(off, off + CHUNK))
      const pkt = new Uint8Array(1 + slice.length)
      pkt[0] = 0x02
      pkt.set(slice, 1)
      ch.send(pkt)
      if (ch.bufferedAmount > 1024 * 1024) {
        await new Promise<void>((r) => { ch.onbufferedamountlow = () => r() })
      }
    }

    ch.send(new Uint8Array([0x03]))
  }

  return (
    <div className="rc-overlay" onClick={(e) => { if (e.target === e.currentTarget && mode === 'idle') onClose() }}>
      <div className="rc-modal">
        <button type="button" className="rc-close" onClick={onClose}>✕</button>
        <h2 className="rc-title">Remote Control</h2>
        <p className="rc-sub">Share your screen with remote input — like TeamViewer, but encrypted and built into Phaze.</p>

        {mode === 'idle' && (
          <div className="rc-options">
            <div className="rc-card">
              <h3>Share your screen</h3>
              <p>Let someone view and control your screen. You'll get a 6-digit code to share.</p>
              <button type="button" className="rc-btn primary" onClick={() => void startHosting()}>🖥 Start sharing</button>
            </div>
            <div className="rc-card">
              <h3>Connect to a screen</h3>
              <p>Enter the 6-digit code from the person sharing their screen.</p>
              <input
                type="text"
                inputMode="numeric"
                maxLength={6}
                placeholder="000000"
                value={joinCode}
                onChange={(e) => setJoinCode(e.target.value.replace(/\D/g, ''))}
                className="rc-code-input"
                onKeyDown={(e) => { if (e.key === 'Enter') startViewing() }}
              />
              <button type="button" className="rc-btn" onClick={startViewing} disabled={joinCode.length !== 6}>Connect</button>
            </div>
          </div>
        )}

        {mode === 'hosting' && (
          <div className="rc-session">
            <div className="rc-code-display">
              <span className="rc-code-label">Your code</span>
              <span className="rc-code-value">{code}</span>
              <span className="rc-code-hint">{connected ? `Connected to ${peer}` : 'Share this code with the person connecting'}</span>
            </div>
            {connected && cursorPos && (
              <div className="rc-cursor-indicator">
                Remote cursor at ({Math.round(cursorPos.x * 100)}%, {Math.round(cursorPos.y * 100)}%)
              </div>
            )}
            <div className="rc-session-actions">
              <input ref={fileInputRef} type="file" style={{ display: 'none' }} onChange={(e) => {
                const f = e.target.files?.[0]
                if (f) void sendFile(f)
                e.target.value = ''
              }} />
              {connected && <button type="button" className="rc-btn" onClick={() => fileInputRef.current?.click()}>📁 Send file</button>}
              <button type="button" className="rc-btn danger" onClick={endSession}>End session</button>
            </div>
          </div>
        )}

        {mode === 'viewing' && (
          <div className="rc-viewer">
            {!connected && <div className="rc-connecting">Connecting…</div>}
            <div
              ref={stageRef}
              className="rc-stage"
              onMouseMove={sendMouseEvent}
              onClick={sendMouseEvent}
              onDoubleClick={sendMouseEvent}
              onContextMenu={(e) => { e.preventDefault(); sendMouseEvent(e) }}
              onKeyDown={sendKeyEvent}
              onKeyUp={sendKeyEvent}
              tabIndex={0}
            >
              <video ref={videoRef} autoPlay playsInline className="rc-video" />
            </div>
            <div className="rc-viewer-bar">
              <span>{connected ? `Connected to ${peer}` : 'Connecting…'}</span>
              <input ref={fileInputRef} type="file" style={{ display: 'none' }} onChange={(e) => {
                const f = e.target.files?.[0]
                if (f) void sendFile(f)
                e.target.value = ''
              }} />
              {connected && <button type="button" className="rc-btn" onClick={() => fileInputRef.current?.click()}>📁 Send file</button>}
              <button type="button" className="rc-btn danger" onClick={endSession}>Disconnect</button>
            </div>
          </div>
        )}

        {fileProgress && (
          <div className="rc-file-progress">
            <span>📁 {fileProgress.name}</span>
            <div className="rc-progress-bar">
              <div className="rc-progress-fill" style={{ width: `${fileProgress.size > 0 ? (fileProgress.received / fileProgress.size * 100) : 0}%` }} />
            </div>
            <span className="rc-progress-text">{fileProgress.done ? 'Downloaded' : `${Math.round(fileProgress.received / 1024)}KB / ${Math.round(fileProgress.size / 1024)}KB`}</span>
          </div>
        )}

        {err && <p className="rc-err">{err}</p>}
      </div>
    </div>
  )
}
