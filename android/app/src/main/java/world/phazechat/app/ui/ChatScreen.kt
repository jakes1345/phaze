package world.phazechat.app.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.filled.Call
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import world.phazechat.app.data.ChatLine

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ChatScreen(
    peer: String,
    peerStatus: String,
    messages: List<ChatLine>,
    onBack: () -> Unit,
    onSend: (String) -> Unit,
    onCall: (() -> Unit)? = null,
    onAttachFile: (() -> Unit)? = null,
    onVoiceRecord: (() -> Unit)? = null,
) {
    var draft by remember { mutableStateOf("") }
    val listState = rememberLazyListState()
    val scope = rememberCoroutineScope()

    LaunchedEffect(messages.size) {
        if (messages.isNotEmpty()) listState.animateScrollToItem(messages.size - 1)
    }

    Scaffold(
        topBar = {
            TopAppBar(
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, "Back")
                    }
                },
                title = {
                    Column {
                        Text(peer, fontWeight = FontWeight.Bold, fontSize = 16.sp)
                        Text(peerStatus, fontSize = 12.sp, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                },
                actions = {
                    if (onCall != null) {
                        IconButton(onClick = onCall) {
                            Icon(Icons.Default.Call, "Call", tint = PhazeBrandDark)
                        }
                    }
                },
                colors = TopAppBarDefaults.topAppBarColors(containerColor = MaterialTheme.colorScheme.surface),
            )
        },
        bottomBar = {
            Surface(tonalElevation = 2.dp) {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(8.dp)
                        .imePadding(),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    if (onAttachFile != null) {
                        IconButton(onClick = onAttachFile, modifier = Modifier.size(40.dp)) {
                            Text("📎", fontSize = 18.sp)
                        }
                    }
                    if (onVoiceRecord != null) {
                        IconButton(onClick = onVoiceRecord, modifier = Modifier.size(40.dp)) {
                            Text("🎙", fontSize = 18.sp)
                        }
                    }
                    OutlinedTextField(
                        value = draft,
                        onValueChange = { draft = it },
                        placeholder = { Text("Message...") },
                        modifier = Modifier.weight(1f),
                        singleLine = false,
                        maxLines = 4,
                        shape = RoundedCornerShape(24.dp),
                    )
                    Spacer(Modifier.width(8.dp))
                    FilledIconButton(
                        onClick = {
                            if (draft.isNotBlank()) {
                                onSend(draft.trim())
                                draft = ""
                                scope.launch {
                                    if (messages.isNotEmpty()) listState.animateScrollToItem(messages.size)
                                }
                            }
                        },
                        enabled = draft.isNotBlank(),
                    ) {
                        Icon(Icons.AutoMirrored.Filled.Send, "Send")
                    }
                }
            }
        },
    ) { padding ->
        if (messages.isEmpty()) {
            Box(
                modifier = Modifier.fillMaxSize().padding(padding),
                contentAlignment = Alignment.Center,
            ) {
                Text("No messages yet. Say hi!", color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        } else {
            LazyColumn(
                state = listState,
                modifier = Modifier.fillMaxSize().padding(padding).padding(horizontal = 12.dp),
                verticalArrangement = Arrangement.spacedBy(4.dp),
                contentPadding = PaddingValues(vertical = 8.dp),
            ) {
                items(messages, key = { it.id }) { line ->
                    MessageBubble(line)
                }
            }
        }
    }
}

@Composable
fun MessageBubble(line: ChatLine) {
    val align = if (line.me) Arrangement.End else Arrangement.Start
    val bubbleColor = if (line.me) PhazeBrand else MaterialTheme.colorScheme.surfaceVariant
    val textColor = if (line.me) MaterialTheme.colorScheme.onPrimary else MaterialTheme.colorScheme.onSurface

    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = align,
    ) {
        Column(
            modifier = Modifier
                .widthIn(max = 280.dp)
                .clip(RoundedCornerShape(16.dp))
                .background(bubbleColor)
                .padding(horizontal = 14.dp, vertical = 8.dp),
        ) {
            if (!line.me) {
                Text(line.from, fontSize = 12.sp, fontWeight = FontWeight.Bold, color = PhazeBrandDark)
                Spacer(Modifier.height(2.dp))
            }
            Text(line.text, color = textColor, fontSize = 15.sp, lineHeight = 20.sp)
        }
    }
}
