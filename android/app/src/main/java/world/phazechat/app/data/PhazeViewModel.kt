package world.phazechat.app.data

import android.app.Application
import android.content.Context
import android.os.Build
import android.util.Log
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.launch
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
)

data class FriendInfo(
    val username: String,
    val status: String = "Offline",
    val mood: String? = null,
)

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

    // Friends
    private val _friends = MutableStateFlow<Map<String, FriendInfo>>(emptyMap())
    val friends = _friends.asStateFlow()

    private val _pending = MutableStateFlow<List<String>>(emptyList())
    val pending = _pending.asStateFlow()

    // Chat
    private val _selectedChat = MutableStateFlow<String?>(null)
    val selectedChat = _selectedChat.asStateFlow()

    private val _chatLog = MutableStateFlow<List<ChatLine>>(emptyList())
    val chatLog = _chatLog.asStateFlow()

    private val _unread = MutableStateFlow<Map<String, Int>>(emptyMap())
    val unread = _unread.asStateFlow()

    // Crypto
    private var keyPair: NaClKeyPair
    private val peerKeys = mutableMapOf<String, ByteArray>()

    // TURN
    private var turnUrl: String? = null
    private var turnUsername: String? = null
    private var turnPassword: String? = null

    init {
        keyPair = loadOrCreateKeys()
        nexus.connect()
        observeMessages()
        autoLogin()
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

    private fun autoLogin() {
        val tok = _sessionToken.value ?: return
        viewModelScope.launch {
            nexus.state.first { it == ConnState.CONNECTED }
            val device = "android/${Build.MODEL}"
            nexus.send(NexusMessage(type = "session_auth", qrToken = tok, deviceInfo = device))
        }
    }

    fun login(username: String, password: String) {
        _authError.value = null
        val device = "android/${Build.MODEL}"
        nexus.send(NexusMessage(type = "auth", sender = username, body = password, deviceInfo = device))
    }

    fun register(username: String, email: String, password: String) {
        _authError.value = null
        nexus.send(NexusMessage(type = "register", sender = username, body = password, email = email))
    }

    fun selectChat(peer: String) {
        if (peer.isBlank()) {
            _selectedChat.value = null
            return
        }
        _selectedChat.value = peer
        _unread.value = _unread.value.toMutableMap().apply { remove(peer) }
        _chatLog.value = emptyList()
        _me.value?.let {
            nexus.send(NexusMessage(type = "dm_history", sender = it, recipient = peer))
        }
    }

    fun sendMessage(text: String) {
        val peer = _selectedChat.value ?: return
        val username = _me.value ?: return
        val peerPub = peerKeys[peer]
        val body = if (peerPub != null) encryptForPeer(text, peerPub, keyPair.secretKey) else text
        val msgId = "${username}-${System.nanoTime()}"
        nexus.send(NexusMessage(type = "msg", sender = username, recipient = peer, body = body, msgId = msgId))
        appendChat(ChatLine(id = msgId, from = username, text = text, me = true))
    }

    fun sendFriendRequest(to: String) {
        nexus.send(NexusMessage(type = "friend_request", sender = _me.value, recipient = to))
    }

    fun acceptFriend(from: String) {
        nexus.send(NexusMessage(type = "friend_accept", recipient = from))
        _pending.value = _pending.value.filter { it != from }
        _friends.value = _friends.value.toMutableMap().apply {
            put(from, FriendInfo(from, "Online"))
        }
    }

    fun signOut() {
        prefs.edit().remove("session_token").remove("username").apply()
        _me.value = null
        _sessionToken.value = null
        _friends.value = emptyMap()
        _chatLog.value = emptyList()
        _selectedChat.value = null
        nexus.disconnect()
        nexus.connect()
    }

    private fun appendChat(line: ChatLine) {
        _chatLog.value = _chatLog.value + line
    }

    private fun decrypt(body: String?, sender: String?): String {
        if (body == null) return ""
        val pk = sender?.let { peerKeys[it] } ?: return body
        return try { decryptFromPeer(body, pk, keyPair.secretKey) } catch (_: Exception) { body }
    }

    private fun observeMessages() {
        viewModelScope.launch {
            nexus.messages.collect { msg ->
                when (msg.type) {
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
                                turnUrl = msg.turnUrl
                                turnUsername = msg.turnUsername
                                turnPassword = msg.turnPassword
                            }
                            nexus.send(NexusMessage(
                                type = "presence",
                                sender = msg.sender,
                                status = "Online",
                                publicKey = encodePublicKeyB64(keyPair.publicKey),
                                keyFingerprint = fingerprint(keyPair.publicKey),
                            ))
                        } else {
                            _authError.value = msg.error ?: msg.status ?: "Auth failed"
                        }
                    }

                    "register_result" -> {
                        if (msg.status == "ok" || msg.status == "pending_verification") {
                            _authError.value = if (msg.status == "ok") "Account created. Sign in." else "Check email for verification code."
                        } else {
                            _authError.value = msg.error ?: "Registration failed"
                        }
                    }

                    "friend_status", "presence" -> {
                        msg.sender?.let { sender ->
                            _friends.value = _friends.value.toMutableMap().apply {
                                val existing = get(sender)
                                put(sender, FriendInfo(sender, msg.status ?: "Offline", existing?.mood))
                            }
                            if (msg.publicKey != null) {
                                decodePublicKeyB64(msg.publicKey)?.let { pk ->
                                    peerKeys[sender] = pk
                                }
                            }
                        }
                    }

                    "friend_request" -> {
                        msg.sender?.let { s ->
                            if (s !in _pending.value) _pending.value = _pending.value + s
                        }
                    }

                    "friend_accepted" -> {
                        msg.sender?.let { s ->
                            _friends.value = _friends.value.toMutableMap().apply {
                                put(s, FriendInfo(s, msg.status ?: "Online"))
                            }
                        }
                    }

                    "pending_requests" -> {
                        _pending.value = msg.results ?: emptyList()
                    }

                    "msg" -> {
                        val sender = msg.sender ?: return@collect
                        val text = decrypt(msg.body, sender)
                        val line = ChatLine(
                            id = msg.msgId ?: "${sender}-${System.nanoTime()}",
                            from = sender,
                            text = text.ifEmpty { "[Encrypted]" },
                            me = false,
                            kind = msg.kind,
                            fileUrl = msg.fileUrl,
                            fileName = msg.fileName,
                        )
                        if (_selectedChat.value == sender) {
                            appendChat(line)
                        } else {
                            _unread.value = _unread.value.toMutableMap().apply {
                                put(sender, (get(sender) ?: 0) + 1)
                            }
                        }
                    }

                    "dm_history" -> {
                        // Server sends back message history
                    }

                    "key_request" -> {
                        msg.sender?.let { sender ->
                            nexus.send(NexusMessage(
                                type = "presence",
                                sender = _me.value,
                                recipient = sender,
                                status = "Online",
                                publicKey = encodePublicKeyB64(keyPair.publicKey),
                                keyFingerprint = fingerprint(keyPair.publicKey),
                            ))
                        }
                    }
                }
            }
        }
    }
}
