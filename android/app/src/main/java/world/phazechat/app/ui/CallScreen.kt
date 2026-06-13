package world.phazechat.app.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import kotlinx.coroutines.delay
import org.webrtc.EglBase
import org.webrtc.RendererCommon
import org.webrtc.SurfaceViewRenderer
import org.webrtc.VideoTrack

@Composable
fun CallScreen(
    peer: String,
    isIncoming: Boolean,
    callStatus: String,
    isMuted: Boolean,
    isCameraOn: Boolean,
    isVideo: Boolean = false,
    isScreenSharing: Boolean = false,
    hasRemoteVideo: Boolean = false,
    eglContext: EglBase.Context? = null,
    localVideoTrack: VideoTrack? = null,
    remoteVideoTrack: VideoTrack? = null,
    onAnswer: () -> Unit,
    onReject: () -> Unit,
    onHangUp: () -> Unit,
    onToggleMute: () -> Unit,
    onToggleCamera: () -> Unit,
    onToggleScreenShare: () -> Unit = {},
    onToggleSpeakerphone: () -> Boolean = { false },
) {
    var speakerOn by remember { mutableStateOf(false) }
    var elapsed by remember { mutableIntStateOf(0) }
    val isActive = callStatus == "connected"
    val showVideo = isVideo && eglContext != null

    LaunchedEffect(isActive) {
        if (isActive) {
            while (true) {
                delay(1000)
                elapsed++
            }
        }
    }

    Box(
        modifier = Modifier
            .fillMaxSize()
            .background(Color.Black),
        contentAlignment = Alignment.Center,
    ) {
        // Remote video fills the screen when available.
        if (showVideo && hasRemoteVideo && remoteVideoTrack != null) {
            VideoSurface(
                track = remoteVideoTrack,
                eglContext = eglContext!!,
                mirror = false,
                modifier = Modifier.fillMaxSize(),
            )
        }

        // Foreground content (avatar/status + controls).
        Column(horizontalAlignment = Alignment.CenterHorizontally, modifier = Modifier.fillMaxSize()) {
            Spacer(Modifier.weight(1f))

            // When there's no remote video yet, show the avatar placeholder.
            if (!(showVideo && hasRemoteVideo && remoteVideoTrack != null)) {
                Box(
                    modifier = Modifier
                        .size(120.dp)
                        .clip(CircleShape)
                        .background(PhazeBrand),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(peer.firstOrNull()?.uppercase() ?: "?", fontSize = 48.sp, fontWeight = FontWeight.Bold, color = Color.White)
                }
                Spacer(Modifier.height(16.dp))
                Text(peer, fontSize = 22.sp, fontWeight = FontWeight.Bold, color = Color.White)
                Spacer(Modifier.height(4.dp))

                Text(
                    when {
                        isActive -> "%d:%02d".format(elapsed / 60, elapsed % 60)
                        isIncoming && callStatus == "ringing" -> if (isVideo) "Incoming video call..." else "Incoming call..."
                        callStatus == "ringing" -> "Calling..."
                        callStatus == "connecting" -> "Connecting..."
                        else -> callStatus
                    },
                    fontSize = 14.sp,
                    color = Color.White.copy(alpha = 0.7f),
                )
            }

            Spacer(Modifier.weight(1f))

            // Controls
            if (isIncoming && callStatus == "ringing") {
                Row(
                    horizontalArrangement = Arrangement.spacedBy(32.dp),
                    modifier = Modifier.padding(bottom = 48.dp),
                ) {
                    CallButton(text = "Decline", color = PhazeDanger) { onReject() }
                    CallButton(text = "Answer", color = PhazeSuccess) { onAnswer() }
                }
            } else {
                Row(
                    horizontalArrangement = Arrangement.spacedBy(20.dp),
                    modifier = Modifier.padding(bottom = 48.dp),
                ) {
                    CallButton(text = if (isMuted) "Unmute" else "Mute", color = if (isMuted) PhazeBrandDark else Color.DarkGray) { onToggleMute() }
                    CallButton(text = if (speakerOn) "Earpiece" else "Speaker", color = if (speakerOn) PhazeBrandDark else Color.DarkGray) {
                        speakerOn = onToggleSpeakerphone()
                    }
                    if (isVideo) {
                        CallButton(text = if (isCameraOn) "Cam Off" else "Cam On", color = if (isCameraOn) PhazeBrandDark else Color.DarkGray) { onToggleCamera() }
                        CallButton(text = if (isScreenSharing) "Stop Share" else "Share", color = if (isScreenSharing) PhazeBrand else Color.DarkGray) { onToggleScreenShare() }
                    }
                    CallButton(text = "End", color = PhazeDanger) { onHangUp() }
                }
            }
        }

        // Local self-view picture-in-picture (top-end corner), only during video calls.
        if (showVideo && localVideoTrack != null && isCameraOn) {
            VideoSurface(
                track = localVideoTrack,
                eglContext = eglContext!!,
                mirror = !isScreenSharing,
                modifier = Modifier
                    .align(Alignment.TopEnd)
                    .padding(16.dp)
                    .size(width = 108.dp, height = 152.dp)
                    .clip(RoundedCornerShape(12.dp)),
            )
        }
    }
}

/**
 * Renders a WebRTC [VideoTrack] into a [SurfaceViewRenderer]. Adds the track as a
 * sink and releases the renderer / removes the sink on disposal or track change.
 */
@Composable
private fun VideoSurface(
    track: VideoTrack,
    eglContext: EglBase.Context,
    mirror: Boolean,
    modifier: Modifier = Modifier,
) {
    val renderer = remember { mutableStateOf<SurfaceViewRenderer?>(null) }

    AndroidView(
        modifier = modifier,
        factory = { ctx ->
            SurfaceViewRenderer(ctx).apply {
                init(eglContext, null)
                setEnableHardwareScaler(true)
                setScalingType(RendererCommon.ScalingType.SCALE_ASPECT_FILL)
                setMirror(mirror)
                renderer.value = this
            }
        },
        update = { it.setMirror(mirror) },
    )

    DisposableEffect(track, renderer.value) {
        val r = renderer.value
        if (r != null) {
            try { track.addSink(r) } catch (_: Exception) {}
        }
        onDispose {
            if (r != null) {
                try { track.removeSink(r) } catch (_: Exception) {}
            }
        }
    }

    DisposableEffect(Unit) {
        onDispose { renderer.value?.release() }
    }
}

@Composable
fun CallButton(text: String, color: Color, onClick: () -> Unit) {
    Column(horizontalAlignment = Alignment.CenterHorizontally) {
        FilledIconButton(
            onClick = onClick,
            modifier = Modifier.size(56.dp),
            colors = IconButtonDefaults.filledIconButtonColors(containerColor = color),
        ) {
            Text(
                when (text) {
                    "Mute" -> "🔇"
                    "Unmute" -> "🎤"
                    "Cam Off" -> "📷"
                    "Cam On" -> "📹"
                    "Share" -> "🖥️"
                    "Stop Share" -> "🛑"
                    "End", "Decline" -> "📴"
                    "Answer" -> "📞"
                    else -> "?"
                },
                fontSize = 22.sp,
            )
        }
        Spacer(Modifier.height(4.dp))
        Text(text, fontSize = 11.sp, color = Color.White.copy(alpha = 0.7f))
    }
}
