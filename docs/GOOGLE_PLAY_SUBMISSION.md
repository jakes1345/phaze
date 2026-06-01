# Google Play Submission — Phaze

Everything you need to paste into Play Console. Answers below match the live
Privacy Policy at https://phazechat.world/privacy — keep them consistent or
Google flags mismatches.

- **Package name:** `world.phazechat.app`
- **Privacy policy URL:** `https://phazechat.world/privacy`
- **Terms URL:** `https://phazechat.world/terms`
- **Support email:** `privacy@phazechat.world` (or your support address)
- **Website:** `https://phazechat.world`
- **App category:** Communication
- **Ads:** No ads
- **In-app purchases:** None (donations are off-platform via Buy Me a Coffee, not Google Play billing)

---

## 1. Store listing

**App name (30 chars max):**
```
Phaze — Encrypted Chat
```

**Short description (80 chars max):**
```
Private, end-to-end encrypted messaging. No ads, no tracking, no nonsense.
```

**Full description (4000 chars max):**
```
Phaze is a private messenger built for people who are done being the product.

Your direct messages and calls are end-to-end encrypted, so no one — not even
us — can read them. We don't show ads, we don't sell your data, and we don't
run third-party trackers. Just fast, clean, encrypted conversations.

WHAT YOU CAN DO
• Encrypted direct messages — secured with NaCl (Curve25519) before they ever
  leave your phone.
• Voice & video calls — peer-to-peer over WebRTC, never recorded.
• Spaces & channels — group conversations organized into topic channels, like
  a private community for your friends.
• Stories — share photos and videos that disappear after 24 hours.
• Files & voice messages — send attachments and record voice notes.
• Search & add friends — find people by username and connect instantly.
• Typing indicators, message reactions, edit & delete.
• Block & report — full control over who can reach you.

PRIVACY FIRST
• End-to-end encryption on direct messages by default.
• No ads. No advertising IDs. No browsing history.
• No selling your data. Ever.
• No data used to train AI models.
• Your private keys are generated on your device and never leave it.
• Message metadata is purged on a short schedule; files auto-delete after 7
  days; stories after 24 hours.

FREE FOR EVERYONE
Phaze is free and stays free. Every feature is available to every person —
there's no premium tier that locks talking to your friends behind a paywall.
If you love it, you can chip in as a supporter to keep the lights on, and get
a supporter badge — but that's optional and unlocks no core features.

MEET KAI
Kai is a friendly built-in assistant you can chat with for help getting
around Phaze or just to talk.

Phaze runs on Android, Windows, Linux, and the web at phazechat.world.

Not affiliated with Microsoft or Skype.

Questions? privacy@phazechat.world
```

**Graphics you still need to upload** (Play requires these):
- App icon: 512×512 PNG (you have the purple "P" — export at 512).
- Feature graphic: 1024×500 PNG/JPG.
- Phone screenshots: at least 2 (up to 8), 16:9 or 9:16, min 320px. Use the
  emulator: Chats list, a DM thread, Spaces/channels, a call screen, Settings.
- (Optional) 7-inch / 10-inch tablet screenshots.

---

## 2. Data safety form

Answer the wizard with these. (Data IS encrypted in transit, and you DO provide
a way to request deletion — both "Yes".)

**Does your app collect or share any of the required user data types?** → Yes
**Is all user data encrypted in transit?** → Yes
**Do you provide a way for users to request that their data be deleted?** → Yes
  (Deletion is via email request to privacy@phazechat.world. There is currently
  NO in-app "delete account" button on Android — the Settings "danger" button is
  just Sign Out. Email-based deletion is accepted by Google, but adding an in-app
  delete-account flow later is stronger. Do NOT claim in-app deletion.)

### Data types — collected (not shared with third parties for their own use):

| Data type | Collected | Purpose | Optional? |
|---|---|---|---|
| Email address | Yes | Account management, account recovery | Required |
| User IDs (username) | Yes | Account management, app functionality | Required |
| Name (display name) | Yes | App functionality (personalization) | Optional |
| Messages (in-app, other) | Yes | App functionality (relay/delivery) | Required |
| Photos/Videos (stories, attachments) | Yes | App functionality | Optional |
| Voice/audio (voice messages) | Yes | App functionality | Optional |
| App activity / Other actions | Yes | App functionality | Required |
| Device or other IDs (FCM push token) | Yes | App functionality (push notifications) | Required |
| Crash logs | Yes | Diagnostics (Sentry is live in prod) | Required |
| Diagnostics (performance) | Yes | Diagnostics (Sentry is live in prod) | Required |

**Phone number:** only if you ship the optional Twilio SMS verification —
then mark Phone number = Collected, purpose Account management, Optional.

### Important "no" answers (matches policy section 4):
- **Location** → Not collected.
- **Financial info** → Not collected (donations are off-platform).
- **Contacts** → Not collected (no address-book access).
- **Data shared for advertising/marketing** → No.
- **Data sold** → No.
- **Data used to track users across apps** → No.

### Encryption / sharing nuance for messages:
- Direct messages are end-to-end encrypted; the server only stores ciphertext
  briefly for delivery. In the form, "Messages" is still "collected" (it transits
  your server) — note E2EE in your data-safety details text.

> NOTE on diagnostics: Sentry IS active in production (SENTRY_DSN is set on Fly),
> so crash logs + diagnostics are collected and processed by Sentry as a service
> provider. Declared above. If you ever remove Sentry, update this.

---

## 3. Content rating questionnaire

Category: **Social / Communication app**. Honest answers for Phaze:

- Violence, blood, sexual content, nudity, profanity (in app's own content) → **No**
- Controlled substances / gambling → **No**
- **Users can interact / communicate with each other** → **Yes**
- **Users can share their location with each other** → **No** (no location feature)
- **User-generated content is shared** → **Yes** (messages, stories, files)
- **Does the app include unmoderated user-generated content?** → **Yes**, and confirm
  you provide **block + report** tools and a way to act on reports (you do).
- Digital purchases → **No**

Likely outcome: **Teen / PEGI 12-ish** (because of open user communication).
This is normal for messengers.

---

## 4. Target audience & content

- **Target age groups:** 18+ (or 13+ if you want — but 18+ avoids the stricter
  Families/child-safety requirements and matches your policy's under-13 exclusion).
  Recommended: **18 and over** for simplest compliance.
- **Appeal to children:** No.
- **Store listing aimed at children:** No.

---

## 5. App access (already done ✅)

Restricted access → reviewer login:
- Username: `PlayReview`
- Password: `PhazeReview2026`
- Instructions: Sign in with the credentials on the Sign In screen. No email
  verification, 2FA, or secondary device required. Chats tab shows a
  conversation with "Kai" (an in-app assistant that replies); Spaces tab shows
  "Phaze Hub" with text channels to open and post in.

---

## 6. Other declarations

- **Ads:** This app does not contain ads.
- **News app:** No.
- **COVID-19 contact tracing/status:** No.
- **Government app:** No.
- **Financial features:** No.
- **Data deletion (account):** Users request deletion via
  privacy@phazechat.world (email-based; accepted by Google). There is no in-app
  delete-account button yet — don't claim one.

---

## 7. Pre-launch checklist (do these before "Apply for production")

- [ ] Upload AAB v14 (1.2.2) to closed testing, roll out
- [ ] 12+ testers opted in and staying opted in for 14 continuous days
- [ ] Store listing text (above) + graphics uploaded
- [ ] Data safety form completed
- [ ] Content rating questionnaire completed
- [ ] Target audience set
- [ ] App access reviewer creds entered ✅
- [ ] Privacy policy URL set ✅ (https://phazechat.world/privacy)
- [ ] Ads declaration: No ads
- [ ] App category: Communication
