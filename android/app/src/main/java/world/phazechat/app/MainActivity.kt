package world.phazechat.app

import android.Manifest
import android.app.Activity
import world.phazechat.app.data.ConnState
import android.content.Context
import android.content.Intent
import android.media.MediaRecorder
import android.media.projection.MediaProjectionManager
import android.os.Build
import android.net.Uri
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Email
import androidx.compose.material.icons.filled.Menu
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewmodel.compose.viewModel
import world.phazechat.app.data.PhazeViewModel
import world.phazechat.app.ui.*
import java.io.File
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

class MainActivity : ComponentActivity() {

    private val vm: PhazeViewModel by lazy {
        ViewModelProvider(this)[PhazeViewModel::class.java]
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        intent?.getStringExtra("open_chat")?.takeIf { it.isNotBlank() }?.let { peer ->
            vm.openChat(peer)
        }
        enableEdgeToEdge()
        setContent {
            val vm: PhazeViewModel = viewModel()  // same instance as this.vm
            val theme by vm.theme.collectAsState()
            val snow by vm.snow.collectAsState()
            PhazeTheme(theme = theme) {
                Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
                    Box(Modifier.fillMaxSize()) {
                        PhazeRoot(vm)
                        if (snow) SnowOverlay()
                    }
                }
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        intent.getStringExtra("open_chat")?.takeIf { it.isNotBlank() }?.let { peer ->
            vm.openChat(peer)
        }
    }
}

@Composable
fun PhazeRoot(vm: PhazeViewModel = viewModel()) {
    val me by vm.me.collectAsState()
    val authError by vm.authError.collectAsState()
    val pendingVerification by vm.pendingVerification.collectAsState()
    val pendingVerifyUsername by vm.pendingVerifyUsername.collectAsState()
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
    val typingFrom by vm.typingFrom.collectAsState()
    val searchResults by vm.searchResults.collectAsState()
    val actionStatus by vm.actionStatus.collectAsState()
    val globalNotice by vm.globalNotice.collectAsState()
    val connState by vm.connState.collectAsState()

    val pendingOpenChat by vm.pendingOpenChat.collectAsState()

    // Consume deep-link navigation (e.g. from push notification tap)
    LaunchedEffect(pendingOpenChat, me) {
        val peer = pendingOpenChat
        if (peer != null && me != null) {
            vm.consumePendingOpenChat()
            vm.selectChat(peer)
        }
    }

    val toastCtx = LocalContext.current
    LaunchedEffect(actionStatus) {
        actionStatus?.let {
            android.widget.Toast.makeText(toastCtx, it, android.widget.Toast.LENGTH_SHORT).show()
            vm.clearActionStatus()
        }
    }

    // Admin global notice — overlays every screen (placed before the early
    // returns below so it shows during calls, chats, stories, etc.).
    globalNotice?.let { gn ->
        AlertDialog(
            onDismissRequest = { vm.clearGlobalNotice() },
            title = { Text("📢 Phaze Announcement") },
            text = { Text(gn.message) },
            confirmButton = { Button(onClick = { vm.clearGlobalNotice() }) { Text("Got it") } },
        )
    }

    var viewingStoryAuthor by remember { mutableStateOf<String?>(null) }

    val storyPicker = rememberLauncherForActivityResult(ActivityResultContracts.GetContent()) { uri: Uri? ->
        uri?.let { vm.postStory(it) }
    }

    // Load stories on login
    LaunchedEffect(me) { if (me != null) vm.loadStories() }

    val context = LocalContext.current

    // Request POST_NOTIFICATIONS on Android 13+ once the user is logged in
    val notifPermission = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) {}
    LaunchedEffect(me) {
        if (me != null && Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            if (androidx.core.content.ContextCompat.checkSelfPermission(
                    context, Manifest.permission.POST_NOTIFICATIONS
                ) != android.content.pm.PackageManager.PERMISSION_GRANTED
            ) {
                notifPermission.launch(Manifest.permission.POST_NOTIFICATIONS)
            }
        }
    }

    var scannedLinkCode by remember { mutableStateOf("") }

    val scannerLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) { result ->
        if (result.resultCode == android.app.Activity.RESULT_OK) {
            val code = result.data?.getStringExtra("scanned_code")
            if (!code.isNullOrBlank()) {
                var tok = code.trim()
                if (tok.contains("token=")) {
                    tok = tok.substringAfter("token=").substringBefore("&")
                }
                scannedLinkCode = tok
                vm.loginWithLinkCode(tok)
            }
        }
    }

    val coroutineScope = androidx.compose.runtime.rememberCoroutineScope()
    val galleryLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.GetContent()
    ) { uri: Uri? ->
        uri?.let {
            // Bitmap decode + ZXing must run off the main thread to avoid ANR
            coroutineScope.launch {
                val code = withContext(Dispatchers.IO) {
                    try {
                        val inputStream = context.contentResolver.openInputStream(it)
                        val bitmap = android.graphics.BitmapFactory.decodeStream(inputStream)
                        inputStream?.close()
                        if (bitmap != null) {
                            val width = bitmap.width
                            val height = bitmap.height
                            val pixels = IntArray(width * height)
                            bitmap.getPixels(pixels, 0, width, 0, 0, width, height)
                            val source = com.google.zxing.RGBLuminanceSource(width, height, pixels)
                            val binaryBitmap = com.google.zxing.BinaryBitmap(com.google.zxing.common.HybridBinarizer(source))
                            com.google.zxing.MultiFormatReader().decode(binaryBitmap).text
                        } else null
                    } catch (e: Exception) {
                        null
                    }
                }
                if (!code.isNullOrBlank()) {
                    var tok = code.trim()
                    if (tok.contains("token=")) {
                        tok = tok.substringAfter("token=").substringBefore("&")
                    }
                    scannedLinkCode = tok
                    vm.loginWithLinkCode(tok)
                } else {
                    android.widget.Toast.makeText(context, "No QR code found in selected image", android.widget.Toast.LENGTH_SHORT).show()
                }
            }
        }
    }

    // File picker
    val filePicker = rememberLauncherForActivityResult(ActivityResultContracts.GetContent()) { uri: Uri? ->
        uri?.let { vm.sendFile(it) }
    }

    // Screen-share (MediaProjection) permission flow for video calls.
    val mediaProjectionManager = remember {
        context.getSystemService(Context.MEDIA_PROJECTION_SERVICE) as MediaProjectionManager
    }
    val screenShareLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) { result ->
        val data = result.data
        if (result.resultCode == Activity.RESULT_OK && data != null) {
            // Start the mediaProjection foreground service FIRST, then begin
            // capture only once it's actually foregrounded — Android 14+ throws
            // SecurityException if MediaProjection.start() runs before that.
            ScreenShareService.start(context) {
                vm.startScreenShare(data)
            }
        }
    }

    // Voice recording state
    var recording by remember { mutableStateOf(false) }
    var recorder by remember { mutableStateOf<MediaRecorder?>(null) }
    var voicePath by remember { mutableStateOf<String?>(null) }

    val micPermission = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        if (granted && !recording) {
            // OGG/OPUS only exist on API 29+. On 26–28 fall back to MPEG-4/AAC
            // (.m4a). The MediaRecorder(Context) constructor is API 31+, so use
            // the deprecated no-arg constructor below that.
            val useOgg = Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q
            val ext = if (useOgg) "ogg" else "m4a"
            val path = File(context.cacheDir, "phaze_voice_${System.currentTimeMillis()}.$ext").absolutePath
            voicePath = path
            @Suppress("DEPRECATION")
            val mr = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) MediaRecorder(context) else MediaRecorder()
            try {
                mr.setAudioSource(MediaRecorder.AudioSource.MIC)
                if (useOgg) {
                    mr.setOutputFormat(MediaRecorder.OutputFormat.OGG)
                    mr.setAudioEncoder(MediaRecorder.AudioEncoder.OPUS)
                } else {
                    mr.setOutputFormat(MediaRecorder.OutputFormat.MPEG_4)
                    mr.setAudioEncoder(MediaRecorder.AudioEncoder.AAC)
                }
                mr.setAudioSamplingRate(16000)
                mr.setOutputFile(path)
                mr.prepare()
                mr.start()
                recorder = mr
                recording = true
            } catch (e: Exception) {
                mr.release()
                voicePath = null
                android.widget.Toast.makeText(context, "Couldn't start recording: ${e.message}", android.widget.Toast.LENGTH_SHORT).show()
            }
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
        val cm = vm.callManager
        CallScreen(
            peer = cs.peer, isIncoming = cs.isIncoming, callStatus = cs.status,
            isMuted = cs.isMuted, isCameraOn = cs.isCameraOn,
            isVideo = cs.isVideo, isScreenSharing = cs.isScreenSharing,
            hasRemoteVideo = cs.hasRemoteVideo,
            eglContext = cm?.eglContext,
            localVideoTrack = cm?.localVideoTrack,
            remoteVideoTrack = cm?.remoteVideoTrack,
            onAnswer = { vm.answerCall() }, onReject = { vm.rejectCall() },
            onHangUp = { ScreenShareService.stop(context); vm.endCall() },
            onToggleMute = { vm.toggleCallMute() },
            onToggleCamera = { vm.toggleCallCamera() },
            onToggleSpeakerphone = { vm.toggleSpeakerphone() },
            onToggleScreenShare = {
                if (cs.isScreenSharing) {
                    vm.stopScreenShare()
                    ScreenShareService.stop(context)
                } else {
                    screenShareLauncher.launch(mediaProjectionManager.createScreenCaptureIntent())
                }
            },
        )
        return
    }

    if (me == null) {
        AuthScreen(
            error = authError,
            onLogin = { u, p -> vm.login(u, p) },
            onRegister = { u, e, p -> vm.register(u, e, p) },
            onVerifyEmail = { code -> vm.verifyEmail(code) },
            onResendVerification = { email -> vm.resendVerification(email) },
            onCancelVerification = { vm.cancelVerification() },
            pendingVerification = pendingVerification,
            onLoginWithLinkCode = { vm.loginWithLinkCode(it) },
            onCancelLinkLogin = { vm.cancelLinkLogin() },
            scannedLinkCode = scannedLinkCode,
            onScanQR = {
                val intent = android.content.Intent(context, QRScannerActivity::class.java)
                scannerLauncher.launch(intent)
            },
            onScanGallery = {
                galleryLauncher.launch("image/*")
            }
        )
        return
    }

    if (selectedChat != null) {
        val peer = selectedChat!!
        val info = friends[peer]

        val isConnected = connState == ConnState.CONNECTED
        if (recording) {
            // Show recording overlay instead of normal chat bottom bar
            ChatScreen(
                peer = peer, peerStatus = info?.status ?: "Unknown", messages = chatLog,
                onBack = { stopVoiceRecord(false); vm.selectChat("") },
                onSend = { vm.sendMessage(it) },
                onCall = { vm.startCall(peer) },
                canSend = isConnected,
            )
            VoiceRecordingOverlay(
                onSend = { stopVoiceRecord(true) },
                onCancel = { stopVoiceRecord(false) },
            )
        } else {
            ChatScreen(
                peer = peer, peerStatus = info?.status ?: "Unknown", messages = chatLog,
                onBack = { vm.selectChat("") },
                onSend = { vm.sendMessage(it) },
                onCall = { vm.startCall(peer) },
                onVideoCall = { vm.startCall(peer, withVideo = true) },
                onAttachFile = { filePicker.launch("*/*") },
                onVoiceRecord = { startVoiceRecord() },
                typing = typingFrom == peer,
                onTyping = { vm.sendTyping() },
                onBlock = { vm.blockUser(peer) },
                onReport = { reason, detail -> vm.reportUser(peer, reason, detail) },
                onEdit = { id, text -> vm.editMessage(id, text) },
                onDelete = { id -> vm.deleteMessage(id) },
                onReact = { id, emoji -> vm.reactMessage(id, emoji) },
                canSend = isConnected,
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
                    searchResults = searchResults,
                    onSearch = { vm.searchUsers(it) },
                    onClearSearch = { vm.clearSearch() },
                )
                1 -> {
                    val discoverList by vm.discoverSpaces.collectAsState()
                    SpacesScreen(
                        spaces = spaces, activeSpace = activeSpace, channels = channels,
                        activeChannel = activeChannel, channelMessages = channelMessages, me = me!!,
                        onSelectSpace = { vm.selectSpace(it) },
                        onSelectChannel = { vm.selectChannel(it) },
                        onSendMessage = { vm.sendChannelMessage(it) },
                        onCreateSpace = { name, vis -> vm.createSpace(name, vis) },
                        onJoinSpace = { vm.joinSpace(it) },
                        onBack = { vm.selectSpace("") },
                        onCreateChannel = { sid, name, kind -> vm.createChannel(sid, name, kind) },
                        discoverList = discoverList,
                        onDiscover = { vm.discoverSpaces() },
                        onJoinPublic = { vm.joinPublicSpace(it) },
                    )
                }
                2 -> {
                    val linkCode by vm.activeLinkCode.collectAsState()
                    val linkStatus by vm.linkStatus.collectAsState()
                    val linkError by vm.linkError.collectAsState()
                    val keyBackupStatus by vm.keyBackupStatus.collectAsState()
                    val keyBackupError by vm.keyBackupError.collectAsState()
                    val theme by vm.theme.collectAsState()
                    val snowPref by vm.snow.collectAsState()
                    val twoFactorUri by vm.twoFactorUri.collectAsState()
                    val twoFactorStatus by vm.twoFactorStatus.collectAsState()
                    val twoFactorBackupCodes by vm.twoFactorBackupCodes.collectAsState()
                    val myDisplayName by vm.myDisplayName.collectAsState()
                    SettingsScreen(
                        me = me!!,
                        mood = friends[me]?.mood ?: "",
                        displayName = myDisplayName,
                        onUpdateProfile = { name, mood -> vm.updateProfile(name, mood) },
                        onEnable2FA = { vm.enable2FA() },
                        onConfirm2FA = { code -> vm.confirm2FA(code) },
                        onDisable2FA = { pw -> vm.disable2FA(pw) },
                        onCancel2FA = { vm.cancel2FAEnrollment() },
                        twoFactorUri = twoFactorUri,
                        twoFactorStatus = twoFactorStatus,
                        twoFactorBackupCodes = twoFactorBackupCodes,
                        onDismissBackupCodes = { vm.clearBackupCodes() },
                        theme = theme,
                        onSetTheme = { vm.setTheme(it) },
                        snow = snowPref,
                        onSetSnow = { vm.setSnow(it) },
                        onSignOut = { vm.signOut() },
                        linkCode = linkCode,
                        linkStatus = linkStatus,
                        linkError = linkError,
                        onGenerateLinkCode = { vm.generateLinkCode() },
                        onApproveDevice = { vm.approveDevice(it) },
                        onClearLinkStatus = { vm.clearLinkStatus() },
                        keyBackupStatus = keyBackupStatus,
                        keyBackupError = keyBackupError,
                        onBackupKeys = { pin -> vm.backupKeys(pin) },
                        onRestoreKeys = { pin -> vm.restoreKeys(pin) },
                        onClearKeyBackupStatus = { vm.clearKeyBackupStatus() },
                        onDeleteAccount = { pw -> vm.deleteAccount(pw) },
                        skypeImportBusy = vm.skypeImportBusy.collectAsState().value,
                        skypeImportStatus = vm.skypeImportStatus.collectAsState().value,
                        skypeContacts = vm.skypeContacts.collectAsState().value,
                        onImportSkype = { uri -> vm.importSkype(uri) },
                        onLoadSkypeContacts = { vm.loadSkypeContacts() },
                        onAddFriendFromSkype = { username -> vm.sendFriendRequest(username) },
                        onClearSkypeStatus = { vm.clearSkypeImportStatus() },
                        referralCount = vm.referralCount.collectAsState().value,
                        referredUsers = vm.referredUsers.collectAsState().value,
                        onGetReferralStats = { vm.getReferralStats() },
                    )
                }
            }
        }
    }
}

/** Bottom overlay shown while a voice message is recording: pulsing dot, timer, Cancel/Send. */
@Composable
fun VoiceRecordingOverlay(onSend: () -> Unit, onCancel: () -> Unit) {
    var elapsed by remember { mutableIntStateOf(0) }
    LaunchedEffect(Unit) {
        while (true) {
            kotlinx.coroutines.delay(1000)
            elapsed++
        }
    }
    val pulse = rememberInfiniteTransition(label = "rec")
    val dotAlpha by pulse.animateFloat(
        initialValue = 1f, targetValue = 0.25f,
        animationSpec = infiniteRepeatable(tween(700), RepeatMode.Reverse), label = "dot",
    )

    Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.BottomCenter) {
        Surface(
            tonalElevation = 6.dp,
            color = MaterialTheme.colorScheme.surface,
            modifier = Modifier.fillMaxWidth(),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 14.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Box(
                    modifier = Modifier
                        .size(12.dp)
                        .clip(CircleShape)
                        .background(Color(0xFFE53935))
                        .alpha(dotAlpha),
                )
                Spacer(Modifier.width(10.dp))
                Text("Recording  %d:%02d".format(elapsed / 60, elapsed % 60), fontSize = 15.sp)
                Spacer(Modifier.weight(1f))
                TextButton(onClick = onCancel) { Text("Cancel") }
                Spacer(Modifier.width(4.dp))
                Button(onClick = onSend) { Text("Send") }
            }
        }
    }
}
