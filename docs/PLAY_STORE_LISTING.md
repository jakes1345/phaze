# Google Play Store — Listing Copy & Data Safety Form

Complete content ready to paste into the Play Console.

---

## App Details

| Field | Value |
|-------|-------|
| **App name** | Phaze — Encrypted Chat |
| **Developer name** | Phaze Project |
| **Default language** | English (United States) |
| **App category** | Communication |
| **Content rating** | Everyone |
| **Privacy policy URL** | `https://phazechat.world/privacy` |

---

## Short Description (80 chars max)

```
Encrypted, private messaging and voice calls — your keys, your conversations.
```
(79 chars)

---

## Full Description (4000 chars max)

```
Phaze is an end-to-end encrypted messenger built for privacy-first communication.
Your messages are encrypted on your device using NaCl (libsodium) box encryption 
before they ever leave your phone — even we can't read them.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔐 TRUE END-TO-END ENCRYPTION
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
• NaCl public-key encryption for all direct messages
• Cryptographic key fingerprints you can verify with contacts
• Optional encrypted key backup protected by a PIN only you know
• No ads. No tracking. No data mining.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
💬 EVERYTHING YOU NEED
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
• 1-on-1 encrypted chat with file and voice message support
• Voice and video calls over WebRTC
• Spaces — group channels for teams, communities, and friends
• Stories — share photos and short videos with your contacts
• QR code device linking — sign in on a new device without a password

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📱 BUILT FOR ANDROID
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
• Clean, modern Material You design
• Push notifications via Firebase so you never miss a message
• Works great on Android 8.0 and above
• Also available on Windows, macOS, and Linux

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🌐 OPEN & INDEPENDENT
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
• Open-source on GitHub
• Self-hostable — run your own Phaze relay server
• No vendor lock-in; your data stays under your control
• Not affiliated with Microsoft or Skype

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⚠️ BETA NOTICE
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Phaze is currently in beta. Core features are fully functional. We welcome 
feedback via GitHub Issues. Some features (Opus audio, federation, 
auto-update) are on our roadmap but not yet shipped.
```

---

## Release Notes — v1.1.0 (500 chars max)

```
First Google Play release 🎉

• End-to-end encrypted 1-on-1 messaging and voice calls
• Spaces (group channels), Stories, file & voice message support
• QR code device linking and encrypted key backup
• Firebase push notifications
• NaCl E2EE — your messages are encrypted before leaving your device

Feedback welcome at github.com/jakes1345/skype7-reborn
```

---

## Screenshots Needed (phone, min 1080px wide, 2–8 required)

Capture these flows on a real device or emulator:

1. **Auth screen** — the clean login/register screen
2. **Chats list** — showing contacts with status indicators and story rings
3. **Chat screen** — a conversation with message bubbles
4. **Voice call screen** — the in-call UI
5. **Spaces screen** — a group channel
6. **Settings screen** — showing the device linking / key backup section

---

## Data Safety Form (Play Console → App Content → Data Safety)

### Does your app collect or share any of the required user data types?
**Yes**

### Data Types Collected

| Type | Collected | Shared | Optional? | Purpose |
|------|-----------|--------|-----------|---------|
| **Name** (username) | ✅ | ❌ | No | App functionality |
| **Email address** | ✅ | ❌ | No | Account management |
| **Messages** | ✅ encrypted | ❌ | No | App functionality |
| **Photos & Videos** | ✅ (if shared) | ❌ | Yes | App functionality |
| **Audio files** | ✅ (voice msgs) | ❌ | Yes | App functionality |
| **User IDs** | ✅ | ❌ | No | App functionality |
| **Device IDs** (FCM token) | ✅ | Google (FCM only) | No | Notifications |
| **App interactions** | ✅ | ❌ | No | Security / fraud prevention |

### Is the data encrypted in transit?
**Yes** — TLS 1.2+ for all server connections + NaCl E2EE for messages.

### Can users request data deletion?
**Yes** — via email to privacy@phazechat.world (describe in-app flow once delete account is implemented).

### Does your app use the Advertising ID?
**No**

---

## Content Rating Questionnaire (IARC)

Answers for the Google Play IARC questionnaire:

- **Violence**: No
- **Sexual content**: No
- **Language**: No profanity in the app itself (user-generated content possible)
- **Controlled substances**: No
- **User-generated content**: Yes — messaging app; users generate their own content
- **Hate speech**: No built-in content
- **Gambling**: No

**Expected rating: Everyone** (due to UGC, may note that content moderation is user-controlled on self-hosted instances)

---

## Checklist Before Submitting

- [ ] Privacy policy live at `https://phazechat.world/privacy`
- [ ] AAB uploaded to Internal Testing track
- [ ] All 6+ screenshots uploaded (phone)
- [ ] Feature graphic uploaded (1024×500)
- [ ] Hi-res icon uploaded (512×512 PNG)
- [ ] Data safety form completed
- [ ] Content rating questionnaire completed  
- [ ] Short + full descriptions entered
- [ ] Release notes entered for v1.1.0
- [ ] Play App Signing opted in (required for new apps)
- [ ] Test on internal track before promoting to production
