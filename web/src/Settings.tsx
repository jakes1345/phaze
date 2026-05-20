import { useState, useEffect, useCallback } from 'react'
import type { NexusMessage } from './nexusTypes'
import './settings.css'

type Tab = 'profile' | 'security' | 'privacy' | 'sessions' | 'danger'

interface Session {
  token: string
  full_token: string
  device: string
  created_at: string
}

interface Props {
  me: string
  sessionToken: string | null
  send: (m: NexusMessage) => void
  subscribe: (handler: (m: NexusMessage) => void) => () => void
  onClose: () => void
}

export default function Settings({ me, sessionToken, send, subscribe, onClose }: Props) {
  const [tab, setTab] = useState<Tab>('profile')

  // Profile
  const [displayName, setDisplayName] = useState('')
  const [mood, setMood] = useState('')
  const [status, setStatus] = useState('Online')
  const [profileMsg, setProfileMsg] = useState('')

  // Security
  const [oldPw, setOldPw] = useState('')
  const [newPw, setNewPw] = useState('')
  const [newPw2, setNewPw2] = useState('')
  const [pwMsg, setPwMsg] = useState('')
  const [totpUri, setTotpUri] = useState('')
  const [totpCode, setTotpCode] = useState('')
  const [totpMsg, setTotpMsg] = useState('')
  const [totpPending, setTotpPending] = useState(false)

  // Privacy
  const [blocks, setBlocks] = useState<string[]>([])
  const [blockMsg, setBlockMsg] = useState('')
  const [inviteEmail, setInviteEmail] = useState('')
  const [inviteMsg, setInviteMsg] = useState('')

  // Sessions
  const [sessions, setSessions] = useState<Session[]>([])
  const [sessionsMsg, setSessionsMsg] = useState('')

  // Danger
  const [delConfirm, setDelConfirm] = useState('')
  const [delPw, setDelPw] = useState('')
  const [delMsg, setDelMsg] = useState('')

  useEffect(() => {
    send({ type: 'list_blocks' })
  }, [send])

  useEffect(() => {
    if (tab === 'sessions') send({ type: 'list_sessions' })
  }, [tab, send])

  const onMsg = useCallback((msg: NexusMessage) => {
    switch (msg.type) {
      case 'update_result':
        setProfileMsg(msg.status === 'ok' ? 'Saved.' : (msg.error || 'Error'))
        break
      case 'change_password_result':
        if (msg.status === 'ok') {
          setPwMsg('Password changed.')
          setOldPw(''); setNewPw(''); setNewPw2('')
        } else {
          setPwMsg(msg.error || 'Error')
        }
        break
      case 'totp_result':
        if (msg.status === 'pending_confirm' && msg.totp_uri) {
          setTotpUri(msg.totp_uri)
          setTotpPending(true)
          setTotpMsg('Scan QR in your authenticator app, then enter the code below.')
        } else if (msg.status === 'enabled') {
          setTotpMsg('2FA enabled.')
          setTotpPending(false)
          setTotpUri('')
          setTotpCode('')
        } else if (msg.status === 'disabled') {
          setTotpMsg('2FA disabled.')
          setTotpPending(false)
        } else {
          setTotpMsg(msg.error || 'Error')
        }
        break
      case 'blocks':
        if (msg.results) setBlocks(msg.results)
        break
      case 'block_result':
        if (msg.status === 'unblocked' && msg.recipient) {
          setBlocks((b) => b.filter((x) => x !== msg.recipient))
          setBlockMsg(`Unblocked ${msg.recipient}.`)
        }
        break
      case 'invite_result':
        setInviteMsg(msg.status === 'sent' ? 'Invite sent!' : (msg.error || 'Error'))
        if (msg.status === 'sent') setInviteEmail('')
        break
      case 'sessions_list':
        try { setSessions(JSON.parse(msg.body || '[]') as Session[]) } catch { /* ignore */ }
        break
      case 'session_revoked':
        setSessionsMsg('Session revoked.')
        send({ type: 'list_sessions' })
        break
      case 'delete_account_result':
        if (msg.status !== 'ok') setDelMsg(msg.error || 'Error')
        break
    }
  }, [send])

  useEffect(() => subscribe(onMsg), [subscribe, onMsg])

  const saveProfile = () => {
    setProfileMsg('')
    send({ type: 'update_profile', sender: me, mood, display_name: displayName })
    if (status) send({ type: 'status_update', body: status })
  }

  const changePassword = () => {
    setPwMsg('')
    if (newPw !== newPw2) { setPwMsg('New passwords do not match'); return }
    if (newPw.length < 8) { setPwMsg('New password must be 8+ characters'); return }
    send({ type: 'change_password', body: `${oldPw}:${newPw}` })
  }

  const enableTotp = () => { setTotpMsg(''); send({ type: 'enable_totp' }) }
  const confirmTotp = () => { setTotpMsg(''); send({ type: 'confirm_totp', totp_code: totpCode }) }
  const disableTotp = () => {
    setTotpMsg('')
    if (!oldPw) { setTotpMsg('Enter your current password to disable 2FA'); return }
    send({ type: 'disable_totp', body: oldPw })
  }

  const unblock = (user: string) => { setBlockMsg(''); send({ type: 'unblock', recipient: user }) }

  const sendInvite = () => {
    setInviteMsg('')
    if (!inviteEmail.includes('@')) { setInviteMsg('Enter a valid email'); return }
    send({ type: 'invite_email', email: inviteEmail })
  }

  const revokeSession = (fullToken: string) => {
    setSessionsMsg('')
    send({ type: 'revoke_session_by_token', body: fullToken })
  }

  const exportData = () => {
    if (!sessionToken) return
    const url = `/api/v1/export?token=${encodeURIComponent(sessionToken)}`
    const a = document.createElement('a')
    a.href = url
    a.download = 'phaze-data-export.json'
    a.click()
  }

  const deleteAccount = () => {
    setDelMsg('')
    if (delConfirm !== 'delete my account') { setDelMsg('Type "delete my account" exactly'); return }
    if (!delPw) { setDelMsg('Password required'); return }
    send({ type: 'delete_account', sender: me, body: delPw })
  }

  const tabs: { id: Tab; label: string }[] = [
    { id: 'profile', label: '👤 Profile' },
    { id: 'security', label: '🔒 Security' },
    { id: 'privacy', label: '🛡 Privacy' },
    { id: 'sessions', label: '📱 Sessions' },
    { id: 'danger', label: '⚠ Danger' },
  ]

  return (
    <div className="settings-overlay" onClick={(e) => e.target === e.currentTarget && onClose()}>
      <div className="settings-modal">
        <div className="settings-header">
          <div className="settings-avatar">{me[0].toUpperCase()}</div>
          <div>
            <div className="settings-username">@{me}</div>
            <div className="settings-subtitle">Account settings</div>
          </div>
          <button className="settings-close" onClick={onClose} aria-label="Close">✕</button>
        </div>

        <nav className="settings-tabs">
          {tabs.map(({ id, label }) => (
            <button
              key={id}
              className={`settings-tab ${tab === id ? 'active' : ''} ${id === 'danger' ? 'danger' : ''}`}
              onClick={() => setTab(id)}
            >{label}</button>
          ))}
        </nav>

        <div className="settings-body">
          {/* ── Profile ──────────────────────────────────────── */}
          {tab === 'profile' && (
            <div className="settings-section">
              <label className="settings-label">Display name</label>
              <input className="settings-input" placeholder={me} value={displayName} onChange={(e) => setDisplayName(e.target.value)} maxLength={64} />

              <label className="settings-label">Mood / status message</label>
              <input className="settings-input" placeholder="What's on your mind?" value={mood} onChange={(e) => setMood(e.target.value)} maxLength={140} />

              <label className="settings-label">Presence</label>
              <select className="settings-select" value={status} onChange={(e) => setStatus(e.target.value)}>
                <option value="Online">🟢 Online</option>
                <option value="Away">🟡 Away</option>
                <option value="Do Not Disturb">🔴 Do Not Disturb</option>
                <option value="Invisible">⚫ Invisible</option>
              </select>

              {profileMsg && <p className={`settings-msg ${profileMsg === 'Saved.' ? 'ok' : 'err'}`}>{profileMsg}</p>}
              <button className="settings-btn" onClick={saveProfile}>Save profile</button>
            </div>
          )}

          {/* ── Security ─────────────────────────────────────── */}
          {tab === 'security' && (
            <div className="settings-section">
              <h3 className="settings-section-title">Change password</h3>
              <input className="settings-input" type="password" placeholder="Current password" value={oldPw} onChange={(e) => setOldPw(e.target.value)} autoComplete="current-password" />
              <input className="settings-input" type="password" placeholder="New password (8+ chars)" value={newPw} onChange={(e) => setNewPw(e.target.value)} autoComplete="new-password" />
              <input className="settings-input" type="password" placeholder="Confirm new password" value={newPw2} onChange={(e) => setNewPw2(e.target.value)} autoComplete="new-password" />
              {pwMsg && <p className={`settings-msg ${pwMsg === 'Password changed.' ? 'ok' : 'err'}`}>{pwMsg}</p>}
              <button className="settings-btn" onClick={changePassword}>Change password</button>

              <hr className="settings-divider" />

              <h3 className="settings-section-title">Two-factor authentication (TOTP)</h3>
              {totpMsg && <p className={`settings-msg ${totpMsg.startsWith('2FA') ? 'ok' : 'err'}`}>{totpMsg}</p>}
              {totpUri && (
                <div className="settings-totp-uri">
                  <p className="settings-label">Copy this URI into your authenticator:</p>
                  <code className="settings-totp-code-block">{totpUri}</code>
                  <input className="settings-input" placeholder="Enter 6-digit code to confirm" value={totpCode} onChange={(e) => setTotpCode(e.target.value)} inputMode="numeric" maxLength={6} />
                  <button className="settings-btn" onClick={confirmTotp}>Confirm 2FA</button>
                </div>
              )}
              {!totpPending && (
                <div className="settings-row">
                  <button className="settings-btn" onClick={enableTotp}>Enable 2FA</button>
                  <button className="settings-btn-secondary" onClick={disableTotp}>Disable 2FA</button>
                </div>
              )}
            </div>
          )}

          {/* ── Privacy ──────────────────────────────────────── */}
          {tab === 'privacy' && (
            <div className="settings-section">
              <h3 className="settings-section-title">Invite a friend</h3>
              <p className="settings-empty">Send a Phaze invite to someone who isn't on the platform yet.</p>
              <div className="settings-row">
                <input className="settings-input" type="email" placeholder="friend@example.com" value={inviteEmail} onChange={(e) => setInviteEmail(e.target.value)} />
                <button className="settings-btn" onClick={sendInvite} style={{ whiteSpace: 'nowrap' }}>Send invite</button>
              </div>
              {inviteMsg && <p className={`settings-msg ${inviteMsg === 'Invite sent!' ? 'ok' : 'err'}`}>{inviteMsg}</p>}

              <hr className="settings-divider" />

              <h3 className="settings-section-title">Blocked users</h3>
              {blockMsg && <p className="settings-msg ok">{blockMsg}</p>}
              {blocks.length === 0 ? (
                <p className="settings-empty">No blocked users.</p>
              ) : (
                <ul className="settings-block-list">
                  {blocks.map((u) => (
                    <li key={u} className="settings-block-item">
                      <span>{u}</span>
                      <button className="settings-btn-secondary small" onClick={() => unblock(u)}>Unblock</button>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}

          {/* ── Sessions ─────────────────────────────────────── */}
          {tab === 'sessions' && (
            <div className="settings-section">
              <h3 className="settings-section-title">Active sessions</h3>
              {sessionsMsg && <p className="settings-msg ok">{sessionsMsg}</p>}
              {sessions.length === 0 ? (
                <p className="settings-empty">No active sessions found.</p>
              ) : (
                <ul className="settings-block-list">
                  {sessions.map((s) => (
                    <li key={s.full_token} className="settings-block-item" style={{ flexDirection: 'column', alignItems: 'flex-start', gap: '0.25rem' }}>
                      <div style={{ display: 'flex', width: '100%', justifyContent: 'space-between', alignItems: 'center' }}>
                        <span style={{ fontWeight: 600 }}>{s.device || 'Unknown device'}</span>
                        <button className="settings-btn-secondary small" onClick={() => revokeSession(s.full_token)}>Revoke</button>
                      </div>
                      <span style={{ fontSize: '0.75rem', color: '#6b7280' }}>Token: {s.token} · {s.created_at}</span>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}

          {/* ── Danger ───────────────────────────────────────── */}
          {tab === 'danger' && (
            <div className="settings-section">
              <h3 className="settings-section-title">Export your data</h3>
              <p className="settings-label" style={{ marginBottom: '0.5rem' }}>Download a copy of your profile, friends, and queued messages (GDPR Article 20).</p>
              <button className="settings-btn" onClick={exportData}>Download my data</button>

              <hr className="settings-divider" />

              <h3 className="settings-section-title danger-title">Delete account</h3>
              <p className="settings-label">This permanently erases your account, all messages, friends, and encryption keys. <strong>Cannot be undone.</strong></p>
              <input className="settings-input" placeholder='Type "delete my account" to confirm' value={delConfirm} onChange={(e) => setDelConfirm(e.target.value)} />
              <input className="settings-input" type="password" placeholder="Your password" value={delPw} onChange={(e) => setDelPw(e.target.value)} autoComplete="current-password" />
              {delMsg && <p className="settings-msg err">{delMsg}</p>}
              <button
                className="settings-btn danger"
                onClick={deleteAccount}
                disabled={delConfirm !== 'delete my account' || !delPw}
              >
                Erase my account
              </button>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
