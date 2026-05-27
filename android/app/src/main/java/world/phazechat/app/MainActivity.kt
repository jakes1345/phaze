package world.phazechat.app

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.layout.*
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Email
import androidx.compose.material.icons.filled.Menu
import androidx.compose.material.icons.filled.Settings
import androidx.compose.ui.unit.dp
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.lifecycle.viewmodel.compose.viewModel
import world.phazechat.app.data.PhazeViewModel
import world.phazechat.app.ui.*

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            PhazeTheme(darkTheme = true) {
                Surface(
                    modifier = Modifier.fillMaxSize(),
                    color = MaterialTheme.colorScheme.background,
                ) {
                    PhazeRoot()
                }
            }
        }
    }
}

@Composable
fun PhazeRoot(vm: PhazeViewModel = viewModel()) {
    val me by vm.me.collectAsState()
    val authError by vm.authError.collectAsState()
    val friends by vm.friends.collectAsState()
    val pending by vm.pending.collectAsState()
    val unread by vm.unread.collectAsState()
    val selectedChat by vm.selectedChat.collectAsState()
    val chatLog by vm.chatLog.collectAsState()

    if (me == null) {
        AuthScreen(
            error = authError,
            onLogin = { u, p -> vm.login(u, p) },
            onRegister = { u, e, p -> vm.register(u, e, p) },
        )
        return
    }

    if (selectedChat != null) {
        val peer = selectedChat!!
        val info = friends[peer]
        ChatScreen(
            peer = peer,
            peerStatus = info?.status ?: "Unknown",
            messages = chatLog,
            onBack = { vm.selectChat("") },
            onSend = { vm.sendMessage(it) },
        )
        return
    }

    var tab by remember { mutableIntStateOf(0) }

    Scaffold(
        bottomBar = {
            NavigationBar {
                NavigationBarItem(
                    selected = tab == 0,
                    onClick = { tab = 0 },
                    icon = { Icon(Icons.Default.Email, "Chats") },
                    label = { Text("Chats") },
                )
                NavigationBarItem(
                    selected = tab == 1,
                    onClick = { tab = 1 },
                    icon = { Icon(Icons.Default.Menu, "Spaces") },
                    label = { Text("Spaces") },
                )
                NavigationBarItem(
                    selected = tab == 2,
                    onClick = { tab = 2 },
                    icon = { Icon(Icons.Default.Settings, "Settings") },
                    label = { Text("Settings") },
                )
            }
        },
    ) { padding ->
        Box(modifier = Modifier.padding(padding)) {
            when (tab) {
                0 -> ChatsScreen(
                    friends = friends,
                    pending = pending,
                    unread = unread,
                    onSelectChat = { vm.selectChat(it) },
                    onAddFriend = { vm.sendFriendRequest(it) },
                    onAcceptFriend = { vm.acceptFriend(it) },
                )
                1 -> Box(Modifier.fillMaxSize()) {
                    Text(
                        "Spaces coming soon",
                        modifier = Modifier.padding(24.dp),
                        style = MaterialTheme.typography.titleMedium,
                    )
                }
                2 -> SettingsScreen(me = me!!, onSignOut = { vm.signOut() })
            }
        }
    }
}
