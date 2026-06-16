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
    val supporter: Boolean = false,
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
    val token: String? = null,
    val visibility: String? = null,
    val topic: String? = null,
    val inviteCode: String? = null,
    val rawServers: String? = null,
    val rawChannels: String? = null,
    val rawMessages: String? = null,
    val rawDmHistory: String? = null,
    val backupCodes: List<String>? = null,
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
        displayName?.let { put("display_name", it) }
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
        token?.let { put("token", it) }
        visibility?.let { put("visibility", it) }
        topic?.let { put("topic", it) }
        inviteCode?.let { put("invite_code", it) }
        rawServers?.let { put("raw_servers", it) }
        rawChannels?.let { put("raw_channels", it) }
        rawMessages?.let { put("raw_messages", it) }
    }

    companion object {
        private fun JSONObject.str(key: String): String? {
            val v = opt(key)
            return if (v == null || v == JSONObject.NULL) null else v.toString().ifEmpty { null }
        }

        fun fromJson(j: JSONObject): NexusMessage {
            val results = try {
                if (j.has("results") && !j.isNull("results")) {
                    val arr = j.getJSONArray("results")
                    (0 until arr.length()).map { arr.getString(it) }
                } else null
            } catch (_: Exception) { null }

            val members = try {
                if (j.has("members") && !j.isNull("members")) {
                    val arr = j.getJSONArray("members")
                    (0 until arr.length()).map { arr.getString(it) }
                } else null
            } catch (_: Exception) { null }

            val backupCodes = try {
                if (j.has("backup_codes") && !j.isNull("backup_codes")) {
                    val arr = j.getJSONArray("backup_codes")
                    (0 until arr.length()).map { arr.getString(it) }
                } else null
            } catch (_: Exception) { null }

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
                sender = j.str("sender"),
                recipient = j.str("recipient"),
                body = j.str("body"),
                status = j.str("status"),
                error = j.str("error"),
                results = results,
                sdp = j.str("sdp"),
                candidate = j.str("candidate"),
                qrToken = j.str("qr_token"),
                email = j.str("email"),
                mood = j.str("mood"),
                displayName = j.str("display_name"),
                supporter = j.optBoolean("supporter", false),
                convoId = j.str("convo_id"),
                convoName = j.str("convo_name"),
                members = members,
                turnUrl = turnUrl,
                turnUsername = turnUser,
                turnPassword = turnPass,
                totpCode = j.str("totp_code"),
                totpUri = j.str("totp_uri"),
                deviceInfo = j.str("device_info"),
                publicKey = j.str("public_key"),
                keyFingerprint = j.str("key_fingerprint"),
                msgId = j.str("msg_id"),
                reaction = j.str("reaction"),
                kind = j.str("kind"),
                fileUrl = j.str("file_url"),
                fileName = j.str("file_name"),
                serverId = j.str("server_id"),
                channelId = j.str("channel_id"),
                serverName = j.str("server_name"),
                channelName = j.str("channel_name"),
                token = j.str("token"),
                visibility = j.str("visibility"),
                topic = j.str("topic"),
                inviteCode = j.str("invite_code"),
                rawServers = j.optJSONArray("servers")?.toString(),
                rawChannels = j.optJSONArray("channels")?.toString(),
                rawMessages = j.optJSONArray("messages")?.toString(),
                rawDmHistory = j.optJSONArray("dm_history")?.toString(),
                backupCodes = backupCodes,
            )
        }
    }
}
