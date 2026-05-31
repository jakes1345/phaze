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

    // Calls
    data class CallState(
        val peer: String,
        val status: String = "ringing", // ringing, connecting, connected, ended
        val isIncoming: Boolean = false,
        val isMuted: Boolean = false,
        val isCameraOn: Boolean = false,
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
            while (me.value == null && _authError.value?.contains("Waiting") == true) {
                nexus.send(NexusMessage(type = "link_check", token = finalToken))
                delay(2500)
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

    fun sendFile(uri: android.net.Uri) {
        val peer = _selectedChat.value ?: return
        val username = _me.value ?: return
        val token = _sessionToken.value ?: return
        viewModelScope.launch(kotlinx.coroutines.Dispatchers.IO) {
            try {
                val cr = getApplication<Application>().contentResolver
                val fileName = uri.lastPathSegment?.substringAfterLast('/') ?: "file"
                val bytes = cr.openInputStream(uri)?.readBytes() ?: return@launch
                val boundary = "----PhazeUpload${System.currentTimeMillis()}"
                val body = buildMultipart(boundary, fileName, bytes)
                val url = java.net.URL("https://phazechat.world/api/v1/upload")
                val conn = url.openConnection() as java.net.HttpURLConnection
                conn.requestMethod = "POST"
                conn.setRequestProperty("Content-Type", "multipart/form-data; boundary=$boundary")
                conn.setRequestProperty("Authorization", "Bearer $token")
                conn.doOutput = true
                conn.outputStream.write(body)
                if (conn.responseCode == 200) {
                    val resp = conn.inputStream.bufferedReader().readText()
                    val fileUrl = JSONObject(resp).optString("url", "")
                    if (fileUrl.isNotEmpty()) {
                        val msgId = "${username}-${System.nanoTime()}"
                        nexus.send(NexusMessage(
                            type = "msg", sender = username, recipient = peer,
                            body = "[File: $fileName]", msgId = msgId,
                            kind = "file", fileUrl = fileUrl, fileName = fileName,
                        ))
                        viewModelScope.launch(kotlinx.coroutines.Dispatchers.Main) {
                            appendChat(ChatLine(id = msgId, from = username, text = "[File: $fileName]", me = true, kind = "file", fileUrl = fileUrl, fileName = fileName))
                        }
                    }
                }
                conn.disconnect()
            } catch (e: Exception) { Log.w(TAG, "sendFile: ${e.message}") }
        }
    }

    fun sendVoiceMessage(filePath: String) {
        val peer = _selectedChat.value ?: return
        val username = _me.value ?: return
        val token = _sessionToken.value ?: return
        viewModelScope.launch(kotlinx.coroutines.Dispatchers.IO) {
            try {
                val bytes = java.io.File(filePath).readBytes()
                val boundary = "----PhazeUpload${System.currentTimeMillis()}"
                val body = buildMultipart(boundary, "voice.ogg", bytes)
                val url = java.net.URL("https://phazechat.world/api/v1/upload")
                val conn = url.openConnection() as java.net.HttpURLConnection
                conn.requestMethod = "POST"
                conn.setRequestProperty("Content-Type", "multipart/form-data; boundary=$boundary")
                conn.setRequestProperty("Authorization", "Bearer $token")
                conn.doOutput = true
                conn.outputStream.write(body)
                if (conn.responseCode == 200) {
                    val resp = conn.inputStream.bufferedReader().readText()
                    val fileUrl = JSONObject(resp).optString("url", "")
                    if (fileUrl.isNotEmpty()) {
                        val msgId = "${username}-${System.nanoTime()}"
                        nexus.send(NexusMessage(
                            type = "msg", sender = username, recipient = peer,
                            body = "[Voice Message]", msgId = msgId,
                            kind = "voice", fileUrl = fileUrl, fileName = "voice.ogg",
                        ))
                        viewModelScope.launch(kotlinx.coroutines.Dispatchers.Main) {
                            appendChat(ChatLine(id = msgId, from = username, text = "[Voice Message]", me = true, kind = "voice"))
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
        nexus.send(NexusMessage(type = "status_update", sender = me, body = mood, displayName = displayName, status = "Online"))
    }

    fun enable2FA() {
        nexus.send(NexusMessage(type = "totp_enable", sender = _me.value))
    }

    fun disable2FA() {
        nexus.send(NexusMessage(type = "totp_disable", sender = _me.value))
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
        _callState.value = CallState(peer = peer, status = "ringing", isCameraOn = withVideo)

        val iceServers = buildIceServers()
        cm.createPeerConnection(iceServers)
        cm.startLocalMedia(getApplication(), withVideo)

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
        val cm = CallManager(getApplication())
        callManager = cm
        _callState.value = cs.copy(status = "connecting")

        val iceServers = buildIceServers()
        cm.createPeerConnection(iceServers)
        cm.startLocalMedia(getApplication(), false)

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

            // Call signaling
            "call_offer" -> {
                val from = msg.sender ?: return
                _callState.value = CallState(peer = from, status = "ringing", isIncoming = true, incomingSdp = msg.sdp)
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
