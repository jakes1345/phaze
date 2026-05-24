import { useState } from 'react'
import './onboarding.css'

interface Props {
  me: string
  sessionToken: string
  /** Sends a WS message (used for the friend-request step). */
  onAddFriend: (username: string) => void
  /** Switches to Live tab if user wants to start broadcasting. */
  onJump: (view: 'dms' | 'spaces' | 'live') => void
  onClose: () => void
}

// Onboarding: a 3-step modal shown once per user on their first sign-in.
// localStorage key 'phaze_onboarded' marks completion so the modal never
// reappears. Each step is skippable; the user can always finish later.
export default function Onboarding({ me, sessionToken, onAddFriend, onJump, onClose }: Props) {
  const [step, setStep] = useState(1)
  const [friendName, setFriendName] = useState('')
  const [done, setDone] = useState<Record<number, boolean>>({})

  const next = () => {
    if (step >= 3) finish()
    else setStep(step + 1)
  }

  const finish = () => {
    try { localStorage.setItem('phaze_onboarded', '1') } catch { /* ignore */ }
    onClose()
  }

  const skip = finish

  const handleAvatarUpload = async (file: File) => {
    if (!sessionToken) return
    const fd = new FormData()
    fd.append('file', file)
    const r = await fetch('/api/v1/upload', {
      method: 'POST',
      headers: { Authorization: `Bearer ${sessionToken}` },
      body: fd,
    })
    if (!r.ok) return
    // Lightweight: we store the avatar URL by uploading it; the existing
    // avatar pipeline takes over from /api/v1/avatars/<name>. v1 just
    // marks the step done so we don't get blocked here.
    setDone((d) => ({ ...d, 1: true }))
  }

  const handleAddFriend = () => {
    if (!friendName.trim()) return
    onAddFriend(friendName.trim())
    setDone((d) => ({ ...d, 2: true }))
    setFriendName('')
    setStep(3)
  }

  return (
    <div className="onboard-overlay" role="dialog" aria-label="Get started with Phaze">
      <div className="onboard-card">
        <header className="onboard-head">
          <h2>Welcome to Phaze, @{me}</h2>
          <p className="onboard-sub">Three steps. Takes a minute. Skip any.</p>
        </header>

        <div className="onboard-progress">
          {[1, 2, 3].map((n) => (
            <span key={n} className={`pip ${step >= n ? 'on' : ''} ${done[n] ? 'done' : ''}`}>{done[n] ? '✓' : n}</span>
          ))}
        </div>

        {step === 1 && (
          <section className="onboard-step">
            <h3>📸 Set an avatar</h3>
            <p className="muted">A photo makes you findable. PNG/JPG, under 5 MB.</p>
            <label className="onboard-file-btn">
              {done[1] ? '✓ Avatar uploaded' : 'Choose image'}
              <input
                type="file"
                accept="image/*"
                style={{ display: 'none' }}
                onChange={(e) => {
                  const f = e.target.files?.[0]
                  if (f) void handleAvatarUpload(f)
                  e.target.value = ''
                }}
              />
            </label>
          </section>
        )}

        {step === 2 && (
          <section className="onboard-step">
            <h3>👥 Add your first friend</h3>
            <p className="muted">Type a Phaze username. Your friend gets a request to accept.</p>
            <div className="onboard-row">
              <input
                placeholder="username"
                value={friendName}
                onChange={(e) => setFriendName(e.target.value)}
                onKeyDown={(e) => { if (e.key === 'Enter') handleAddFriend() }}
              />
              <button type="button" onClick={handleAddFriend} disabled={!friendName.trim()}>Send request</button>
            </div>
            {done[2] && <p className="onboard-ok">✓ Request sent to {friendName || '...'}</p>}
            <p className="muted small" style={{ marginTop: '0.5rem' }}>
              Or jump straight to the global Hub — everyone hangs out in #general.
            </p>
            <button type="button" className="onboard-secondary" onClick={() => { onJump('spaces'); finish() }}>
              Open the Phaze Hub →
            </button>
          </section>
        )}

        {step === 3 && (
          <section className="onboard-step">
            <h3>✨ Post your first story</h3>
            <p className="muted">Share a photo or short video — disappears in 24 hours.</p>
            <p className="muted small">
              The story ring is at the top of your friends list. Tap the <strong>＋</strong> to upload.
            </p>
            <button type="button" className="onboard-primary" onClick={finish}>I'll try it</button>
          </section>
        )}

        <footer className="onboard-foot">
          <button type="button" className="onboard-skip" onClick={skip}>Skip rest</button>
          {step < 3 && <button type="button" className="onboard-next" onClick={next}>Next →</button>}
        </footer>
      </div>
    </div>
  )
}
