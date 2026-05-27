package world.phazechat.app.data

import android.app.Application
import android.content.Context
import android.os.Build
import android.util.Log
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.launch
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
)

data class FriendInfo(
    val username: String,
    val status: String = "Offline",
    val mood: String? = null,
)

data class SpaceInfo(
    val id: String,
    val name: String,
    val description: String? = null,
    val owner: String = "",
    val visibility: String = "private",
    val role: String = "member",
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

    // Spaces
    private val _spaces = MutableStateFlow<List<SpaceInfo>>(emptyList())
    val spaces = _spaces.asStateFlow()
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

    // Crypto
    private var keyPair: NaClKeyPair
    private val peerKeys = mutableMapOf<String, ByteArray>()

    // TURN
    var turnUrl: String? = null; private set
    var turnUsername: String? = null; private set
    var turnPassword: String? = null; private set

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
        nexus.send(NexusMessage(type = "auth", sender = username, body = password, deviceInfo = "android/${Build.MODEL}"))
    }

    fun register(username: String, email: String, password: String) {
        _authError.value = null
        nexus.send(NexusMessage(type = "register", sender = username, body = password, email = email))
    }

    fun selectChat(peer: String) {
        if (peer.isBlank()) { _selectedChat.value = null; return }
        _selectedChat.value = peer
        _unread.value = _unread.value.toMutableMap().apply { remove(peer) }
        _chatLog.value = emptyList()
        _me.value?.let { nexus.send(NexusMessage(type = "dm_history", sender = it, recipient = peer)) }
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
        _friends.value = _friends.value.toMutableMap().apply { put(from, FriendInfo(from, "Online")) }
    }

    fun signOut() {
        prefs.edit().remove("session_token").remove("username").apply()
        _me.value = null
        _sessionToken.value = null
        _friends.value = emptyMap()
        _chatLog.value = emptyList()
        _selectedChat.value = null
        _spaces.value = emptyList()
        nexus.disconnect()
        nexus.connect()
    }

    // Spaces
    fun loadSpaces() { nexus.send(NexusMessage(type = "server_list")) }

    fun selectSpace(id: String) {
        if (id.isBlank()) { _activeSpace.value = null; _activeChannel.value = null; return }
        _activeSpace.value = id
        _activeChannel.value = null
        _channelMessages.value = emptyList()
        nexus.send(NexusMessage(type = "server_channels", serverId = id))
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
        nexus.send(NexusMessage(type = "server_create", serverName = name, body = visibility))
    }

    fun joinSpace(code: String) {
        nexus.send(NexusMessage(type = "server_join", body = code))
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
                handleMessage(msg)
            }
        }
    }

    private fun handleMessage(msg: NexusMessage) {
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
                        turnUrl = msg.turnUrl; turnUsername = msg.turnUsername; turnPassword = msg.turnPassword
                    }
                    nexus.send(NexusMessage(
                        type = "presence", sender = msg.sender, status = "Online",
                        publicKey = encodePublicKeyB64(keyPair.publicKey),
                        keyFingerprint = fingerprint(keyPair.publicKey),
                    ))
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

            "friend_status", "presence" -> {
                msg.sender?.let { sender ->
                    _friends.value = _friends.value.toMutableMap().apply {
                        val existing = get(sender)
                        put(sender, FriendInfo(sender, msg.status ?: "Offline", existing?.mood))
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
            "server_channels_result" -> handleChannels(msg)
            "channel_history_result" -> handleChannelHistory(msg)
            "channel_msg" -> handleChannelMsg(msg)
            "server_created", "server_joined" -> loadSpaces()
        }
    }

    private fun handleDmHistory(msg: NexusMessage) {
        val me = _me.value ?: return
        val peer = _selectedChat.value ?: return
        val raw = msg.toJson().optString("raw_dm_history", null) ?: return
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
                    text = try { decryptFromPeer(text, pk, keyPair.secretKey) } catch (_: Exception) { text }
                }
                lines.add(ChatLine(
                    id = r.optString("msg_id", "$i"),
                    from = sender, text = text, me = isMe,
                    ts = try { java.text.SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ss", java.util.Locale.US).parse(r.getString("created_at"))?.time ?: 0L } catch (_: Exception) { 0L },
                ))
            }
            _chatLog.value = lines.sortedBy { it.ts }
        } catch (e: Exception) {
            Log.w(TAG, "dm_history parse: ${e.message}")
        }
    }

    private fun handleServerList(msg: NexusMessage) {
        val raw = msg.toJson().optString("raw_servers", null) ?: return
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

    private fun handleChannels(msg: NexusMessage) {
        val serverId = msg.serverId ?: return
        val raw = msg.toJson().optString("raw_channels", null) ?: return
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
        val raw = msg.toJson().optString("raw_messages", null) ?: return
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
