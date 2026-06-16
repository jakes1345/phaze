package world.phazechat.app.ui

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.filled.Call
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.font.FontStyle
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
    onVideoCall: (() -> Unit)? = null,
    onAttachFile: (() -> Unit)? = null,
    onVoiceRecord: (() -> Unit)? = null,
    typing: Boolean = false,
    onTyping: () -> Unit = {},
    onBlock: () -> Unit = {},
    onReport: (String, String) -> Unit = { _, _ -> },
    onEdit: (String, String) -> Unit = { _, _ -> },
    onDelete: (String) -> Unit = {},
    onReact: (String, String) -> Unit = { _, _ -> },
) {
    var draft by remember { mutableStateOf("") }
    val listState = rememberLazyListState()
    val scope = rememberCoroutineScope()

    var menuOpen by remember { mutableStateOf(false) }
    var reportOpen by remember { mutableStateOf(false) }
    var blockConfirm by remember { mutableStateOf(false) }
    var editing by remember { mutableStateOf<ChatLine?>(null) }

    LaunchedEffect(messages.size) {
        if (messages.isNotEmpty()) listState.animateScrollToItem(0)
    }

    if (reportOpen) {
        ReportDialog(peer = peer, onDismiss = { reportOpen = false }, onSubmit = { reason, detail ->
            onReport(reason, detail); reportOpen = false
        })
    }
    if (blockConfirm) {
        AlertDialog(
            onDismissRequest = { blockConfirm = false },
            title = { Text("Block $peer?") },
            text = { Text("They won't be able to message you, and this chat will be removed.") },
            confirmButton = { Button(onClick = { onBlock(); blockConfirm = false }) { Text("Block") } },
            dismissButton = { TextButton(onClick = { blockConfirm = false }) { Text("Cancel") } },
        )
    }
    editing?.let { line ->
        EditDialog(initial = line.text, onDismiss = { editing = null }, onSave = { newText ->
            onEdit(line.id, newText); editing = null
        })
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
                        Text(
                            if (typing) "typing…" else peerStatus,
                            fontSize = 12.sp,
                            color = if (typing) PhazeBrandDark else MaterialTheme.colorScheme.onSurfaceVariant,
                            fontStyle = if (typing) FontStyle.Italic else FontStyle.Normal,
                        )
                    }
                },
                actions = {
                    if (onCall != null) {
                        IconButton(onClick = onCall) {
                            Icon(Icons.Default.Call, "Voice call", tint = PhazeBrandDark)
                        }
                    }
                    if (onVideoCall != null) {
                        IconButton(onClick = onVideoCall) {
                            Icon(Icons.Default.PlayArrow, "Video call", tint = PhazeBrandDark)
                        }
                    }
                    IconButton(onClick = { menuOpen = true }) {
                        Icon(Icons.Default.MoreVert, "More")
                    }
                    DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
                        DropdownMenuItem(
                            text = { Text("Report $peer") },
                            onClick = { menuOpen = false; reportOpen = true },
                        )
                        DropdownMenuItem(
                            text = { Text("Block $peer", color = PhazeDanger) },
                            onClick = { menuOpen = false; blockConfirm = true },
                        )
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
                        onValueChange = { draft = it; onTyping() },
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
                                    if (messages.isNotEmpty()) listState.animateScrollToItem(0)
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
                reverseLayout = true,
                modifier = Modifier.fillMaxSize().padding(padding).padding(horizontal = 12.dp),
                verticalArrangement = Arrangement.spacedBy(4.dp),
                contentPadding = PaddingValues(vertical = 8.dp),
            ) {
                items(messages.asReversed(), key = { it.id }) { line ->
                    MessageBubble(
                        line = line,
                        onEdit = { editing = line },
                        onDelete = { onDelete(line.id) },
                        onReact = { emoji -> onReact(line.id, emoji) },
                    )
                }
            }
        }
    }
}

private val REACTIONS = listOf("👍", "❤️", "😂", "😮", "😢", "🙏")

@OptIn(ExperimentalFoundationApi::class)
@Composable
fun MessageBubble(
    line: ChatLine,
    onEdit: () -> Unit = {},
    onDelete: () -> Unit = {},
    onReact: (String) -> Unit = {},
) {
    val align = if (line.me) Arrangement.End else Arrangement.Start
    val bubbleColor = if (line.me) PhazeBrand else MaterialTheme.colorScheme.surfaceVariant
    val textColor = if (line.me) MaterialTheme.colorScheme.onPrimary else MaterialTheme.colorScheme.onSurface
    val uriHandler = LocalUriHandler.current
    var menuOpen by remember { mutableStateOf(false) }

    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = align,
    ) {
        Column(horizontalAlignment = if (line.me) Alignment.End else Alignment.Start) {
            Column(
                modifier = Modifier
                    .widthIn(max = 280.dp)
                    .clip(RoundedCornerShape(16.dp))
                    .background(bubbleColor)
                    .combinedClickable(
                        onClick = {},
                        onLongClick = { if (!line.deleted) menuOpen = true },
                    )
                    .padding(horizontal = 14.dp, vertical = 8.dp),
            ) {
                if (!line.me) {
                    Text(line.from, fontSize = 12.sp, fontWeight = FontWeight.Bold, color = PhazeBrandDark)
                    Spacer(Modifier.height(2.dp))
                }
                if (line.deleted) {
                    Text("🚫 message deleted", color = textColor, fontSize = 14.sp, fontStyle = FontStyle.Italic)
                } else {
                    val phazeFile = remember(line.text) {
                        if (line.text.startsWith("phaze-file{"))
                            try { org.json.JSONObject(line.text.removePrefix("phaze-file")) } catch (_: Exception) { null }
                        else null
                    }
                    if (phazeFile != null) {
                        val name = phazeFile.optString("name", "file")
                        val mime = phazeFile.optString("mime", "")
                        val size = phazeFile.optLong("size", 0L)
                        val url = phazeFile.optString("url", "")
                        val isVoice = mime.contains("audio") || name.endsWith(".ogg") || name.endsWith(".m4a")
                        val sizeLabel = when {
                            size >= 1_048_576 -> "%.1f MB".format(size / 1_048_576.0)
                            size >= 1024 -> "${size / 1024} KB"
                            else -> "$size B"
                        }
                        val openModifier = if (url.isNotEmpty()) Modifier.clickable { uriHandler.openUri(url) } else Modifier
                        if (isVoice) {
                            Row(modifier = openModifier, verticalAlignment = Alignment.CenterVertically) {
                                Text("🎙", fontSize = 20.sp)
                                Spacer(Modifier.width(6.dp))
                                Column {
                                    Text("Voice note", color = textColor, fontSize = 14.sp, fontWeight = FontWeight.Medium)
                                    Text(sizeLabel, color = textColor.copy(alpha = 0.6f), fontSize = 11.sp)
                                }
                            }
                        } else {
                            Row(modifier = openModifier, verticalAlignment = Alignment.CenterVertically) {
                                Text("📎", fontSize = 20.sp)
                                Spacer(Modifier.width(6.dp))
                                Column {
                                    Text(name, color = textColor, fontSize = 14.sp, fontWeight = FontWeight.Medium)
                                    Text(sizeLabel, color = textColor.copy(alpha = 0.6f), fontSize = 11.sp)
                                }
                            }
                        }
                    } else {
                        Text(line.text, color = textColor, fontSize = 15.sp, lineHeight = 20.sp)
                    }
                    if (line.edited) {
                        Text("edited", color = textColor.copy(alpha = 0.6f), fontSize = 10.sp, fontStyle = FontStyle.Italic)
                    }
                }
                if (line.me) {
                    Text(
                        if (line.seen) "✓✓" else "✓",
                        color = textColor.copy(alpha = 0.7f),
                        fontSize = 10.sp,
                        modifier = androidx.compose.ui.Modifier.align(Alignment.End),
                    )
                }
            }
            if (line.reaction != null) {
                Surface(
                    shape = RoundedCornerShape(12.dp),
                    color = MaterialTheme.colorScheme.surfaceVariant,
                    tonalElevation = 2.dp,
                    modifier = Modifier.padding(top = 2.dp),
                ) {
                    Text(line.reaction, fontSize = 13.sp, modifier = Modifier.padding(horizontal = 6.dp, vertical = 2.dp))
                }
            }
            DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
                Row(modifier = Modifier.padding(horizontal = 8.dp)) {
                    REACTIONS.forEach { emoji ->
                        Text(
                            emoji, fontSize = 22.sp,
                            modifier = Modifier
                                .clip(RoundedCornerShape(8.dp))
                                .combinedClickable(onClick = { onReact(emoji); menuOpen = false })
                                .padding(6.dp),
                        )
                    }
                }
                if (line.me) {
                    HorizontalDivider()
                    DropdownMenuItem(text = { Text("Edit") }, onClick = { menuOpen = false; onEdit() })
                    DropdownMenuItem(
                        text = { Text("Delete", color = PhazeDanger) },
                        onClick = { menuOpen = false; onDelete() },
                    )
                }
            }
        }
    }
}

@Composable
private fun EditDialog(initial: String, onDismiss: () -> Unit, onSave: (String) -> Unit) {
    var text by remember { mutableStateOf(initial) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Edit message") },
        text = {
            OutlinedTextField(value = text, onValueChange = { text = it }, modifier = Modifier.fillMaxWidth())
        },
        confirmButton = {
            Button(onClick = { if (text.isNotBlank()) onSave(text.trim()) }, enabled = text.isNotBlank()) { Text("Save") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun ReportDialog(peer: String, onDismiss: () -> Unit, onSubmit: (String, String) -> Unit) {
    val reasons = listOf("Spam", "Harassment", "Inappropriate content", "Impersonation", "Other")
    var reason by remember { mutableStateOf(reasons.first()) }
    var expanded by remember { mutableStateOf(false) }
    var detail by remember { mutableStateOf("") }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Report $peer") },
        text = {
            Column {
                ExposedDropdownMenuBox(expanded = expanded, onExpandedChange = { expanded = it }) {
                    OutlinedTextField(
                        value = reason, onValueChange = {}, readOnly = true,
                        label = { Text("Reason") },
                        trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(expanded = expanded) },
                        modifier = Modifier.menuAnchor().fillMaxWidth(),
                    )
                    ExposedDropdownMenu(expanded = expanded, onDismissRequest = { expanded = false }) {
                        reasons.forEach { r ->
                            DropdownMenuItem(text = { Text(r) }, onClick = { reason = r; expanded = false })
                        }
                    }
                }
                Spacer(Modifier.height(8.dp))
                OutlinedTextField(
                    value = detail, onValueChange = { detail = it },
                    label = { Text("Details (optional)") },
                    modifier = Modifier.fillMaxWidth(), minLines = 2,
                )
            }
        },
        confirmButton = { Button(onClick = { onSubmit(reason, detail.trim()) }) { Text("Submit report") } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}
