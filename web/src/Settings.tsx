import { useState, useEffect, useCallback } from 'react'
import QRCode from 'qrcode'
import type { NexusMessage } from './nexusTypes'
import './settings.css'

type Tab = 'profile' | 'security' | 'devices' | 'privacy' | 'sessions' | 'danger' | 'notifications' | 'invite'

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
  onSignOut: () => void
  onSetBackupPin: (pin: string) => Promise<void>
  onDeleteBackup: () => void
  initialTab?: Tab
}

export default function Settings({ me, sessionToken, send, subscribe, onClose, onSignOut, onSetBackupPin, onDeleteBackup, initialTab }: Props) {
  const [tab, setTab] = useState<Tab>(initialTab ?? 'profile')

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
  const [totpBackupCodes, setTotpBackupCodes] = useState<string[] | null>(null)

  // Privacy
  const [blocks, setBlocks] = useState<string[]>([])
  const [blockMsg, setBlockMsg] = useState('')
  const [inviteEmail, setInviteEmail] = useState('')
  const [inviteMsg, setInviteMsg] = useState('')

  // Sessions
  const [sessions, setSessions] = useState<Session[]>([])
  const [sessionsMsg, setSessionsMsg] = useState('')

  // Phone linking
  const [phone, setPhone] = useState('')
  const [phoneCode, setPhoneCode] = useState('')
  const [phonePending, setPhonePending] = useState(false)
  const [phoneMsg, setPhoneMsg] = useState('')

  // Push notifications
  const [pushEnabled, setPushEnabled] = useState(false)
  const [pushMsg, setPushMsg] = useState('')

  // Privacy extra
  const [purgeEmailMsg, setPurgeEmailMsg] = useState('')

  // Danger
  const [delConfirm, setDelConfirm] = useState('')
  const [delPw, setDelPw] = useState('')
  const [delMsg, setDelMsg] = useState('')

  const inviteLink = `https://phazechat.world/web?ref=${encodeURIComponent(me)}`

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
          if (msg.backup_codes?.length) setTotpBackupCodes(msg.backup_codes)
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
        if (msg.status === 'blocked' && msg.recipient) {
          setBlocks((b) => b.includes(msg.recipient!) ? b : [...b, msg.recipient!])
          setBlockMsg(`Blocked ${msg.recipient}.`)
        } else if (msg.status === 'unblocked' && msg.recipient) {
          setBlocks((b) => b.filter((x) => x !== msg.recipient))
          setBlockMsg(`Unblocked ${msg.recipient}.`)
        } else if (msg.error) {
          setBlockMsg(msg.error)
        }
        break
      case 'phone_link_result':
        if (msg.status === 'code_sent') {
          setPhonePending(true)
          setPhoneMsg('Code sent — enter it below.')
        } else if (msg.status === 'verified' || msg.status === 'ok') {
          setPhonePending(false)
          setPhoneMsg('Phone number linked.')
          setPhone('')
          setPhoneCode('')
        } else {
          setPhoneMsg(msg.error || 'Error')
        }
        break
      case 'purge_email_result':
        setPurgeEmailMsg(msg.status === 'ok' ? 'Email removed from account.' : (msg.error || 'Error'))
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
    { id: 'invite', label: '🎁 Invite Friends' },
    { id: 'profile', label: '👤 Profile' },
    { id: 'security', label: '🔒 Security' },
    { id: 'devices', label: '💾 Backup & Devices' },
    { id: 'notifications', label: '🔔 Notifications' },
    { id: 'privacy', label: '🛡 Privacy' },
    { id: 'sessions', label: '📱 Sessions' },
    { id: 'danger', label: '⚠ Danger' },
  ]

  // ── Backup & Devices state ───────────────────────────────────
  const [backupPin1, setBackupPin1] = useState('')
  const [backupPin2, setBackupPin2] = useState('')
  const [backupBusy, setBackupBusy] = useState(false)
  const [backupMsg, setBackupMsg] = useState('')
  const [linkCode, setLinkCode] = useState('')
  const [linkPollBusy, setLinkPollBusy] = useState(false)
  const [linkMsg, setLinkMsg] = useState('')

  // QR login (shows a deep link on this device; another device scans/enters it)
  const [qrToken, setQrToken] = useState('')
  const [qrMsg, setQrMsg] = useState('')

  // Local QR Code states
  const [totpQrCodeDataUrl, setTotpQrCodeDataUrl] = useState('')
  const [qrLoginQrCodeDataUrl, setQrLoginQrCodeDataUrl] = useState('')
  const [approveCodeInput, setApproveCodeInput] = useState('')
  const [approveBusy, setApproveBusy] = useState(false)
  const [approveMsg, setApproveMsg] = useState('')

  useEffect(() => {
    if (totpUri) {
      QRCode.toDataURL(totpUri, { margin: 1, width: 200 })
        .then(url => setTotpQrCodeDataUrl(url))
        .catch(err => console.error(err))
    } else {
      setTotpQrCodeDataUrl('')
    }
  }, [totpUri])

  useEffect(() => {
    if (qrToken) {
      QRCode.toDataURL(`phaze://login?token=${qrToken}`, { margin: 1, width: 200 })
        .then(url => setQrLoginQrCodeDataUrl(url))
        .catch(err => console.error(err))
    } else {
      setQrLoginQrCodeDataUrl('')
    }
  }, [qrToken])

  const handleSetPin = async () => {
    setBackupMsg('')
    if (backupPin1.length < 4) { setBackupMsg('PIN must be at least 4 characters'); return }
    if (backupPin1 !== backupPin2) { setBackupMsg('PINs do not match'); return }
    setBackupBusy(true)
    try {
      await onSetBackupPin(backupPin1)
      setBackupMsg('✓ Backup saved. Now your keys can be restored on any device with this PIN.')
      setBackupPin1('')
      setBackupPin2('')
    } catch (e) {
      setBackupMsg((e as Error).message || 'Failed to save backup')
    } finally {
      setBackupBusy(false)
    }
  }

  const handleDeleteBackup = () => {
    if (!confirm('Delete your encrypted key backup from the server? You will not be able to restore on a new device until you set a new PIN.')) return
    onDeleteBackup()
    setBackupMsg('Backup deleted.')
  }

  const generateLinkCode = () => {
    setLinkMsg('')
    setLinkCode('')
    send({ type: 'link_create' })
  }

  // Subscribe to link_result / link_check responses while devices tab is open.
  useEffect(() => {
    if (tab !== 'devices') return
    const unsub = subscribe((m: NexusMessage) => {
      if (m.type === 'qr_login_result' && m.status === 'pending' && m.qr_token) {
        setQrToken(m.qr_token)
        setQrMsg('Show this code to the device you want to sign in, or enter it in the "Sign in with link code" field.')
      }
      if (m.type === 'link_result' && m.status === 'ok' && m.token) {
        setLinkCode(m.token)
        // Begin polling so we can show "approved" status.
        const poll = setInterval(() => {
          send({ type: 'link_check', token: m.token })
        }, 2500)
        setLinkPollBusy(true)
        ;(window as unknown as { __phazeLinkPoll?: ReturnType<typeof setInterval> }).__phazeLinkPoll = poll
      }
      if (m.type === 'link_result' && m.status !== 'ok' && m.status !== 'approved') {
        // If it's a link_result response for link_approve, handle it:
        setApproveBusy(false)
        if (m.error) {
          setApproveMsg(m.error)
        }
      }
      if (m.type === 'link_result' && m.status === 'approved') {
        // This could be for link_approve response:
        setApproveBusy(false)
        setApproveMsg('✓ Device approved successfully.')
        setApproveCodeInput('')
      }
      if (m.type === 'link_check' && m.status === 'approved') {
        const poll = (window as unknown as { __phazeLinkPoll?: ReturnType<typeof setInterval> }).__phazeLinkPoll
        if (poll) clearInterval(poll)
        setLinkPollBusy(false)
        setLinkMsg('✓ New device linked successfully.')
        setLinkCode('')
      }
    })
    return () => {
      unsub()
      const poll = (window as unknown as { __phazeLinkPoll?: ReturnType<typeof setInterval> }).__phazeLinkPoll
      if (poll) clearInterval(poll)
    }
  }, [tab, subscribe, send])

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
          {/* ── Invite Friends ─────────────────────────────────── */}
          {tab === 'invite' && (
            <div className="settings-section">
              <h3 className="settings-section-title">Share Phaze</h3>
              <p className="settings-label">Every person you bring to Phaze makes the network stronger. Share your personal link or send an email invite.</p>

              <div className="invite-link-box">
                <input className="settings-input" readOnly value={inviteLink} onClick={(e) => (e.target as HTMLInputElement).select()} />
                <button className="settings-btn" onClick={async () => {
                  try {
                    if (typeof navigator.share === 'function') {
                      await navigator.share({ title: 'Join me on Phaze', text: 'Encrypted chat, calls, and more — join me on Phaze!', url: inviteLink })
                    } else {
                      await navigator.clipboard.writeText(inviteLink)
                      setInviteMsg('Link copied!')
                    }
                  } catch { setInviteMsg('Link copied!'); void navigator.clipboard.writeText(inviteLink).catch(() => {}) }
                }}>{typeof navigator.share === 'function' ? 'Share' : 'Copy link'}</button>
              </div>

              <hr className="settings-divider" />

              <h3 className="settings-section-title">Email invite</h3>
              <p className="settings-label">We'll send them an invite with your name on it.</p>
              <div className="invite-link-box">
                <input className="settings-input" type="email" placeholder="friend@example.com" value={inviteEmail} onChange={(e) => setInviteEmail(e.target.value)} />
                <button className="settings-btn" disabled={!inviteEmail.includes('@')} onClick={() => {
                  send({ type: 'invite_email', email: inviteEmail })
                }}>Send invite</button>
              </div>
              {inviteMsg && <p className={`settings-msg ${inviteMsg.includes('!') ? 'ok' : 'err'}`}>{inviteMsg}</p>}
            </div>
          )}

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

              <hr className="settings-divider" />

              <h3 className="settings-section-title">Phone number</h3>
              <p className="settings-empty">Link a phone number for account recovery and two-factor options.</p>
              {phoneMsg && <p className={`settings-msg ${phoneMsg.includes('linked') || phoneMsg.includes('sent') ? 'ok' : 'err'}`}>{phoneMsg}</p>}
              {!phonePending ? (
                <div className="settings-row">
                  <input className="settings-input" type="tel" placeholder="+1 555 000 0000" value={phone} onChange={(e) => setPhone(e.target.value)} />
                  <button className="settings-btn" style={{ whiteSpace: 'nowrap' }} onClick={() => {
                    setPhoneMsg('')
                    if (!phone.trim()) { setPhoneMsg('Enter a phone number'); return }
                    send({ type: 'request_phone_link', sender: me, phone: phone.trim() })
                  }}>Link</button>
                </div>
              ) : (
                <div className="settings-row">
                  <input className="settings-input" type="text" placeholder="Enter SMS code" value={phoneCode} onChange={(e) => setPhoneCode(e.target.value)} inputMode="numeric" maxLength={8} />
                  <button className="settings-btn" style={{ whiteSpace: 'nowrap' }} onClick={() => {
                    setPhoneMsg('')
                    if (!phoneCode.trim()) { setPhoneMsg('Enter the code'); return }
                    send({ type: 'verify_phone_link', sender: me, phone: phone.trim(), body: phoneCode.trim() })
                  }}>Verify</button>
                </div>
              )}
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
                  <p className="settings-label">Scan this QR code with your authenticator app (Google Authenticator, Authy, etc.):</p>
                  <div style={{ textAlign: 'center', margin: '16px 0' }}>
                    {totpQrCodeDataUrl ? (
                      <img
                        src={totpQrCodeDataUrl}
                        alt="2FA QR Code"
                        width={200}
                        height={200}
                        style={{ borderRadius: 8, border: '2px solid #232328', background: '#fff' }}
                      />
                    ) : (
                      <div style={{ width: 200, height: 200, display: 'flex', alignItems: 'center', justifyContent: 'center', margin: '0 auto', background: '#232328', borderRadius: 8 }}>Generating QR code...</div>
                    )}
                  </div>
                  <details style={{ marginBottom: 12 }}>
                    <summary style={{ cursor: 'pointer', fontSize: '0.85rem', color: '#888' }}>Can't scan? Copy the key manually</summary>
                    <code className="settings-totp-code-block" style={{ fontSize: '0.75rem', wordBreak: 'break-all' }}>{totpUri}</code>
                  </details>
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
              {totpBackupCodes && (
                <div className="settings-backup-codes">
                  <p className="settings-label" style={{ fontWeight: 600, marginBottom: 4 }}>Recovery codes</p>
                  <p className="settings-empty" style={{ marginBottom: 10 }}>Save these somewhere safe. Each can be used once if you lose your authenticator.</p>
                  <div className="settings-backup-codes-grid">
                    {totpBackupCodes.map((c) => (
                      <code key={c} className="settings-backup-code">{c}</code>
                    ))}
                  </div>
                  <button className="settings-btn-secondary" style={{ marginTop: 10 }} onClick={() => setTotpBackupCodes(null)}>I've saved these</button>
                </div>
              )}
            </div>
          )}

          {/* ── Backup & Devices ─────────────────────────────── */}
          {tab === 'devices' && (
            <div className="settings-section">
              <h3 className="settings-section-title">Recovery PIN</h3>
              <p className="settings-empty">
                Set a Recovery PIN to encrypt and back up your end-to-end encryption keys on the
                server. The server can never read them — only your PIN can unlock the backup.
                Without this, signing in on a new device or after clearing your browser will
                give you a fresh identity (your old messages stay readable on the original device only).
              </p>
              <input className="settings-input" type="password" placeholder="Recovery PIN (4+ chars)" value={backupPin1} onChange={(e) => setBackupPin1(e.target.value)} />
              <input className="settings-input" type="password" placeholder="Confirm PIN" value={backupPin2} onChange={(e) => setBackupPin2(e.target.value)} />
              {backupMsg && <p className={`settings-msg ${backupMsg.startsWith('✓') ? 'ok' : 'err'}`}>{backupMsg}</p>}
              <div className="settings-row">
                <button className="settings-btn" onClick={() => void handleSetPin()} disabled={backupBusy}>{backupBusy ? 'Saving…' : 'Save backup'}</button>
                <button className="settings-btn-secondary" onClick={handleDeleteBackup}>Delete backup</button>
              </div>

              <hr className="settings-divider" />

              <h3 className="settings-section-title">Link a new device</h3>
              <p className="settings-empty">
                Generate a one-time code, then enter it on the device you want to sign in.
                The new device will get its own session — and if you have a Recovery PIN,
                it can also restore your encryption keys so old messages are readable.
              </p>
              {linkMsg && <p className="settings-msg ok">{linkMsg}</p>}
              {linkCode ? (
                <>
                  <code className="settings-totp-code-block" style={{ fontSize: '1.4rem', letterSpacing: '0.1em', textAlign: 'center' }}>{linkCode}</code>
                  <p className="settings-empty">
                    {linkPollBusy ? 'Waiting for the new device to enter this code…' : 'Code expires in 5 minutes.'}
                  </p>
                </>
              ) : (
                <button className="settings-btn" onClick={generateLinkCode}>Generate link code</button>
              )}

              <hr className="settings-divider" />

              <h3 className="settings-section-title">QR login code</h3>
              <p className="settings-empty">
                Generate a one-time QR login code. Scan it with a signed-in device, enter the token on the new device's "Sign in with link code" screen, or use a QR scanner pointing to the <code>phaze://login?token=…</code> URL.
              </p>
              {qrMsg && <p className="settings-msg ok">{qrMsg}</p>}
              {qrToken ? (
                <>
                  <div style={{ textAlign: 'center', margin: '16px 0' }}>
                    {qrLoginQrCodeDataUrl ? (
                      <img
                        src={qrLoginQrCodeDataUrl}
                        alt="QR Login Code"
                        width={200}
                        height={200}
                        style={{ borderRadius: 8, border: '2px solid #232328', background: '#fff' }}
                      />
                    ) : (
                      <div style={{ width: 200, height: 200, display: 'flex', alignItems: 'center', justifyContent: 'center', margin: '0 auto', background: '#232328', borderRadius: 8 }}>Generating QR code...</div>
                    )}
                  </div>
                  <code className="settings-totp-code-block" style={{ fontSize: '1.1rem', letterSpacing: '0.05em', textAlign: 'center', wordBreak: 'break-all' }}>{qrToken}</code>
                  <p className="settings-empty" style={{ fontSize: '0.75rem' }}>Deep link: <code>phaze://login?token={qrToken}</code></p>
                  <button className="settings-btn-secondary" onClick={() => { setQrToken(''); setQrMsg('') }}>Clear</button>
                </>
              ) : (
                <button className="settings-btn" onClick={() => {
                  setQrMsg('')
                  setQrToken('')
                  send({ type: 'qr_login_create', sender: me })
                }}>Show QR login code</button>
              )}

              <hr className="settings-divider" />

              <h3 className="settings-section-title">Approve another device</h3>
              <p className="settings-empty">
                Enter the Link Code or QR token displayed on the other device to authorize it to sign in.
              </p>
              <div className="invite-link-box">
                <input
                  className="settings-input"
                  placeholder="Enter Link Code or QR Token"
                  value={approveCodeInput}
                  onChange={(e) => setApproveCodeInput(e.target.value)}
                />
                <button
                  className="settings-btn"
                  disabled={!approveCodeInput.trim() || approveBusy}
                  onClick={() => {
                    const token = approveCodeInput.trim()
                    let tok = token
                    if (tok.includes('token=')) {
                      tok = tok.split('token=')[1].split('&')[0]
                    }
                    setApproveBusy(true)
                    setApproveMsg('')
                    send({ type: 'link_approve', token: tok, device_info: `web/${window.location.hostname}` })
                  }}
                >
                  {approveBusy ? 'Approving...' : 'Approve Device'}
                </button>
              </div>
              {approveMsg && <p className={`settings-msg ${approveMsg.startsWith('✓') ? 'ok' : 'err'}`}>{approveMsg}</p>}
            </div>
          )}

          {/* ── Notifications ────────────────────────────────── */}
          {tab === 'notifications' && (
            <div className="settings-section">
              <h3 className="settings-section-title">Push notifications</h3>
              <p className="settings-empty">
                Get notified of new messages even when Phaze isn't open.
                Requires your browser to grant notification permission.
              </p>
              {pushMsg && <p className={`settings-msg ${pushMsg.startsWith('✓') ? 'ok' : 'err'}`}>{pushMsg}</p>}
              <div className="settings-row">
                <label className="settings-label" style={{ marginBottom: 0 }}>Enable push notifications</label>
                <button
                  className={`settings-btn${pushEnabled ? '-secondary' : ''}`}
                  onClick={async () => {
                    setPushMsg('')
                    if (pushEnabled) {
                      setPushEnabled(false)
                      setPushMsg('Push notifications disabled.')
                      return
                    }
                    if (!('serviceWorker' in navigator) || !('PushManager' in window)) {
                      setPushMsg('Push notifications are not supported in this browser.')
                      return
                    }
                    try {
                      const perm = await Notification.requestPermission()
                      if (perm !== 'granted') { setPushMsg('Notification permission denied.'); return }
                      const reg = await navigator.serviceWorker.register('/web/sw.js', { scope: '/web/' })
                      const resp = await fetch('/api/v1/vapid-key')
                      if (!resp.ok) { setPushMsg('Server does not support push yet.'); return }
                      const { publicKey } = await resp.json() as { publicKey: string }
                      if (!publicKey) { setPushMsg('Server does not support push yet.'); return }
                      const sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: publicKey })
                      const json = sub.toJSON()
                      send({ type: 'subscribe_push', sender: me, body: JSON.stringify({ endpoint: json.endpoint, p256dh: json.keys?.p256dh, auth: json.keys?.auth }) })
                      setPushEnabled(true)
                      setPushMsg('✓ Push notifications enabled.')
                    } catch (e) {
                      setPushMsg((e as Error).message || 'Failed to enable push notifications')
                    }
                  }}
                >
                  {pushEnabled ? 'Disable' : 'Enable'}
                </button>
              </div>
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

              <hr className="settings-divider" />

              <h3 className="settings-section-title">Email privacy</h3>
              <p className="settings-label" style={{ marginBottom: '0.5rem' }}>
                Remove your email address from the account. <strong>Warning:</strong> without an email you cannot reset your password if you forget it.
              </p>
              {purgeEmailMsg && <p className={`settings-msg ${purgeEmailMsg.includes('removed') ? 'ok' : 'err'}`}>{purgeEmailMsg}</p>}
              <button
                className="settings-btn danger"
                onClick={() => {
                  if (!confirm('Remove your email? You will lose the ability to reset your password.')) return
                  setPurgeEmailMsg('')
                  send({ type: 'purge_email', sender: me })
                }}
              >Remove email from account</button>
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
              <h3 className="settings-section-title">Sign out</h3>
              <p className="settings-label" style={{ marginBottom: '0.5rem' }}>Sign out of Phaze on this device. Your encryption keys and chat history will stay in this browser.</p>
              <button className="settings-btn" onClick={onSignOut}>Sign out</button>

              <hr className="settings-divider" />

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
