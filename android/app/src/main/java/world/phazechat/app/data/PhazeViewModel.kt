package world.phazechat.app.data

import android.app.Application
import android.content.Context
import android.os.Build
import android.util.Log
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.*
import org.json.JSONArray
import org.json.JSONObject
import world.phazechat.app.crypto.*

data class ChatLine(
    val id: String,
    val from: String,
    val text: String,
    val me: Boolean,
    val ts: Long = System.currentTimeMillis(),
    val kind: String? = null,
    val fileUrl: String? = null,
    val fileName: String? = null,
    val edited: Boolean = false,
    val deleted: Boolean = false,
    val reaction: String? = null,
    val seen: Boolean = false,
)

data class FriendInfo(
    val username: String,
    val status: String = "Offline",
    val mood: String? = null,
    val supporter: Boolean = false,
)

data class SpaceInfo(
    val id: String,
    val name: String,
    val description: String? = null,
    val owner: String = "",
    val visibility: String = "private",
    val role: String = "member",
    val memberCount: Int = 0,
    val isMember: Boolean = false,
)

data class ChannelInfo(
    val id: String,
    val serverId: String,
    val name: String,
    val topic: String? = null,
    val kind: String = "text",
)

data class ChannelMsg(
    val id: Long,
    val sender: String,
    val body: String,
    val createdAt: String,
    val edited: Boolean = false,
    val deleted: Boolean = false,
)

// GlobalNotice is an admin broadcast shown as a popup on every connected client.
data class GlobalNotice(val from: String, val message: String)

class PhazeViewModel(app: Application) : AndroidViewModel(app) {

    companion object {
        private const val TAG = "PhazeVM"
        private const val PREFS = "phaze_prefs"
    }

    private val prefs = app.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
    val nexus = NexusClient(viewModelScope)

    // Auth
    private val _me = MutableStateFlow<String?>(null)
    val me = _me.asStateFlow()
    private val _sessionToken = MutableStateFlow<String?>(prefs.getString("session_token", null))
    val sessionToken = _sessionToken.asStateFlow()
    private val _authError = MutableStateFlow<String?>(null)
    val authError = _authError.asStateFlow()

    // Profile
    private val _myDisplayName = MutableStateFlow(prefs.getString("display_name", "") ?: "")
    val myDisplayName = _myDisplayName.asStateFlow()

    // Device Linking State
    private val _activeLinkCode = MutableStateFlow<String?>(null)
    val activeLinkCode = _activeLinkCode.asStateFlow()
    private val _linkStatus = MutableStateFlow<String?>(null)
    val linkStatus = _linkStatus.asStateFlow()
    private val _linkError = MutableStateFlow<String?>(null)
    val linkError = _linkError.asStateFlow()

    // Key Backup State
    private val _keyBackupStatus = MutableStateFlow<String?>(null)
    val keyBackupStatus = _keyBackupStatus.asStateFlow()
    private val _keyBackupError = MutableStateFlow<String?>(null)
    val keyBackupError = _keyBackupError.asStateFlow()

    // 2FA / TOTP enrollment. _twoFactorUri holds the otpauth:// URI while a
    // confirmation code is pending; _twoFactorStatus surfaces result messages.
    private val _twoFactorUri = MutableStateFlow<String?>(null)
    val twoFactorUri = _twoFactorUri.asStateFlow()
    private val _twoFactorStatus = MutableStateFlow<String?>(null)
    val twoFactorStatus = _twoFactorStatus.asStateFlow()
    private val _twoFactorEnabled = MutableStateFlow(false)
    val twoFactorEnabled = _twoFactorEnabled.asStateFlow()
    private val _twoFactorBackupCodes = MutableStateFlow<List<String>?>(null)
    val twoFactorBackupCodes = _twoFactorBackupCodes.asStateFlow()
    fun clearBackupCodes() { _twoFactorBackupCodes.value = null }

    // Friends
    private val _friends = MutableStateFlow<Map<String, FriendInfo>>(emptyMap())
    val friends = _friends.asStateFlow()
    private val _pending = MutableStateFlow<List<String>>(emptyList())
    val pending = _pending.asStateFlow()

    // Deep-link: open a specific chat (e.g. from a push notification tap)
    private val _pendingOpenChat = MutableStateFlow<String?>(null)
    val pendingOpenChat = _pendingOpenChat.asStateFlow()
    fun consumePendingOpenChat(): String? {
        val peer = _pendingOpenChat.value
        _pendingOpenChat.value = null
        return peer
    }

    // Chat
    private val _selectedChat = MutableStateFlow<String?>(null)
    val selectedChat = _selectedChat.asStateFlow()
    private val _chatLog = MutableStateFlow<List<ChatLine>>(emptyList())
    val chatLog = _chatLog.asStateFlow()
    private val _unread = MutableStateFlow<Map<String, Int>>(emptyMap())
    val unread = _unread.asStateFlow()
    // Who is currently typing to me (the peer's username), auto-cleared.
    private val _typingFrom = MutableStateFlow<String?>(null)
    val typingFrom = _typingFrom.asStateFlow()
    private var typingClearJob: kotlinx.coroutines.Job? = null
    private var lastTypingSentAt = 0L

    // User search
    private val _searchResults = MutableStateFlow<List<String>>(emptyList())
    val searchResults = _searchResults.asStateFlow()

    // Transient status for block/report/channel actions (shown as a snackbar/toast).
    private val _actionStatus = MutableStateFlow<String?>(null)
    val actionStatus = _actionStatus.asStateFlow()
    fun clearActionStatus() { _actionStatus.value = null }

    // Admin global notice popup — broadcast from the admin portal to every client.
    private val _globalNotice = MutableStateFlow<GlobalNotice?>(null)
    val globalNotice = _globalNotice.asStateFlow()
    fun clearGlobalNotice() { _globalNotice.value = null }

    // Spaces
    private val _spaces = MutableStateFlow<List<SpaceInfo>>(emptyList())
    val spaces = _spaces.asStateFlow()
    private val _discoverSpaces = MutableStateFlow<List<SpaceInfo>>(emptyList())
    val discoverSpaces = _discoverSpaces.asStateFlow()

    // Theme pack: "dark" (default), "light", or "skype7". Persisted locally.
    private val _theme = MutableStateFlow(prefs.getString("theme", "dark") ?: "dark")
    val theme = _theme.asStateFlow()
    fun setTheme(t: String) {
        _theme.value = t
        prefs.edit().putString("theme", t).apply()
    }
    // Snowflakes seasonal overlay.
    private val _snow = MutableStateFlow(prefs.getBoolean("snow", false))
    val snow = _snow.asStateFlow()
    fun setSnow(on: Boolean) {
        _snow.value = on
        prefs.edit().putBoolean("snow", on).apply()
    }
    private val _activeSpace = MutableStateFlow<String?>(null)
    val activeSpace = _activeSpace.asStateFlow()
    private val _channels = MutableStateFlow<Map<String, List<ChannelInfo>>>(emptyMap())
    val channels = _channels.asStateFlow()
    private val _activeChannel = MutableStateFlow<String?>(null)
    val activeChannel = _activeChannel.asStateFlow()
    private val _channelMessages = MutableStateFlow<List<ChannelMsg>>(emptyList())
    val channelMessages = _channelMessages.asStateFlow()
    private val _channelDraft = MutableStateFlow("")
    val channelDraft = _channelDraft.asStateFlow()

    // Calls
    data class CallState(
        val peer: String,
        val status: String = "ringing", // ringing, connecting, connected, ended
        val isIncoming: Boolean = false,
        val isMuted: Boolean = false,
        val isCameraOn: Boolean = false,
        val isVideo: Boolean = false,
        val isScreenSharing: Boolean = false,
        val hasRemoteVideo: Boolean = false,
        val incomingSdp: String? = null,
    )
    private val _callState = MutableStateFlow<CallState?>(null)
    val callState = _callState.asStateFlow()
    var callManager: CallManager? = null; private set

    // Stories
    private val _stories = MutableStateFlow<List<world.phazechat.app.ui.Story>>(emptyList())
    val stories = _stories.asStateFlow()

    // Crypto
    private var keyPair: NaClKeyPair
    private val peerKeys = mutableMapOf<String, ByteArray>()

    // TURN
    var turnUrl: String? = null; private set
    var turnUsername: String? = null; private set
    var turnPassword: String? = null; private set

    init {
        keyPair = loadOrCreateKeys()
        observeMessages()
        observeConnection()   // (re)authenticate on every connect — survives socket drops
        nexus.connect()
    }

    private fun loadOrCreateKeys(): NaClKeyPair {
        val pub = prefs.getString("nacl_pub", null)
        val sec = prefs.getString("nacl_sec", null)
        if (pub != null && sec != null) {
            val pubBytes = decodePublicKeyB64(pub)
            val secBytes = decodePublicKeyB64(sec)
            if (pubBytes != null && secBytes != null) return NaClKeyPair(pubBytes, secBytes)
        }
        val kp = generateKeyPair()
        prefs.edit()
            .putString("nacl_pub", encodePublicKeyB64(kp.publicKey))
            .putString("nacl_sec", encodePublicKeyB64(kp.secretKey))
            .apply()
        return kp
    }

    // Re-authenticate on EVERY transition to CONNECTED, not just the first.
    // OkHttp transparently reconnects after a dropped socket (idle timeout,
    // network blip, app resume); without re-auth that new socket is
    // unauthenticated and the server silently drops every action (search,
    // DMs, messages to the Kai bot), making the app appear frozen until a
    // manual restart. Collecting the state flow re-sends session_auth each
    // time we reconnect, which the auth_result handler uses to reload state.
    private fun observeConnection() {
        viewModelScope.launch {
            nexus.state.collect { st ->
                if (st == ConnState.CONNECTED) {
                    val tok = _sessionToken.value ?: return@collect
                    nexus.send(NexusMessage(type = "session_auth", qrToken = tok, deviceInfo = "android/${Build.MODEL}"))
                }
            }
        }
    }

    private var linkPollJob: Job? = null

    fun login(username: String, password: String) {
        _authError.value = null
        nexus.send(NexusMessage(type = "auth", sender = username, body = password, deviceInfo = "android/${Build.MODEL}"))
    }

    fun loginWithLinkCode(code: String) {
        linkPollJob?.cancel()
        _authError.value = "Waiting for approval on another device..."
        var tok = code.trim()
        if (tok.contains("token=")) {
            tok = tok.substringAfter("token=").substringBefore("&")
        }
        val finalToken = tok
        linkPollJob = viewModelScope.launch {
            nexus.state.first { it == ConnState.CONNECTED }
            var attempts = 0
            while (me.value == null && _authError.value?.contains("Waiting") == true && attempts++ < 120) {
                nexus.send(NexusMessage(type = "link_check", token = finalToken))
                delay(2500)
            }
            if (me.value == null && _authError.value?.contains("Waiting") == true) {
                _authError.value = "Link code expired. Generate a new one."
            }
        }
    }

    fun cancelLinkLogin() {
        linkPollJob?.cancel()
        linkPollJob = null
        _authError.value = null
    }

    fun generateLinkCode() {
        _linkError.value = null
        _linkStatus.value = "Generating Link Code..."
        nexus.send(NexusMessage(type = "link_create", sender = _me.value))
    }

    fun approveDevice(code: String) {
        _linkError.value = null
        _linkStatus.value = "Approving..."
        var tok = code.trim()
        if (tok.contains("token=")) {
            tok = tok.substringAfter("token=").substringBefore("&")
        }
        val device = "android/${Build.MODEL}"
        nexus.send(NexusMessage(type = "link_approve", token = tok, deviceInfo = device, sender = _me.value))
    }

    fun clearLinkStatus() {
        _linkStatus.value = null
        _linkError.value = null
        _activeLinkCode.value = null
    }

    fun clearKeyBackupStatus() {
        _keyBackupStatus.value = null
        _keyBackupError.value = null
    }

    /** Encrypt this device's NaCl keypair with [pin] and upload to server. */
    fun backupKeys(pin: String) {
        if (pin.length < 4) {
            _keyBackupError.value = "PIN must be at least 4 characters"
            return
        }
        _keyBackupError.value = null
        _keyBackupStatus.value = "Encrypting & uploading backup…"
        viewModelScope.launch(kotlinx.coroutines.Dispatchers.Default) {
            try {
                val blob = world.phazechat.app.crypto.KeyBackup.encryptKeypair(
                    keyPair.publicKey, keyPair.secretKey, pin
                )
                val payload = org.json.JSONObject().apply {
                    put("ciphertext", blob.ciphertext)
                    put("salt", blob.salt)
                    put("iterations", blob.iterations)
                }.toString()
                nexus.send(NexusMessage(
                    type = "key_backup_put",
                    sender = _me.value,
                    token = payload,
                ))
            } catch (e: Exception) {
                _keyBackupStatus.value = null
                _keyBackupError.value = "Backup failed: ${e.message}"
            }
        }
    }

    /** Fetch the encrypted keypair backup from server and decrypt with [pin]. */
    fun restoreKeys(pin: String) {
        if (pin.length < 4) {
            _keyBackupError.value = "PIN must be at least 4 characters"
            return
        }
        _keyBackupError.value = null
        _keyBackupStatus.value = "Fetching backup from server…"
        // Store pin for use when the async result comes back
        pendingRestorePin = pin
        nexus.send(NexusMessage(type = "key_backup_get", sender = _me.value))
    }

    private var pendingRestorePin: String? = null

    fun register(username: String, email: String, password: String) {
        _authError.value = null
        nexus.send(NexusMessage(type = "register", sender = username, body = password, email = email))
    }

    fun selectChat(peer: String) {
        if (peer.isBlank()) { _selectedChat.value = null; return }
        _selectedChat.value = peer
        _unread.value = _unread.value.toMutableMap().apply { remove(peer) }
        _chatLog.value = emptyList()
        _me.value?.let {
            nexus.send(NexusMessage(type = "dm_history", sender = it, recipient = peer))
            nexus.send(NexusMessage(type = "read_receipt", sender = it, recipient = peer, body = peer))
        }
    }

    fun sendMessage(text: String) {
        val peer = _selectedChat.value ?: return
        val username = _me.value ?: return
        val peerPub = peerKeys[peer]
        val body = if (peerPub != null) encryptForPeer(text, peerPub, keyPair.secretKey) else text
        val msgId = "${username}-${System.nanoTime()}"
        nexus.send(NexusMessage(type = "msg", sender = username, recipient = peer, body = body, msgId = msgId))
        appendChat(ChatLine(id = msgId, from = username, text = text, me = true, ts = System.currentTimeMillis()))
    }

    // ── User search ──────────────────────────────────────────────
    fun searchUsers(query: String) {
        val q = query.trim()
        if (q.isBlank()) { _searchResults.value = emptyList(); return }
        nexus.send(NexusMessage(type = "search", body = q))
    }
    fun clearSearch() { _searchResults.value = emptyList() }

    // ── Block / Report (Google Play requires these for social apps) ──
    fun blockUser(user: String) {
        nexus.send(NexusMessage(type = "block", recipient = user))
    }
    fun unblockUser(user: String) {
        nexus.send(NexusMessage(type = "unblock", recipient = user))
    }
    fun reportUser(subject: String, reason: String, detail: String) {
        nexus.send(NexusMessage(type = "report_abuse", recipient = subject, status = reason, body = detail))
        _actionStatus.value = "Report submitted. Thanks for keeping Phaze safe."
    }

    // ── Channels ─────────────────────────────────────────────────
    fun createChannel(serverId: String, name: String, kind: String = "text") {
        val n = name.trim().lowercase()
        if (n.isBlank()) return
        nexus.send(NexusMessage(type = "channel_create", serverId = serverId, channelName = n, kind = kind))
    }

    // ── Message edit / delete / react (DMs) ──────────────────────
    fun editMessage(msgId: String, newText: String) {
        val peer = _selectedChat.value ?: return
        val peerPub = peerKeys[peer]
        val body = if (peerPub != null) encryptForPeer(newText, peerPub, keyPair.secretKey) else newText
        nexus.send(NexusMessage(type = "msg_edit", recipient = peer, msgId = msgId, body = body))
        _chatLog.value = _chatLog.value.map { if (it.id == msgId) it.copy(text = newText, edited = true) else it }
    }
    fun deleteMessage(msgId: String) {
        val peer = _selectedChat.value ?: return
        nexus.send(NexusMessage(type = "msg_delete", recipient = peer, msgId = msgId))
        _chatLog.value = _chatLog.value.map { if (it.id == msgId) it.copy(text = "", deleted = true) else it }
    }
    fun reactMessage(msgId: String, emoji: String) {
        val peer = _selectedChat.value ?: return
        nexus.send(NexusMessage(type = "msg_react", recipient = peer, msgId = msgId, reaction = emoji))
        _chatLog.value = _chatLog.value.map {
            if (it.id == msgId) it.copy(reaction = if (it.reaction == emoji) null else emoji) else it
        }
    }

    // ── Typing indicator (throttled to ~1/sec) ───────────────────
    fun sendTyping() {
        val peer = _selectedChat.value ?: return
        val now = System.currentTimeMillis()
        if (now - lastTypingSentAt < 1000) return
        lastTypingSentAt = now
        nexus.send(NexusMessage(type = "typing", recipient = peer))
    }

    // Remove a user from the local chat/friend lists (after blocking).
    private fun dropChat(user: String) {
        _friends.value = _friends.value.toMutableMap().apply { remove(user) }
        _unread.value = _unread.value.toMutableMap().apply { remove(user) }
        if (_selectedChat.value == user) { _selectedChat.value = null; _chatLog.value = emptyList() }
    }

    fun sendFile(uri: android.net.Uri) {
        val peer = _selectedChat.value ?: return
        val username = _me.value ?: return
        val token = _sessionToken.value ?: return
        viewModelScope.launch(kotlinx.coroutines.Dispatchers.IO) {
            try {
                val cr = getApplication<Application>().contentResolver
                // Resolve a human-readable filename from the content URI
                val fileName = run {
                    var name: String? = null
                    if (uri.scheme == "content") {
                        cr.query(uri, arrayOf(android.provider.OpenableColumns.DISPLAY_NAME), null, null, null)?.use { c ->
                            if (c.moveToFirst()) name = c.getString(0)
                        }
                    }
                    name ?: uri.lastPathSegment?.substringAfterLast('/') ?: "file"
                }
                val bytes = cr.openInputStream(uri)?.readBytes()
                if (bytes == null) {
                    viewModelScope.launch(kotlinx.coroutines.Dispatchers.Main) { _actionStatus.value = "Could not read file" }
                    return@launch
                }
                val boundary = "----PhazeUpload${System.currentTimeMillis()}"
                val body = buildMultipart(boundary, fileName, bytes)
                val url = java.net.URL("https://phazechat.world/api/v1/upload")
                val conn = url.openConnection() as java.net.HttpURLConnection
                conn.requestMethod = "POST"
                conn.setRequestProperty("Content-Type", "multipart/form-data; boundary=$boundary")
                conn.setRequestProperty("Authorization", "Bearer $token")
                conn.doOutput = true
                conn.outputStream.write(body)
                val code = conn.responseCode
                if (code == 200) {
                    val resp = conn.inputStream.bufferedReader().readText()
                    val j = JSONObject(resp)
                    val fileUrl = j.optString("url", "")
                    val mime = j.optString("mime", "application/octet-stream")
                    val size = j.optLong("size", bytes.size.toLong())
                    if (fileUrl.isNotEmpty()) {
                        val msgId = "${username}-${System.nanoTime()}"
                        val peerPub = peerKeys[peer]
                        val fileJson = JSONObject().apply {
                            put("url", fileUrl); put("name", fileName); put("mime", mime); put("size", size)
                        }
                        val plaintext = "phaze-file${fileJson}"
                        val encBody = if (peerPub != null) encryptForPeer(plaintext, peerPub, keyPair.secretKey) else plaintext
                        nexus.send(NexusMessage(type = "msg", sender = username, recipient = peer, body = encBody, msgId = msgId))
                        viewModelScope.launch(kotlinx.coroutines.Dispatchers.Main) {
                            appendChat(ChatLine(id = msgId, from = username, text = plaintext, me = true, kind = "file", fileUrl = fileUrl, fileName = fileName, ts = System.currentTimeMillis()))
                        }
                    }
                } else {
                    val err = runCatching { conn.errorStream?.bufferedReader()?.readText() }.getOrNull() ?: ""
                    Log.w(TAG, "sendFile HTTP $code: $err")
                    viewModelScope.launch(kotlinx.coroutines.Dispatchers.Main) { _actionStatus.value = "File upload failed ($code)" }
                }
                conn.disconnect()
            } catch (e: Exception) {
                Log.w(TAG, "sendFile: ${e.message}")
                viewModelScope.launch(kotlinx.coroutines.Dispatchers.Main) { _actionStatus.value = "File send error: ${e.message}" }
            }
        }
    }

    fun sendVoiceMessage(filePath: String) {
        val peer = _selectedChat.value ?: return
        val username = _me.value ?: return
        val token = _sessionToken.value ?: return
        viewModelScope.launch(kotlinx.coroutines.Dispatchers.IO) {
            try {
                val bytes = java.io.File(filePath).readBytes()
                val voiceFileName = java.io.File(filePath).name // .ogg (API 29+) or .m4a (API 26–28)
                val boundary = "----PhazeUpload${System.currentTimeMillis()}"
                val body = buildMultipart(boundary, voiceFileName, bytes)
                val url = java.net.URL("https://phazechat.world/api/v1/upload")
                val conn = url.openConnection() as java.net.HttpURLConnection
                conn.requestMethod = "POST"
                conn.setRequestProperty("Content-Type", "multipart/form-data; boundary=$boundary")
                conn.setRequestProperty("Authorization", "Bearer $token")
                conn.doOutput = true
                conn.outputStream.write(body)
                if (conn.responseCode == 200) {
                    val resp = conn.inputStream.bufferedReader().readText()
                    val j = JSONObject(resp)
                    val fileUrl = j.optString("url", "")
                    val mime = j.optString("mime", "audio/ogg")
                    val size = j.optLong("size", bytes.size.toLong())
                    if (fileUrl.isNotEmpty()) {
                        val msgId = "${username}-${System.nanoTime()}"
                        val peerPub = peerKeys[peer]
                        val fileJson = JSONObject().apply {
                            put("url", fileUrl); put("name", voiceFileName); put("mime", mime); put("size", size)
                        }
                        val plaintext = "phaze-file${fileJson}"
                        val body = if (peerPub != null) encryptForPeer(plaintext, peerPub, keyPair.secretKey) else plaintext
                        nexus.send(NexusMessage(type = "msg", sender = username, recipient = peer, body = body, msgId = msgId))
                        viewModelScope.launch(kotlinx.coroutines.Dispatchers.Main) {
                            appendChat(ChatLine(id = msgId, from = username, text = plaintext, me = true, kind = "voice", fileUrl = fileUrl, fileName = voiceFileName, ts = System.currentTimeMillis()))
                        }
                    }
                }
                conn.disconnect()
                java.io.File(filePath).delete()
            } catch (e: Exception) { Log.w(TAG, "sendVoice: ${e.message}") }
        }
    }

    private fun buildMultipart(boundary: String, fileName: String, data: ByteArray): ByteArray {
        val bos = java.io.ByteArrayOutputStream()
        val w = bos.writer()
        w.write("--$boundary\r\n")
        w.write("Content-Disposition: form-data; name=\"file\"; filename=\"$fileName\"\r\n")
        w.write("Content-Type: application/octet-stream\r\n\r\n")
        w.flush()
        bos.write(data)
        w.write("\r\n--$boundary--\r\n")
        w.flush()
        return bos.toByteArray()
    }

    fun sendFriendRequest(to: String) {
        nexus.send(NexusMessage(type = "friend_request", sender = _me.value, recipient = to))
    }

    fun acceptFriend(from: String) {
        nexus.send(NexusMessage(type = "friend_accept", recipient = from))
        _pending.value = _pending.value.filter { it != from }
        _friends.value = _friends.value.toMutableMap().apply { put(from, FriendInfo(from, "Online")) }
    }

    fun updateProfile(displayName: String, mood: String) {
        val me = _me.value ?: return
        _myDisplayName.value = displayName
        nexus.send(NexusMessage(type = "update_profile", sender = me, displayName = displayName, mood = mood))
        nexus.send(NexusMessage(type = "status_update", sender = me, body = mood, status = "Online"))
    }

    /** Step 1 of enrollment: ask the server for a TOTP secret/URI. */
    fun enable2FA() {
        _twoFactorStatus.value = "Generating 2FA secret…"
        nexus.send(NexusMessage(type = "enable_totp", sender = _me.value))
    }

    /** Step 2: confirm enrollment with a code from the authenticator app. */
    fun confirm2FA(code: String) {
        if (code.isBlank()) { _twoFactorStatus.value = "Enter the 6-digit code"; return }
        nexus.send(NexusMessage(type = "confirm_totp", sender = _me.value, totpCode = code.trim()))
    }

    fun cancel2FAEnrollment() {
        _twoFactorUri.value = null
        _twoFactorStatus.value = null
    }

    /** Disable 2FA — the server requires the account password. */
    fun disable2FA(password: String) {
        if (password.isBlank()) { _twoFactorStatus.value = "Password required to disable 2FA"; return }
        nexus.send(NexusMessage(type = "disable_totp", sender = _me.value, body = password))
    }

    fun clearTwoFactorStatus() { _twoFactorStatus.value = null }

    fun signOut() {
        prefs.edit().remove("session_token").remove("username").apply()
        _me.value = null
        _sessionToken.value = null
        _friends.value = emptyMap()
        _pending.value = emptyList()
        _chatLog.value = emptyList()
        _selectedChat.value = null
        _unread.value = emptyMap()
        _spaces.value = emptyList()
        peerKeys.clear()
        nexus.disconnect()
        nexus.connect()
    }

    // Permanently erase the account (GDPR right-to-erasure). The server
    // requires the current password as confirmation. On success the
    // delete_account_result handler signs the user out.
    fun deleteAccount(password: String) {
        if (password.isBlank()) { _actionStatus.value = "Enter your password to confirm"; return }
        nexus.send(NexusMessage(type = "delete_account", body = password))
    }

    // Spaces
    fun loadSpaces() { nexus.send(NexusMessage(type = "server_list")) }

    fun selectSpace(id: String) {
        if (id.isBlank()) { _activeSpace.value = null; _activeChannel.value = null; return }
        _activeSpace.value = id
        _activeChannel.value = null
        _channelMessages.value = emptyList()
        nexus.send(NexusMessage(type = "server_info", serverId = id))
    }

    fun selectChannel(id: String) {
        if (id.isBlank()) { _activeChannel.value = null; return }
        _activeChannel.value = id
        _channelMessages.value = emptyList()
        nexus.send(NexusMessage(type = "channel_history", channelId = id))
    }

    fun sendChannelMessage(text: String) {
        val ch = _activeChannel.value ?: return
        val me = _me.value ?: return
        nexus.send(NexusMessage(type = "channel_msg", sender = me, channelId = ch, body = text))
    }

    fun createSpace(name: String, visibility: String) {
        nexus.send(NexusMessage(type = "server_create", serverName = name, visibility = visibility))
    }

    /** Load the public-space directory (Discover). */
    fun discoverSpaces() {
        nexus.send(NexusMessage(type = "server_discover"))
    }

    /** Join a public space straight from Discover — no invite code. */
    fun joinPublicSpace(id: String) {
        nexus.send(NexusMessage(type = "server_join", serverId = id))
    }

    fun joinSpace(code: String) {
        nexus.send(NexusMessage(type = "server_join", inviteCode = code))
    }

    // Stories
    fun loadStories() {
        val token = _sessionToken.value ?: return
        viewModelScope.launch(kotlinx.coroutines.Dispatchers.IO) {
            try {
                val url = java.net.URL("https://phazechat.world/api/v1/stories")
                val conn = url.openConnection() as java.net.HttpURLConnection
                conn.setRequestProperty("Authorization", "Bearer $token")
                if (conn.responseCode == 200) {
                    val resp = conn.inputStream.bufferedReader().readText()
                    val arr = org.json.JSONArray(resp)
                    val list = (0 until arr.length()).map { i ->
                        val s = arr.getJSONObject(i)
                        world.phazechat.app.ui.Story(
                            id = s.optString("id", "$i"),
                            author = s.optString("author", ""),
                            mediaUrl = s.optString("media_url", ""),
                            mediaKind = s.optString("media_kind", "image"),
                            createdAt = s.optString("created_at", ""),
                        )
                    }
                    _stories.value = list
                }
                conn.disconnect()
            } catch (e: Exception) { Log.w(TAG, "loadStories: ${e.message}") }
        }
    }

    fun postStory(uri: android.net.Uri) {
        val token = _sessionToken.value ?: return
        viewModelScope.launch(kotlinx.coroutines.Dispatchers.IO) {
            try {
                val cr = getApplication<Application>().contentResolver
                val bytes = cr.openInputStream(uri)?.readBytes() ?: return@launch
                val fileName = uri.lastPathSegment?.substringAfterLast('/') ?: "story.jpg"
                val boundary = "----PhazeStory${System.currentTimeMillis()}"
                val body = buildMultipart(boundary, fileName, bytes)

                // Upload
                val uploadUrl = java.net.URL("https://phazechat.world/api/v1/upload")
                val uc = uploadUrl.openConnection() as java.net.HttpURLConnection
                uc.requestMethod = "POST"
                uc.setRequestProperty("Content-Type", "multipart/form-data; boundary=$boundary")
                uc.setRequestProperty("Authorization", "Bearer $token")
                uc.doOutput = true
                uc.outputStream.write(body)
                if (uc.responseCode != 200) { uc.disconnect(); return@launch }
                val uploadResp = JSONObject(uc.inputStream.bufferedReader().readText())
                val mediaUrl = uploadResp.optString("url", "")
                val mime = uploadResp.optString("mime", "image/jpeg")
                uc.disconnect()
                if (mediaUrl.isEmpty()) return@launch

                // Post story
                val kind = if (mime.startsWith("video/")) "video" else "image"
                val storyUrl = java.net.URL("https://phazechat.world/api/v1/stories")
                val sc = storyUrl.openConnection() as java.net.HttpURLConnection
                sc.requestMethod = "POST"
                sc.setRequestProperty("Content-Type", "application/json")
                sc.setRequestProperty("Authorization", "Bearer $token")
                sc.doOutput = true
                sc.outputStream.write(JSONObject().apply {
                    put("media_url", mediaUrl)
                    put("media_kind", kind)
                }.toString().toByteArray())
                sc.responseCode
                sc.disconnect()

                loadStories()
            } catch (e: Exception) { Log.w(TAG, "postStory: ${e.message}") }
        }
    }

    // Calls
    fun startCall(peer: String, withVideo: Boolean = false) {
        val me = _me.value ?: return
        val cm = CallManager(getApplication())
        callManager = cm
        _callState.value = CallState(peer = peer, status = "ringing", isCameraOn = withVideo, isVideo = withVideo)

        val iceServers = buildIceServers()
        cm.createPeerConnection(iceServers)
        cm.startLocalMedia(getApplication(), withVideo)

        cm.onRemoteVideoTrack = {
            _callState.value = _callState.value?.copy(hasRemoteVideo = true)
        }

        cm.onIceCandidate = { candidate ->
            nexus.send(NexusMessage(
                type = "ice_candidate", sender = me, recipient = peer,
                candidate = org.json.JSONObject().apply {
                    put("candidate", candidate.sdp)
                    put("sdpMid", candidate.sdpMid)
                    put("sdpMLineIndex", candidate.sdpMLineIndex)
                }.toString(),
            ))
        }
        cm.onConnectionChange = { state ->
            when (state) {
                org.webrtc.PeerConnection.IceConnectionState.CONNECTED ->
                    _callState.value = _callState.value?.copy(status = "connected")
                org.webrtc.PeerConnection.IceConnectionState.DISCONNECTED,
                org.webrtc.PeerConnection.IceConnectionState.FAILED ->
                    endCall()
                else -> {}
            }
        }

        viewModelScope.launch {
            val offer = cm.createOffer() ?: return@launch
            cm.setLocalDescription(offer)
            nexus.send(NexusMessage(
                type = "call_offer", sender = me, recipient = peer, sdp = offer.description,
            ))
        }
    }

    fun answerCall() {
        val cs = _callState.value ?: return
        val me = _me.value ?: return
        val sdp = cs.incomingSdp ?: return
        // Honor video: if the caller's offer advertises a video m-line, answer with video too.
        val withVideo = sdp.contains("m=video")
        val cm = CallManager(getApplication())
        callManager = cm
        _callState.value = cs.copy(status = "connecting", isVideo = withVideo, isCameraOn = withVideo)

        val iceServers = buildIceServers()
        cm.createPeerConnection(iceServers)
        cm.startLocalMedia(getApplication(), withVideo)

        cm.onRemoteVideoTrack = {
            _callState.value = _callState.value?.copy(hasRemoteVideo = true)
        }

        cm.onIceCandidate = { candidate ->
            nexus.send(NexusMessage(
                type = "ice_candidate", sender = me, recipient = cs.peer,
                candidate = org.json.JSONObject().apply {
                    put("candidate", candidate.sdp)
                    put("sdpMid", candidate.sdpMid)
                    put("sdpMLineIndex", candidate.sdpMLineIndex)
                }.toString(),
            ))
        }
        cm.onConnectionChange = { state ->
            when (state) {
                org.webrtc.PeerConnection.IceConnectionState.CONNECTED ->
                    _callState.value = _callState.value?.copy(status = "connected")
                org.webrtc.PeerConnection.IceConnectionState.DISCONNECTED,
                org.webrtc.PeerConnection.IceConnectionState.FAILED ->
                    endCall()
                else -> {}
            }
        }
        cm.onRemoteStream = { /* audio plays automatically via AudioTrack */ }

        viewModelScope.launch {
            cm.setRemoteDescription(org.webrtc.SessionDescription(org.webrtc.SessionDescription.Type.OFFER, sdp))
            val answer = cm.createAnswer() ?: return@launch
            cm.setLocalDescription(answer)
            nexus.send(NexusMessage(
                type = "call_answer", sender = me, recipient = cs.peer, sdp = answer.description,
            ))
        }
    }

    fun rejectCall() {
        val cs = _callState.value ?: return
        nexus.send(NexusMessage(type = "call_reject", sender = _me.value, recipient = cs.peer))
        _callState.value = null
    }

    fun endCall() {
        val cs = _callState.value ?: return
        nexus.send(NexusMessage(type = "call_end", sender = _me.value, recipient = cs.peer))
        callManager?.hangUp()
        callManager = null
        _callState.value = null
    }

    fun toggleCallMute() {
        val muted = callManager?.toggleMute() ?: return
        _callState.value = _callState.value?.copy(isMuted = muted)
    }

    fun toggleCallCamera() {
        val on = callManager?.toggleCamera() ?: return
        _callState.value = _callState.value?.copy(isCameraOn = on)
    }

    fun toggleSpeakerphone(): Boolean {
        return callManager?.toggleSpeakerphone() ?: false
    }

    fun openChat(peer: String) {
        _pendingOpenChat.value = peer
    }

    /** Begin screen sharing with the Intent returned by the MediaProjection permission prompt. */
    fun startScreenShare(projectionData: android.content.Intent) {
        val cm = callManager ?: return
        cm.startScreenShare(projectionData)
        _callState.value = _callState.value?.copy(isScreenSharing = true)
    }

    fun stopScreenShare() {
        val cm = callManager ?: return
        cm.stopScreenShare(getApplication())
        _callState.value = _callState.value?.copy(isScreenSharing = cm.isScreenSharing)
    }

    private fun buildIceServers(): List<org.webrtc.PeerConnection.IceServer> {
        val servers = mutableListOf(
            org.webrtc.PeerConnection.IceServer.builder("stun:stun.l.google.com:19302").createIceServer()
        )
        if (turnUrl != null) {
            servers.add(
                org.webrtc.PeerConnection.IceServer.builder(turnUrl!!)
                    .setUsername(turnUsername ?: "")
                    .setPassword(turnPassword ?: "")
                    .createIceServer()
            )
        }
        return servers
    }

    private fun appendChat(line: ChatLine) {
        _chatLog.value = _chatLog.value + line
    }

    private fun decrypt(body: String?, sender: String?): String {
        if (body == null) return ""
        val pk = sender?.let { peerKeys[it] } ?: return body
        return try { decryptFromPeer(body, pk, keyPair.secretKey) } catch (_: Exception) { "[Encrypted]" }
    }

    private fun observeMessages() {
        viewModelScope.launch {
            nexus.messages.collect { msg ->
                handleMessage(msg)
            }
        }
    }

    private fun handleMessage(msg: NexusMessage) {
        when (msg.type) {
            "link_check" -> {
                if (msg.status == "approved" && msg.qrToken != null) {
                    linkPollJob?.cancel()
                    _me.value = msg.sender
                    _authError.value = null
                    _sessionToken.value = msg.qrToken
                    prefs.edit().putString("session_token", msg.qrToken).apply()
                    msg.sender?.let { prefs.edit().putString("username", it).apply() }
                    
                    if (msg.turnUrl != null) {
                        turnUrl = msg.turnUrl; turnUsername = msg.turnUsername; turnPassword = msg.turnPassword
                    }
                    nexus.send(NexusMessage(
                        type = "presence", sender = msg.sender, status = "Online",
                        publicKey = encodePublicKeyB64(keyPair.publicKey),
                        keyFingerprint = fingerprint(keyPair.publicKey),
                    ))
                    val fcmToken = prefs.getString("fcm_token", null)
                    if (!fcmToken.isNullOrEmpty()) {
                        nexus.send(NexusMessage(type = "register_fcm_token", body = fcmToken))
                    }
                    loadSpaces()
                } else if (msg.error != null) {
                    linkPollJob?.cancel()
                    _authError.value = msg.error
                }
            }

            "link_result" -> {
                if (msg.status == "ok") {
                    _activeLinkCode.value = msg.token
                    _linkStatus.value = "Link Code generated. Share it with your new device."
                } else if (msg.status == "approved") {
                    _linkStatus.value = "Device approved successfully."
                    _activeLinkCode.value = null
                } else if (msg.error != null) {
                    _linkError.value = msg.error
                    _activeLinkCode.value = null
                }
            }

            "qr_login_result" -> {
                if (msg.status == "approved") {
                    _linkStatus.value = "Device approved successfully."
                } else if (msg.error != null) {
                    _linkError.value = msg.error
                }
            }

            "auth_result" -> {
                if (msg.status == "ok") {
                    _me.value = msg.sender
                    _authError.value = null
                    msg.qrToken?.let { tok ->
                        _sessionToken.value = tok
                        prefs.edit().putString("session_token", tok).apply()
                    }
                    msg.sender?.let { prefs.edit().putString("username", it).apply() }
                    if (msg.turnUrl != null) {
                        turnUrl = msg.turnUrl; turnUsername = msg.turnUsername; turnPassword = msg.turnPassword
                    }
                    nexus.send(NexusMessage(
                        type = "presence", sender = msg.sender, status = "Online",
                        publicKey = encodePublicKeyB64(keyPair.publicKey),
                        keyFingerprint = fingerprint(keyPair.publicKey),
                    ))
                    // Refresh open chat after reconnect so missed messages appear
                    _selectedChat.value?.let { peer ->
                        nexus.send(NexusMessage(type = "dm_history", sender = msg.sender, recipient = peer))
                    }
                    // Register FCM token so push notifications work on this device
                    val fcmToken = prefs.getString("fcm_token", null)
                    if (!fcmToken.isNullOrEmpty()) {
                        nexus.send(NexusMessage(type = "register_fcm_token", body = fcmToken))
                    }
                    loadSpaces()
                } else {
                    _authError.value = msg.error ?: msg.status ?: "Auth failed"
                }
            }

            "register_result" -> {
                _authError.value = when (msg.status) {
                    "ok" -> "Account created. Sign in."
                    "pending_verification" -> "Check email for verification code."
                    else -> msg.error ?: "Registration failed"
                }
            }

            "update_result" -> {
                if (msg.status == "ok") {
                    prefs.edit().putString("display_name", _myDisplayName.value).apply()
                }
            }

            "profile_update" -> {
                msg.sender?.let { sender ->
                    _friends.value = _friends.value.toMutableMap().apply {
                        val existing = get(sender)
                        if (existing != null) {
                            put(sender, existing.copy(mood = msg.mood ?: existing.mood))
                        }
                    }
                }
            }

            "friend_status", "presence" -> {
                msg.sender?.let { sender ->
                    _friends.value = _friends.value.toMutableMap().apply {
                        val existing = get(sender)
                        put(sender, FriendInfo(sender, msg.status ?: "Offline", existing?.mood, msg.supporter || (existing?.supporter ?: false)))
                    }
                    msg.publicKey?.let { pk -> decodePublicKeyB64(pk)?.let { peerKeys[sender] = it } }
                }
            }

            "friend_request" -> {
                msg.sender?.let { s -> if (s !in _pending.value) _pending.value = _pending.value + s }
            }

            "friend_accepted" -> {
                msg.sender?.let { s ->
                    _friends.value = _friends.value.toMutableMap().apply { put(s, FriendInfo(s, msg.status ?: "Online")) }
                }
            }

            "pending_requests" -> { _pending.value = msg.results ?: emptyList() }

            "msg" -> {
                val sender = msg.sender ?: return
                // Skip server echo of our own messages — already appended optimistically.
                if (sender == _me.value) return
                val text = decrypt(msg.body, sender)
                val line = ChatLine(
                    id = msg.msgId ?: "${sender}-${System.nanoTime()}",
                    from = sender, text = text.ifEmpty { "[Encrypted]" }, me = false,
                    kind = msg.kind, fileUrl = msg.fileUrl, fileName = msg.fileName,
                )
                if (_selectedChat.value == sender) appendChat(line)
                else _unread.value = _unread.value.toMutableMap().apply { put(sender, (get(sender) ?: 0) + 1) }
                if (sender !in _friends.value) {
                    _friends.value = _friends.value.toMutableMap().apply { put(sender, FriendInfo(sender)) }
                }
            }

            "dm_history" -> handleDmHistory(msg)

            "key_request" -> {
                msg.sender?.let { sender ->
                    nexus.send(NexusMessage(
                        type = "presence", sender = _me.value, recipient = sender, status = "Online",
                        publicKey = encodePublicKeyB64(keyPair.publicKey),
                        keyFingerprint = fingerprint(keyPair.publicKey),
                    ))
                }
            }

            // Spaces
            "server_list_result" -> handleServerList(msg)
            "server_discover_result" -> handleDiscoverList(msg)
            "server_info_result", "server_channels_updated" -> handleChannels(msg)
            "channel_history_result" -> handleChannelHistory(msg)
            "channel_msg" -> handleChannelMsg(msg)
            "server_created", "server_joined" -> loadSpaces()
            "channel_result" -> { msg.error?.let { _actionStatus.value = "Channel: $it" } }

            // Search
            "search_results" -> _searchResults.value = msg.results ?: emptyList()

            // Block / report confirmations
            "block_result" -> {
                when (msg.status) {
                    "blocked" -> { _actionStatus.value = "Blocked ${msg.recipient}"; msg.recipient?.let { dropChat(it) } }
                    "unblocked" -> _actionStatus.value = "Unblocked ${msg.recipient}"
                    else -> msg.error?.let { _actionStatus.value = "Block: $it" }
                }
            }
            "report_result" -> {
                if (msg.status == "received") _actionStatus.value = "Report received. Thank you."
                else msg.error?.let { _actionStatus.value = "Report: $it" }
            }

            "kicked" -> {
                _authError.value = msg.body ?: "Signed in from another location"
                signOut()
            }

            // Account deletion confirmation
            "delete_account_result" -> {
                if (msg.status == "ok") {
                    _actionStatus.value = "Your account has been deleted."
                    signOut()
                } else {
                    _actionStatus.value = msg.error ?: "Couldn't delete account. Try again."
                }
            }

            // Typing indicator from a peer
            "typing" -> {
                val from = msg.sender ?: return
                if (_selectedChat.value == from) {
                    _typingFrom.value = from
                    typingClearJob?.cancel()
                    typingClearJob = viewModelScope.launch {
                        kotlinx.coroutines.delay(4000)
                        _typingFrom.value = null
                    }
                }
            }

            // Admin broadcast popup to every connected client
            "global_notice" -> {
                val text = msg.body ?: return
                _globalNotice.value = GlobalNotice(from = msg.sender ?: "Phaze", message = text)
            }

            // Live edit / delete / react relays for DMs
            "read_receipt" -> {
                // Mark all our sent messages to this peer as seen
                _chatLog.value = _chatLog.value.map { if (it.me && !it.seen) it.copy(seen = true) else it }
            }
            "msg_edit" -> {
                val id = msg.msgId ?: return
                val sender = msg.sender ?: return
                val text = decrypt(msg.body, sender)
                _chatLog.value = _chatLog.value.map { if (it.id == id) it.copy(text = text, edited = true) else it }
            }
            "msg_delete" -> {
                val id = msg.msgId ?: return
                _chatLog.value = _chatLog.value.map { if (it.id == id) it.copy(text = "", deleted = true) else it }
            }
            "msg_react" -> {
                val id = msg.msgId ?: return
                val emoji = msg.reaction
                _chatLog.value = _chatLog.value.map {
                    if (it.id == id) it.copy(reaction = if (it.reaction == emoji) null else emoji) else it
                }
            }

            // Call signaling
            "call_offer" -> {
                val from = msg.sender ?: return
                _callState.value = CallState(peer = from, status = "ringing", isIncoming = true, incomingSdp = msg.sdp, isVideo = (msg.sdp?.contains("m=video") == true))
            }
            "call_answer" -> {
                val sdp = msg.sdp ?: return
                viewModelScope.launch {
                    callManager?.setRemoteDescription(
                        org.webrtc.SessionDescription(org.webrtc.SessionDescription.Type.ANSWER, sdp)
                    )
                    _callState.value = _callState.value?.copy(status = "connecting")
                }
            }
            "ice_candidate" -> {
                val raw = msg.candidate ?: return
                try {
                    val j = org.json.JSONObject(raw)
                    callManager?.addIceCandidate(
                        org.webrtc.IceCandidate(j.getString("sdpMid"), j.getInt("sdpMLineIndex"), j.getString("candidate"))
                    )
                } catch (_: Exception) {}
            }
            "call_reject", "call_end" -> {
                callManager?.hangUp()
                callManager = null
                _callState.value = null
            }

            "key_backup_result" -> handleKeyBackupResult(msg)

            "totp_result" -> when {
                msg.status == "pending_confirm" -> {
                    _twoFactorUri.value = msg.totpUri
                    _twoFactorStatus.value = "Scan the code in your authenticator, then enter a code to confirm."
                }
                msg.status == "enabled" -> {
                    _twoFactorUri.value = null
                    _twoFactorEnabled.value = true
                    _twoFactorStatus.value = "✓ 2FA enabled"
                    if (!msg.backupCodes.isNullOrEmpty()) _twoFactorBackupCodes.value = msg.backupCodes
                }
                msg.status == "disabled" -> {
                    _twoFactorEnabled.value = false
                    _twoFactorStatus.value = "2FA disabled"
                    _twoFactorBackupCodes.value = null
                }
                msg.error != null -> _twoFactorStatus.value = msg.error
            }
        }
    }

    private fun handleKeyBackupResult(msg: NexusMessage) {
        when {
            // Server acknowledged a successful put (status = "stored")
            msg.status == "stored" -> {
                pendingRestorePin = null
                _keyBackupStatus.value = "✓ Key backup saved successfully"
            }
            // Server says no backup exists
            msg.status == "not_found" -> {
                pendingRestorePin = null
                _keyBackupStatus.value = null
                _keyBackupError.value = "No backup found on server. Back up your keys first."
            }
            // Server returned the backup blob (status = "ok", token field contains JSON blob)
            msg.status == "ok" && msg.token != null -> {
                val pin = pendingRestorePin ?: run {
                    _keyBackupError.value = "No PIN found for restoration"
                    _keyBackupStatus.value = null
                    return
                }
                pendingRestorePin = null
                viewModelScope.launch(kotlinx.coroutines.Dispatchers.Default) {
                    try {
                        val j = org.json.JSONObject(msg.token)
                        val blob = world.phazechat.app.crypto.KeyBackupBlob(
                            ciphertext = j.getString("ciphertext"),
                            salt = j.getString("salt"),
                            iterations = j.getInt("iterations"),
                        )
                        val restored = world.phazechat.app.crypto.KeyBackup.decryptKeypair(blob, pin)
                        // Persist and activate restored keypair
                        keyPair = restored
                        prefs.edit()
                            .putString("nacl_pub", world.phazechat.app.crypto.encodePublicKeyB64(restored.publicKey))
                            .putString("nacl_sec", world.phazechat.app.crypto.encodePublicKeyB64(restored.secretKey))
                            .apply()
                        _keyBackupStatus.value = "✓ Keys restored successfully"
                        _keyBackupError.value = null
                    } catch (e: Exception) {
                        _keyBackupStatus.value = null
                        _keyBackupError.value = "Restore failed: ${e.message}"
                    }
                }
            }
            msg.error != null -> {
                pendingRestorePin = null
                _keyBackupStatus.value = null
                _keyBackupError.value = msg.error
            }
        }
    }

    private fun handleDmHistory(msg: NexusMessage) {
        val me = _me.value ?: return
        val peer = _selectedChat.value ?: return
        val raw = msg.rawDmHistory ?: return
        try {
            val arr = JSONArray(raw)
            val lines = mutableListOf<ChatLine>()
            for (i in 0 until arr.length()) {
                val r = arr.getJSONObject(i)
                val sender = r.getString("sender")
                val isMe = sender == me
                var text = r.optString("body", "")
                val pk = peerKeys[if (isMe) peer else sender]
                if (pk != null && text.isNotEmpty()) {
                    text = try { decryptFromPeer(text, pk, keyPair.secretKey) } catch (_: Exception) { "[Encrypted]" }
                }
                lines.add(ChatLine(
                    id = r.optString("msg_id", "$i"),
                    from = sender, text = text, me = isMe,
                    ts = try {
                        val sdf = java.text.SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ss", java.util.Locale.US)
                        sdf.timeZone = java.util.TimeZone.getTimeZone("UTC")
                        sdf.parse(r.getString("created_at"))?.time ?: 0L
                    } catch (_: Exception) { 0L },
                ))
            }
            // Preserve optimistic messages we sent that haven't been persisted yet
            val existingOptimistic = _chatLog.value.filter { existing ->
                existing.me && lines.none { l -> l.id == existing.id }
            }
            _chatLog.value = (lines + existingOptimistic).sortedBy { it.ts }
        } catch (e: Exception) {
            Log.w(TAG, "dm_history parse: ${e.message}")
        }
    }

    private fun handleServerList(msg: NexusMessage) {
        val raw = msg.rawServers ?: return
        try {
            val arr = JSONArray(raw)
            _spaces.value = (0 until arr.length()).map { i ->
                val s = arr.getJSONObject(i)
                SpaceInfo(
                    id = s.getString("id"), name = s.getString("name"),
                    description = s.optString("description", null),
                    owner = s.optString("owner", ""),
                    visibility = s.optString("visibility", "private"),
                    role = s.optString("role", "member"),
                )
            }
        } catch (e: Exception) { Log.w(TAG, "server_list: ${e.message}") }
    }

    private fun handleDiscoverList(msg: NexusMessage) {
        val raw = msg.rawServers ?: return
        try {
            val arr = JSONArray(raw)
            _discoverSpaces.value = (0 until arr.length()).map { i ->
                val s = arr.getJSONObject(i)
                SpaceInfo(
                    id = s.getString("id"), name = s.getString("name"),
                    description = s.optString("description", null),
                    owner = s.optString("owner", ""),
                    visibility = s.optString("visibility", "public"),
                    memberCount = s.optInt("member_count", 0),
                    isMember = s.optBoolean("is_member", false),
                )
            }
        } catch (e: Exception) { Log.w(TAG, "server_discover: ${e.message}") }
    }

    private fun handleChannels(msg: NexusMessage) {
        val serverId = msg.serverId ?: return
        val raw = msg.rawChannels ?: return
        try {
            val arr = JSONArray(raw)
            val list = (0 until arr.length()).map { i ->
                val c = arr.getJSONObject(i)
                ChannelInfo(
                    id = c.getString("id"), serverId = serverId, name = c.getString("name"),
                    topic = c.optString("topic", null), kind = c.optString("kind", "text"),
                )
            }
            _channels.value = _channels.value.toMutableMap().apply { put(serverId, list) }
        } catch (e: Exception) { Log.w(TAG, "channels: ${e.message}") }
    }

    private fun handleChannelHistory(msg: NexusMessage) {
        val raw = msg.rawMessages ?: return
        try {
            val arr = JSONArray(raw)
            _channelMessages.value = (0 until arr.length()).map { i ->
                val m = arr.getJSONObject(i)
                ChannelMsg(
                    id = m.getLong("id"), sender = m.getString("sender"),
                    body = m.getString("body"), createdAt = m.optString("created_at", ""),
                    edited = m.optBoolean("edited"), deleted = m.optBoolean("deleted"),
                )
            }
        } catch (e: Exception) { Log.w(TAG, "channel_history: ${e.message}") }
    }

    private fun handleChannelMsg(msg: NexusMessage) {
        if (msg.channelId != _activeChannel.value) return
        val sender = msg.sender ?: return
        _channelMessages.value = _channelMessages.value + ChannelMsg(
            id = System.currentTimeMillis(), sender = sender,
            body = msg.body ?: "", createdAt = "",
        )
    }
}
