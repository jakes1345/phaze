import { useEffect, useState } from 'react'
import type { NexusMessage } from './nexusTypes'

interface Props {
  username: string
  me: string
  /** Map of friend username -> presence (used to detect "already a friend"). */
  friends: Record<string, string>
  /** Outbound friend requests this session knows about. */
  pendingOut?: string[]
  send: (m: NexusMessage) => void
  onClose: () => void
  /** Switches the app view to DMs and opens the chat with this user. */
  onStartDM: (username: string) => void
}

interface Profile {
  username: string
  display_name?: string
  mood?: string
}

// UserProfile is the click-a-user modal: shows the target's profile and
// gives one-tap Start DM / Add friend / Block buttons. Reuses the existing
// /api/v1/profile/<name> HTTP endpoint and the friend_request WS message.
export default function UserProfile({ username, me, friends, pendingOut = [], send, onClose, onStartDM }: Props) {
  const [profile, setProfile] = useState<Profile | null>(null)
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  const isMe = username === me
  const isFriend = !!friends[username]
  const presence = friends[username] ?? null
  const isPending = pendingOut.includes(username)

  useEffect(() => {
    let cancelled = false
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setLoading(true)
    fetch(`/api/v1/profile/${encodeURIComponent(username)}`)
      .then((r) => {
        if (r.status === 404) return Promise.reject(new Error('User not found'))
        if (!r.ok) return Promise.reject(new Error(`HTTP ${r.status}`))
        return r.json()
      })
      .then((data: Profile) => { if (!cancelled) setProfile(data) })
      .catch((e: Error) => { if (!cancelled) setErr(e.message || 'Could not load profile') })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [username])

  const handleAddFriend = () => {
    send({ type: 'friend_request', sender: me, recipient: username })
    // Optimistic UX: caller can pass a fresh pendingOut next render.
  }

  const handleBlock = () => {
    send({ type: 'block_user', sender: me, recipient: username })
    onClose()
  }

  const handleStartDM = () => {
    onStartDM(username)
    onClose()
  }

  return (
    <div className="user-profile-modal" onClick={onClose} role="presentation">
      <div className="user-profile-card" onClick={(e) => e.stopPropagation()}>
        <button type="button" className="user-profile-close" onClick={onClose} aria-label="Close">×</button>

        <div className="user-profile-avatar" aria-hidden>
          <img
            src={`/api/v1/avatars/${encodeURIComponent(username)}`}
            alt=""
            onError={(e) => { (e.currentTarget as HTMLImageElement).style.display = 'none' }}
          />
          <span className="user-profile-initial">{username[0]?.toUpperCase() ?? '?'}</span>
        </div>

        <h3 className="user-profile-name">
          {profile?.display_name || username}
          {presence && (
            <span className={`user-profile-status ${presence === 'Online' ? 'on' : 'off'}`}>{presence}</span>
          )}
        </h3>
        <p className="user-profile-handle">@{username}</p>
        {profile?.mood && <p className="user-profile-mood">"{profile.mood}"</p>}

        {loading && <p className="muted small">Loading…</p>}
        {err && <p className="user-profile-err">{err}</p>}

        {!isMe && !err && (
          <div className="user-profile-actions">
            <button type="button" className="user-profile-btn primary" onClick={handleStartDM}>
              📨 Message
            </button>
            {!isFriend && !isPending && (
              <button type="button" className="user-profile-btn" onClick={handleAddFriend}>
                ➕ Add friend
              </button>
            )}
            {isPending && (
              <button type="button" className="user-profile-btn" disabled>
                ⏳ Request sent
              </button>
            )}
            <button type="button" className="user-profile-btn danger" onClick={handleBlock}>
              🚫 Block
            </button>
          </div>
        )}
      </div>
    </div>
  )
}
