import { useState } from 'react'

interface Props {
  me?: string | null
  bmcUrl: string
  onClose: () => void
}

// SupportForm captures a supporter opt-in (Phaze username + name + email),
// records it server-side so the admin can match it to the Buy Me a Coffee
// payment and grant a badge, then forwards the donor to BMC to actually pay.
export default function SupportForm({ me, bmcUrl, onClose }: Props) {
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [username, setUsername] = useState(me ?? '')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!name.trim() || !email.trim()) { setErr('Name and email are required.'); return }
    setBusy(true)
    setErr('')
    try {
      const r = await fetch('/api/v1/support/request', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username: username.trim(), name: name.trim(), email: email.trim() }),
      })
      const data = (await r.json().catch(() => null)) as { bmc_url?: string } | null
      if (!r.ok) { setErr('Something went wrong. Please try again.'); setBusy(false); return }
      // Off to Buy Me a Coffee to actually pay.
      window.open(data?.bmc_url || bmcUrl, '_blank', 'noopener,noreferrer')
      onClose()
    } catch {
      setErr('Network error. Please try again.')
      setBusy(false)
    }
  }

  return (
    <div className="support-modal" onClick={onClose} role="presentation">
      <div className="support-card" onClick={(e) => e.stopPropagation()}>
        <button type="button" className="support-close" onClick={onClose} aria-label="Close">×</button>
        <h3 className="support-title">💜 Support Phaze</h3>
        <p className="support-sub">
          Pop in your details so we can add your supporter badge after your contribution lands,
          then you'll head to Buy&nbsp;Me&nbsp;a&nbsp;Coffee to chip in.
        </p>
        <form onSubmit={submit} className="support-form">
          <label>
            Phaze username <span className="support-opt">(optional)</span>
            <input value={username} onChange={(e) => setUsername(e.target.value)} placeholder="your @handle" maxLength={64} />
          </label>
          <label>
            Name
            <input value={name} onChange={(e) => setName(e.target.value)} placeholder="What should we call you?" maxLength={120} required />
          </label>
          <label>
            Email
            <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="you@example.com" maxLength={200} required />
          </label>
          {err && <p className="support-err">{err}</p>}
          <button type="submit" className="support-submit" disabled={busy}>
            {busy ? 'One sec…' : '☕ Continue to payment'}
          </button>
        </form>
        <p className="support-fine">No payment info touches our servers — that all happens on Buy&nbsp;Me&nbsp;a&nbsp;Coffee.</p>
      </div>
    </div>
  )
}
