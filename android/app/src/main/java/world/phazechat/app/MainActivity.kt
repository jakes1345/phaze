package world.phazechat.app

import android.Manifest
import android.media.MediaRecorder
import android.net.Uri
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.*
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Email
import androidx.compose.material.icons.filled.Menu
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.viewmodel.compose.viewModel
import world.phazechat.app.data.PhazeViewModel
import world.phazechat.app.ui.*
import java.io.File

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            PhazeTheme(darkTheme = true) {
                Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
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
    val spaces by vm.spaces.collectAsState()
    val activeSpace by vm.activeSpace.collectAsState()
    val channels by vm.channels.collectAsState()
    val activeChannel by vm.activeChannel.collectAsState()
    val channelMessages by vm.channelMessages.collectAsState()
    val callState by vm.callState.collectAsState()
    val stories by vm.stories.collectAsState()

    var viewingStoryAuthor by remember { mutableStateOf<String?>(null) }

    val storyPicker = rememberLauncherForActivityResult(ActivityResultContracts.GetContent()) { uri: Uri? ->
        uri?.let { vm.postStory(it) }
    }

    // Load stories on login
    LaunchedEffect(me) { if (me != null) vm.loadStories() }

    val context = LocalContext.current

    // File picker
    val filePicker = rememberLauncherForActivityResult(ActivityResultContracts.GetContent()) { uri: Uri? ->
        uri?.let { vm.sendFile(it) }
    }

    // Voice recording state
    var recording by remember { mutableStateOf(false) }
    var recorder by remember { mutableStateOf<MediaRecorder?>(null) }
    var voicePath by remember { mutableStateOf<String?>(null) }

    val micPermission = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        if (granted && !recording) {
            val path = File(context.cacheDir, "phaze_voice_${System.currentTimeMillis()}.ogg").absolutePath
            voicePath = path
            val mr = MediaRecorder(context).apply {
                setAudioSource(MediaRecorder.AudioSource.MIC)
                setOutputFormat(MediaRecorder.OutputFormat.OGG)
                setAudioEncoder(MediaRecorder.AudioEncoder.OPUS)
                setAudioSamplingRate(16000)
                setOutputFile(path)
                prepare()
                start()
            }
            recorder = mr
            recording = true
        }
    }

    fun startVoiceRecord() {
        micPermission.launch(Manifest.permission.RECORD_AUDIO)
    }

    fun stopVoiceRecord(send: Boolean) {
        recorder?.let {
            try { it.stop() } catch (_: Exception) {}
            it.release()
        }
        recorder = null
        recording = false
        if (send && voicePath != null) {
            vm.sendVoiceMessage(voicePath!!)
        } else {
            voicePath?.let { File(it).delete() }
        }
        voicePath = null
    }

    // Story viewer overlay
    if (viewingStoryAuthor != null) {
        val authorStories = stories.filter { it.author == viewingStoryAuthor }
        if (authorStories.isNotEmpty()) {
            StoryViewer(stories = authorStories, onClose = { viewingStoryAuthor = null })
            return
        } else {
            viewingStoryAuthor = null
        }
    }

    // Call overlay
    if (callState != null) {
        val cs = callState!!
        CallScreen(
            peer = cs.peer, isIncoming = cs.isIncoming, callStatus = cs.status,
            isMuted = cs.isMuted, isCameraOn = cs.isCameraOn,
            onAnswer = { vm.answerCall() }, onReject = { vm.rejectCall() },
            onHangUp = { vm.endCall() }, onToggleMute = { vm.toggleCallMute() },
            onToggleCamera = { vm.toggleCallCamera() },
        )
        return
    }

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

        if (recording) {
            // Show recording overlay instead of normal chat bottom bar
            ChatScreen(
                peer = peer, peerStatus = info?.status ?: "Unknown", messages = chatLog,
                onBack = { stopVoiceRecord(false); vm.selectChat("") },
                onSend = { vm.sendMessage(it) },
                onCall = { vm.startCall(peer) },
            )
            // TODO: overlay recording UI — for now the send/cancel is on stop
            AlertDialog(
                onDismissRequest = { stopVoiceRecord(false) },
                title = { Text("Recording voice message...") },
                confirmButton = { Button(onClick = { stopVoiceRecord(true) }) { Text("Send") } },
                dismissButton = { TextButton(onClick = { stopVoiceRecord(false) }) { Text("Cancel") } },
            )
        } else {
            ChatScreen(
                peer = peer, peerStatus = info?.status ?: "Unknown", messages = chatLog,
                onBack = { vm.selectChat("") },
                onSend = { vm.sendMessage(it) },
                onCall = { vm.startCall(peer) },
                onAttachFile = { filePicker.launch("*/*") },
                onVoiceRecord = { startVoiceRecord() },
            )
        }
        return
    }

    var tab by remember { mutableIntStateOf(0) }

    Scaffold(
        bottomBar = {
            NavigationBar {
                NavigationBarItem(
                    selected = tab == 0, onClick = { tab = 0 },
                    icon = { Icon(Icons.Default.Email, "Chats") }, label = { Text("Chats") },
                )
                NavigationBarItem(
                    selected = tab == 1, onClick = { tab = 1; vm.loadSpaces() },
                    icon = { Icon(Icons.Default.Menu, "Spaces") }, label = { Text("Spaces") },
                )
                NavigationBarItem(
                    selected = tab == 2, onClick = { tab = 2 },
                    icon = { Icon(Icons.Default.Settings, "Settings") }, label = { Text("Settings") },
                )
            }
        },
    ) { padding ->
        Box(modifier = Modifier.padding(padding)) {
            when (tab) {
                0 -> ChatsScreen(
                    friends = friends, pending = pending, unread = unread,
                    stories = stories, me = me!!,
                    onSelectChat = { vm.selectChat(it) },
                    onAddFriend = { vm.sendFriendRequest(it) },
                    onAcceptFriend = { vm.acceptFriend(it) },
                    onViewStory = { viewingStoryAuthor = it },
                    onAddStory = { storyPicker.launch("image/*") },
                )
                1 -> SpacesScreen(
                    spaces = spaces, activeSpace = activeSpace, channels = channels,
                    activeChannel = activeChannel, channelMessages = channelMessages, me = me!!,
                    onSelectSpace = { vm.selectSpace(it) },
                    onSelectChannel = { vm.selectChannel(it) },
                    onSendMessage = { vm.sendChannelMessage(it) },
                    onCreateSpace = { name, vis -> vm.createSpace(name, vis) },
                    onJoinSpace = { vm.joinSpace(it) },
                    onBack = { vm.selectSpace("") },
                )
                2 -> SettingsScreen(
                    me = me!!,
                    onUpdateProfile = { name, mood -> vm.updateProfile(name, mood) },
                    onEnable2FA = { vm.enable2FA() },
                    onDisable2FA = { vm.disable2FA() },
                    onSignOut = { vm.signOut() },
                )
            }
        }
    }
}
