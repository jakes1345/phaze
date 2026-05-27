package world.phazechat.app.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.filled.Add
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import world.phazechat.app.data.ChannelInfo
import world.phazechat.app.data.ChannelMsg
import world.phazechat.app.data.SpaceInfo

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SpacesScreen(
    spaces: List<SpaceInfo>,
    activeSpace: String?,
    channels: Map<String, List<ChannelInfo>>,
    activeChannel: String?,
    channelMessages: List<ChannelMsg>,
    me: String,
    onSelectSpace: (String) -> Unit,
    onSelectChannel: (String) -> Unit,
    onSendMessage: (String) -> Unit,
    onCreateSpace: (String, String) -> Unit,
    onJoinSpace: (String) -> Unit,
    onBack: () -> Unit,
) {
    var createOpen by remember { mutableStateOf(false) }
    var joinOpen by remember { mutableStateOf(false) }
    var newName by remember { mutableStateOf("") }
    var joinCode by remember { mutableStateOf("") }

    val activeSpaceInfo = spaces.find { it.id == activeSpace }
    val activeChannelInfo = channels[activeSpace]?.find { it.id == activeChannel }

    // Channel message view
    if (activeChannelInfo != null) {
        ChannelChatScreen(
            channel = activeChannelInfo,
            messages = channelMessages,
            me = me,
            onSend = onSendMessage,
            onBack = { onSelectChannel("") },
        )
        return
    }

    // Channel list for active space
    if (activeSpaceInfo != null) {
        val chList = channels[activeSpace] ?: emptyList()
        Scaffold(
            topBar = {
                TopAppBar(
                    navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Back") } },
                    title = { Text(activeSpaceInfo.name, fontWeight = FontWeight.Bold) },
                    colors = TopAppBarDefaults.topAppBarColors(containerColor = MaterialTheme.colorScheme.surface),
                )
            },
        ) { padding ->
            if (chList.isEmpty()) {
                Box(Modifier.fillMaxSize().padding(padding), contentAlignment = Alignment.Center) {
                    Text("No channels yet", color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            } else {
                LazyColumn(modifier = Modifier.padding(padding)) {
                    items(chList, key = { it.id }) { ch ->
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .clickable { onSelectChannel(ch.id) }
                                .padding(horizontal = 16.dp, vertical = 12.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Text(
                                if (ch.kind == "voice") "🎙" else "#",
                                fontSize = 18.sp, color = MaterialTheme.colorScheme.onSurfaceVariant,
                                modifier = Modifier.width(28.dp),
                            )
                            Column {
                                Text(ch.name, fontWeight = FontWeight.SemiBold, fontSize = 15.sp)
                                ch.topic?.let { Text(it, fontSize = 12.sp, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1) }
                            }
                        }
                    }
                }
            }
        }
        return
    }

    // Space list
    Column(modifier = Modifier.fillMaxSize()) {
        Row(
            modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text("Spaces", fontSize = 24.sp, fontWeight = FontWeight.ExtraBold, modifier = Modifier.weight(1f))
            IconButton(onClick = { createOpen = true }) { Icon(Icons.Default.Add, "Create") }
        }

        if (spaces.isEmpty()) {
            Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                Column(horizontalAlignment = Alignment.CenterHorizontally) {
                    Text("No Spaces yet", fontWeight = FontWeight.Bold, fontSize = 16.sp)
                    Spacer(Modifier.height(8.dp))
                    Button(onClick = { createOpen = true }) { Text("Create a Space") }
                    Spacer(Modifier.height(8.dp))
                    OutlinedButton(onClick = { joinOpen = true }) { Text("Join with code") }
                }
            }
        } else {
            LazyColumn {
                items(spaces, key = { it.id }) { space ->
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clickable { onSelectSpace(space.id) }
                            .padding(horizontal = 16.dp, vertical = 10.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Box(
                            modifier = Modifier.size(44.dp).clip(RoundedCornerShape(12.dp)).background(PhazeBrand),
                            contentAlignment = Alignment.Center,
                        ) {
                            Text(space.name.firstOrNull()?.uppercase() ?: "?", color = MaterialTheme.colorScheme.onPrimary, fontWeight = FontWeight.Bold, fontSize = 18.sp)
                        }
                        Spacer(Modifier.width(12.dp))
                        Column {
                            Text(space.name, fontWeight = FontWeight.SemiBold, fontSize = 15.sp)
                            Text(space.visibility, fontSize = 12.sp, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    }
                }
                item {
                    TextButton(onClick = { joinOpen = true }, modifier = Modifier.padding(16.dp)) {
                        Text("Join a Space with invite code")
                    }
                }
            }
        }
    }

    if (createOpen) {
        AlertDialog(
            onDismissRequest = { createOpen = false },
            title = { Text("Create Space") },
            text = {
                OutlinedTextField(value = newName, onValueChange = { newName = it }, label = { Text("Space name") }, singleLine = true, modifier = Modifier.fillMaxWidth())
            },
            confirmButton = {
                Button(onClick = { if (newName.isNotBlank()) { onCreateSpace(newName.trim(), "private"); newName = ""; createOpen = false } }) { Text("Create") }
            },
            dismissButton = { TextButton(onClick = { createOpen = false }) { Text("Cancel") } },
        )
    }

    if (joinOpen) {
        AlertDialog(
            onDismissRequest = { joinOpen = false },
            title = { Text("Join Space") },
            text = {
                OutlinedTextField(value = joinCode, onValueChange = { joinCode = it }, label = { Text("Invite code") }, singleLine = true, modifier = Modifier.fillMaxWidth())
            },
            confirmButton = {
                Button(onClick = { if (joinCode.isNotBlank()) { onJoinSpace(joinCode.trim()); joinCode = ""; joinOpen = false } }) { Text("Join") }
            },
            dismissButton = { TextButton(onClick = { joinOpen = false }) { Text("Cancel") } },
        )
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ChannelChatScreen(
    channel: ChannelInfo,
    messages: List<ChannelMsg>,
    me: String,
    onSend: (String) -> Unit,
    onBack: () -> Unit,
) {
    var draft by remember { mutableStateOf("") }
    val listState = rememberLazyListState()

    LaunchedEffect(messages.size) {
        if (messages.isNotEmpty()) listState.animateScrollToItem(messages.size - 1)
    }

    Scaffold(
        topBar = {
            TopAppBar(
                navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Back") } },
                title = {
                    Column {
                        Text("${if (channel.kind == "voice") "🎙" else "#"} ${channel.name}", fontWeight = FontWeight.Bold, fontSize = 16.sp)
                        channel.topic?.let { Text(it, fontSize = 12.sp, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                    }
                },
                colors = TopAppBarDefaults.topAppBarColors(containerColor = MaterialTheme.colorScheme.surface),
            )
        },
        bottomBar = {
            Surface(tonalElevation = 2.dp) {
                Row(
                    modifier = Modifier.fillMaxWidth().padding(8.dp).imePadding(),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    OutlinedTextField(
                        value = draft, onValueChange = { draft = it },
                        placeholder = { Text("Message #${channel.name}...") },
                        modifier = Modifier.weight(1f), singleLine = false, maxLines = 4,
                        shape = RoundedCornerShape(24.dp),
                    )
                    Spacer(Modifier.width(8.dp))
                    FilledIconButton(onClick = {
                        if (draft.isNotBlank()) { onSend(draft.trim()); draft = "" }
                    }, enabled = draft.isNotBlank()) {
                        Icon(Icons.AutoMirrored.Filled.Send, "Send")
                    }
                }
            }
        },
    ) { padding ->
        LazyColumn(
            state = listState,
            modifier = Modifier.fillMaxSize().padding(padding).padding(horizontal = 12.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp),
            contentPadding = PaddingValues(vertical = 8.dp),
        ) {
            items(messages, key = { it.id }) { m ->
                val isMe = m.sender == me
                Row(modifier = Modifier.fillMaxWidth().padding(vertical = 2.dp)) {
                    if (!isMe) {
                        Box(
                            modifier = Modifier.size(28.dp).clip(CircleShape).background(PhazeBrand),
                            contentAlignment = Alignment.Center,
                        ) {
                            Text(m.sender.firstOrNull()?.uppercase() ?: "?", color = MaterialTheme.colorScheme.onPrimary, fontSize = 12.sp, fontWeight = FontWeight.Bold)
                        }
                        Spacer(Modifier.width(8.dp))
                    }
                    Column(modifier = Modifier.weight(1f)) {
                        if (!isMe) Text(m.sender, fontSize = 12.sp, fontWeight = FontWeight.Bold, color = PhazeBrandDark)
                        Text(
                            if (m.deleted) "[deleted]" else m.body,
                            fontSize = 14.sp,
                            color = if (m.deleted) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onSurface,
                        )
                    }
                }
            }
        }
    }
}
