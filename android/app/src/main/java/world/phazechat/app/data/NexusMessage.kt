package world.phazechat.app.data

import org.json.JSONObject

data class NexusMessage(
    val type: String,
    val sender: String? = null,
    val recipient: String? = null,
    val body: String? = null,
    val status: String? = null,
    val error: String? = null,
    val results: List<String>? = null,
    val sdp: String? = null,
    val candidate: String? = null,
    val qrToken: String? = null,
    val email: String? = null,
    val mood: String? = null,
    val displayName: String? = null,
    val convoId: String? = null,
    val convoName: String? = null,
    val members: List<String>? = null,
    val turnUrl: String? = null,
    val turnUsername: String? = null,
    val turnPassword: String? = null,
    val totpCode: String? = null,
    val totpUri: String? = null,
    val deviceInfo: String? = null,
    val publicKey: String? = null,
    val keyFingerprint: String? = null,
    val msgId: String? = null,
    val reaction: String? = null,
    val kind: String? = null,
    val fileUrl: String? = null,
    val fileName: String? = null,
    val serverId: String? = null,
    val channelId: String? = null,
    val serverName: String? = null,
    val channelName: String? = null,
) {
    fun toJson(): JSONObject = JSONObject().apply {
        put("type", type)
        sender?.let { put("sender", it) }
        recipient?.let { put("recipient", it) }
        body?.let { put("body", it) }
        status?.let { put("status", it) }
        qrToken?.let { put("qr_token", it) }
        email?.let { put("email", it) }
        mood?.let { put("mood", it) }
        totpCode?.let { put("totp_code", it) }
        deviceInfo?.let { put("device_info", it) }
        publicKey?.let { put("public_key", it) }
        keyFingerprint?.let { put("key_fingerprint", it) }
        msgId?.let { put("msg_id", it) }
        reaction?.let { put("reaction", it) }
        kind?.let { put("kind", it) }
        fileUrl?.let { put("file_url", it) }
        fileName?.let { put("file_name", it) }
        convoId?.let { put("convo_id", it) }
        convoName?.let { put("convo_name", it) }
        members?.let { put("members", org.json.JSONArray(it)) }
        serverId?.let { put("server_id", it) }
        channelId?.let { put("channel_id", it) }
        serverName?.let { put("server_name", it) }
        channelName?.let { put("channel_name", it) }
        candidate?.let { put("candidate", it) }
        sdp?.let { put("sdp", it) }
    }

    companion object {
        fun fromJson(j: JSONObject): NexusMessage {
            val results = if (j.has("results")) {
                val arr = j.getJSONArray("results")
                (0 until arr.length()).map { arr.getString(it) }
            } else null

            val members = if (j.has("members")) {
                val arr = j.getJSONArray("members")
                (0 until arr.length()).map { arr.getString(it) }
            } else null

            var turnUrl: String? = null
            var turnUser: String? = null
            var turnPass: String? = null
            if (j.has("turn_config")) {
                val tc = j.getJSONObject("turn_config")
                turnUrl = tc.optString("url", null)
                turnUser = tc.optString("username", null)
                turnPass = tc.optString("password", null)
            }

            return NexusMessage(
                type = j.getString("type"),
                sender = j.optString("sender", null),
                recipient = j.optString("recipient", null),
                body = j.optString("body", null),
                status = j.optString("status", null),
                error = j.optString("error", null),
                results = results,
                sdp = j.optString("sdp", null),
                candidate = j.optString("candidate", null),
                qrToken = j.optString("qr_token", null),
                email = j.optString("email", null),
                mood = j.optString("mood", null),
                displayName = j.optString("display_name", null),
                convoId = j.optString("convo_id", null),
                convoName = j.optString("convo_name", null),
                members = members,
                turnUrl = turnUrl,
                turnUsername = turnUser,
                turnPassword = turnPass,
                totpCode = j.optString("totp_code", null),
                totpUri = j.optString("totp_uri", null),
                deviceInfo = j.optString("device_info", null),
                publicKey = j.optString("public_key", null),
                keyFingerprint = j.optString("key_fingerprint", null),
                msgId = j.optString("msg_id", null),
                reaction = j.optString("reaction", null),
                kind = j.optString("kind", null),
                fileUrl = j.optString("file_url", null),
                fileName = j.optString("file_name", null),
                serverId = j.optString("server_id", null),
                channelId = j.optString("channel_id", null),
                serverName = j.optString("server_name", null),
                channelName = j.optString("channel_name", null),
            )
        }
    }
}
