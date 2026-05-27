package world.phazechat.app.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import world.phazechat.app.data.FriendInfo

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ChatsScreen(
    friends: Map<String, FriendInfo>,
    pending: List<String>,
    unread: Map<String, Int>,
    stories: List<Story> = emptyList(),
    me: String = "",
    onSelectChat: (String) -> Unit,
    onAddFriend: (String) -> Unit,
    onAcceptFriend: (String) -> Unit,
    onViewStory: (String) -> Unit = {},
    onAddStory: () -> Unit = {},
) {
    var addDialogOpen by remember { mutableStateOf(false) }
    var addName by remember { mutableStateOf("") }

    Column(modifier = Modifier.fillMaxSize()) {
        // Header
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 16.dp, vertical = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text("Chats", fontSize = 24.sp, fontWeight = FontWeight.ExtraBold, modifier = Modifier.weight(1f))
            IconButton(onClick = { addDialogOpen = true }) {
                Icon(Icons.Default.Add, contentDescription = "Add friend")
            }
        }

        // Stories
        if (stories.isNotEmpty() || me.isNotEmpty()) {
            StoriesRow(stories = stories, me = me, onViewStory = onViewStory, onAddStory = onAddStory)
            Spacer(Modifier.height(8.dp))
            HorizontalDivider()
        }

        // Pending requests
        if (pending.isNotEmpty()) {
            pending.forEach { from ->
                Card(
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
                    colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant),
                ) {
                    Row(
                        modifier = Modifier.padding(12.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Avatar(from, 36)
                        Spacer(Modifier.width(12.dp))
                        Text("$from wants to be friends", modifier = Modifier.weight(1f), fontSize = 14.sp)
                        Button(onClick = { onAcceptFriend(from) }, contentPadding = PaddingValues(horizontal = 12.dp, vertical = 4.dp)) {
                            Text("Accept", fontSize = 13.sp)
                        }
                    }
                }
            }
            Spacer(Modifier.height(8.dp))
        }

        // Friends list
        val sorted = friends.values.sortedWith(compareByDescending<FriendInfo> { it.status == "Online" }.thenBy { it.username })
        if (sorted.isEmpty()) {
            Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                Column(horizontalAlignment = Alignment.CenterHorizontally) {
                    Text("No friends yet", fontWeight = FontWeight.Bold, fontSize = 16.sp)
                    Spacer(Modifier.height(4.dp))
                    Text("Add someone by their username", color = MaterialTheme.colorScheme.onSurfaceVariant, fontSize = 14.sp)
                }
            }
        } else {
            LazyColumn {
                items(sorted, key = { it.username }) { friend ->
                    val count = unread[friend.username] ?: 0
                    FriendRow(friend, count) { onSelectChat(friend.username) }
                }
            }
        }
    }

    if (addDialogOpen) {
        AlertDialog(
            onDismissRequest = { addDialogOpen = false },
            title = { Text("Add Friend") },
            text = {
                OutlinedTextField(
                    value = addName,
                    onValueChange = { addName = it },
                    label = { Text("Username") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
            },
            confirmButton = {
                Button(onClick = {
                    if (addName.isNotBlank()) { onAddFriend(addName.trim()); addName = ""; addDialogOpen = false }
                }) { Text("Send Request") }
            },
            dismissButton = {
                TextButton(onClick = { addDialogOpen = false }) { Text("Cancel") }
            },
        )
    }
}

@Composable
fun FriendRow(friend: FriendInfo, unreadCount: Int, onClick: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Avatar(friend.username, 48, friend.status)
        Spacer(Modifier.width(12.dp))
        Column(modifier = Modifier.weight(1f)) {
            Text(friend.username, fontWeight = FontWeight.SemiBold, fontSize = 15.sp)
            Text(
                friend.mood ?: friend.status,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                fontSize = 13.sp,
                maxLines = 1,
            )
        }
        if (unreadCount > 0) {
            Badge(containerColor = PhazeBrandDark) {
                Text("$unreadCount", color = MaterialTheme.colorScheme.onPrimary, fontSize = 11.sp)
            }
        }
    }
}

@Composable
fun Avatar(name: String, size: Int, status: String? = null) {
    Box(contentAlignment = Alignment.BottomEnd) {
        Box(
            modifier = Modifier
                .size(size.dp)
                .clip(CircleShape)
                .background(PhazeBrand),
            contentAlignment = Alignment.Center,
        ) {
            Text(
                name.firstOrNull()?.uppercase() ?: "?",
                color = MaterialTheme.colorScheme.onPrimary,
                fontWeight = FontWeight.Bold,
                fontSize = (size / 2.5).sp,
            )
        }
        if (status == "Online") {
            Box(
                modifier = Modifier
                    .size((size / 4).dp)
                    .clip(CircleShape)
                    .background(PhazeSuccess)
            )
        }
    }
}
