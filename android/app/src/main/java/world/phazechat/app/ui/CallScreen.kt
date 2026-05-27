package world.phazechat.app.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.delay

@Composable
fun CallScreen(
    peer: String,
    isIncoming: Boolean,
    callStatus: String,
    isMuted: Boolean,
    isCameraOn: Boolean,
    onAnswer: () -> Unit,
    onReject: () -> Unit,
    onHangUp: () -> Unit,
    onToggleMute: () -> Unit,
    onToggleCamera: () -> Unit,
) {
    var elapsed by remember { mutableIntStateOf(0) }
    val isActive = callStatus == "connected"

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
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Spacer(Modifier.weight(1f))

            // Avatar
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
                    isIncoming && callStatus == "ringing" -> "Incoming call..."
                    callStatus == "ringing" -> "Calling..."
                    callStatus == "connecting" -> "Connecting..."
                    else -> callStatus
                },
                fontSize = 14.sp,
                color = Color.White.copy(alpha = 0.7f),
            )

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
                    horizontalArrangement = Arrangement.spacedBy(24.dp),
                    modifier = Modifier.padding(bottom = 48.dp),
                ) {
                    CallButton(text = if (isMuted) "Unmute" else "Mute", color = if (isMuted) PhazeBrandDark else Color.DarkGray) { onToggleMute() }
                    CallButton(text = if (isCameraOn) "Cam Off" else "Cam On", color = if (isCameraOn) PhazeBrandDark else Color.DarkGray) { onToggleCamera() }
                    CallButton(text = "End", color = PhazeDanger) { onHangUp() }
                }
            }
        }
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
