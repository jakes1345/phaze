package world.phazechat.app.data

import android.util.Log
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import okhttp3.*
import org.json.JSONObject

enum class ConnState { DISCONNECTED, CONNECTING, CONNECTED }

class NexusClient(private val scope: CoroutineScope) {

    companion object {
        private const val TAG = "NexusClient"
        private const val WS_URL = "wss://phazechat.world/ws"
        private const val MAX_RETRY_DELAY = 30_000L
    }

    private val client = OkHttpClient.Builder()
        .pingInterval(java.time.Duration.ofSeconds(30))
        .build()

    private var ws: WebSocket? = null
    private var retryDelay = 1000L

    private val _state = MutableStateFlow(ConnState.DISCONNECTED)
    val state = _state.asStateFlow()

    private val _messages = MutableSharedFlow<NexusMessage>(extraBufferCapacity = 64)
    val messages = _messages.asSharedFlow()

    fun connect() {
        if (_state.value == ConnState.CONNECTING) return
        _state.value = ConnState.CONNECTING

        val req = Request.Builder().url(WS_URL).build()
        ws = client.newWebSocket(req, object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) {
                Log.d(TAG, "Connected to Nexus")
                _state.value = ConnState.CONNECTED
                retryDelay = 1000L
            }

            override fun onMessage(webSocket: WebSocket, text: String) {
                try {
                    val msg = NexusMessage.fromJson(JSONObject(text))
                    scope.launch { _messages.emit(msg) }
                } catch (e: Exception) {
                    Log.w(TAG, "Parse error: ${e.message}")
                }
            }

            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                Log.d(TAG, "WS closed: $code $reason")
                _state.value = ConnState.DISCONNECTED
                scheduleReconnect()
            }

            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                Log.w(TAG, "WS failure: ${t.message}")
                _state.value = ConnState.DISCONNECTED
                scheduleReconnect()
            }
        })
    }

    fun send(msg: NexusMessage) {
        ws?.send(msg.toJson().toString())
    }

    fun disconnect() {
        ws?.close(1000, "bye")
        ws = null
        _state.value = ConnState.DISCONNECTED
    }

    private fun scheduleReconnect() {
        scope.launch {
            delay(retryDelay)
            retryDelay = (retryDelay * 2).coerceAtMost(MAX_RETRY_DELAY)
            connect()
        }
    }
}
