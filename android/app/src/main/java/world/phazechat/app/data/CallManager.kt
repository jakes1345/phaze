package world.phazechat.app.data

import android.content.Context
import android.content.Intent
import android.util.Log
import org.webrtc.*

class CallManager(context: Context) {

    companion object {
        private const val TAG = "CallManager"
    }

    private val appContext = context.applicationContext
    private val eglBase = EglBase.create()
    val eglContext: EglBase.Context get() = eglBase.eglBaseContext

    private val factory: PeerConnectionFactory

    var peerConnection: PeerConnection? = null; private set
    var localStream: MediaStream? = null; private set
    var localVideoTrack: VideoTrack? = null; private set
    var localAudioTrack: AudioTrack? = null; private set

    // Remote video, surfaced once the peer starts sending frames.
    var remoteVideoTrack: VideoTrack? = null; private set

    // Capture-source bookkeeping so we can swap camera <-> screen and release cleanly.
    private var cameraCapturer: VideoCapturer? = null
    private var activeCapturer: VideoCapturer? = null
    private var videoSource: VideoSource? = null
    private var surfaceHelper: SurfaceTextureHelper? = null
    private var videoSender: RtpSender? = null
    private var screenCapturer: VideoCapturer? = null

    var isScreenSharing: Boolean = false; private set

    var onIceCandidate: ((IceCandidate) -> Unit)? = null
    var onRemoteStream: ((MediaStream) -> Unit)? = null
    var onRemoteVideoTrack: ((VideoTrack) -> Unit)? = null
    var onConnectionChange: ((PeerConnection.IceConnectionState) -> Unit)? = null

    init {
        PeerConnectionFactory.initialize(
            PeerConnectionFactory.InitializationOptions.builder(context)
                .setEnableInternalTracer(false)
                .createInitializationOptions()
        )
        factory = PeerConnectionFactory.builder()
            .setVideoDecoderFactory(DefaultVideoDecoderFactory(eglBase.eglBaseContext))
            .setVideoEncoderFactory(DefaultVideoEncoderFactory(eglBase.eglBaseContext, true, true))
            .createPeerConnectionFactory()
    }

    fun createPeerConnection(iceServers: List<PeerConnection.IceServer>): PeerConnection? {
        val config = PeerConnection.RTCConfiguration(iceServers).apply {
            sdpSemantics = PeerConnection.SdpSemantics.UNIFIED_PLAN
        }
        peerConnection = factory.createPeerConnection(config, object : PeerConnection.Observer {
            override fun onIceCandidate(candidate: IceCandidate) {
                onIceCandidate?.invoke(candidate)
            }
            override fun onAddStream(stream: MediaStream) {
                stream.videoTracks.firstOrNull()?.let { surfaceRemoteVideo(it) }
                onRemoteStream?.invoke(stream)
            }
            override fun onAddTrack(receiver: RtpReceiver?, streams: Array<out MediaStream>?) {
                (receiver?.track() as? VideoTrack)?.let { surfaceRemoteVideo(it) }
            }
            override fun onIceConnectionChange(state: PeerConnection.IceConnectionState) {
                Log.d(TAG, "ICE: $state")
                onConnectionChange?.invoke(state)
            }
            override fun onSignalingChange(s: PeerConnection.SignalingState) {}
            override fun onIceConnectionReceivingChange(b: Boolean) {}
            override fun onIceGatheringChange(s: PeerConnection.IceGatheringState) {}
            override fun onRemoveStream(s: MediaStream) {}
            override fun onDataChannel(dc: DataChannel) {}
            override fun onRenegotiationNeeded() {}
            override fun onIceCandidatesRemoved(candidates: Array<out IceCandidate>?) {}
        })
        return peerConnection
    }

    private fun surfaceRemoteVideo(track: VideoTrack) {
        if (remoteVideoTrack === track) return
        remoteVideoTrack = track
        onRemoteVideoTrack?.invoke(track)
    }

    fun startLocalMedia(context: Context, withVideo: Boolean) {
        val audioSource = factory.createAudioSource(MediaConstraints())
        localAudioTrack = factory.createAudioTrack("audio0", audioSource)

        if (withVideo) {
            val capturer = createCameraCapturer(context)
            if (capturer != null) {
                cameraCapturer = capturer
                startCapturer(capturer, isScreencast = false, width = 640, height = 480, fps = 30)
                localVideoTrack = factory.createVideoTrack("video0", videoSource)
            }
        }

        localStream = factory.createLocalMediaStream("local")
        localAudioTrack?.let { localStream?.addTrack(it) }
        localVideoTrack?.let { localStream?.addTrack(it) }

        peerConnection?.let { pc ->
            localAudioTrack?.let { pc.addTrack(it) }
            localVideoTrack?.let { videoSender = pc.addTrack(it) }
        }
    }

    // Spins up a fresh VideoSource + SurfaceTextureHelper for the given capturer.
    private fun startCapturer(capturer: VideoCapturer, isScreencast: Boolean, width: Int, height: Int, fps: Int) {
        surfaceHelper = SurfaceTextureHelper.create("CaptureThread", eglBase.eglBaseContext)
        videoSource = factory.createVideoSource(isScreencast)
        capturer.initialize(surfaceHelper, appContext, videoSource!!.capturerObserver)
        capturer.startCapture(width, height, fps)
        activeCapturer = capturer
    }

    private fun stopActiveCapture() {
        try { activeCapturer?.stopCapture() } catch (_: Exception) {}
        activeCapturer?.dispose()
        activeCapturer = null
        surfaceHelper?.dispose()
        surfaceHelper = null
        videoSource?.dispose()
        videoSource = null
    }

    private fun createCameraCapturer(context: Context): VideoCapturer? {
        val enumerator = Camera2Enumerator(context)
        for (name in enumerator.deviceNames) {
            if (enumerator.isFrontFacing(name)) {
                return enumerator.createCapturer(name, null)
            }
        }
        for (name in enumerator.deviceNames) {
            return enumerator.createCapturer(name, null)
        }
        return null
    }

    /**
     * Replace the outgoing video with a screen capture stream. Requires the
     * Intent returned from MediaProjectionManager.createScreenCaptureIntent and
     * an already-running foreground service (mediaProjection type).
     * Uses RtpSender.setTrack so no renegotiation is needed.
     */
    fun startScreenShare(mediaProjectionPermissionResultData: Intent) {
        if (isScreenSharing) return
        val capturer = ScreenCapturerAndroid(
            mediaProjectionPermissionResultData,
            object : android.media.projection.MediaProjection.Callback() {
                override fun onStop() {
                    Log.d(TAG, "MediaProjection stopped by system")
                }
            }
        )
        stopActiveCapture()
        screenCapturer = capturer
        startCapturer(capturer, isScreencast = true, width = 1280, height = 720, fps = 15)
        val screenTrack = factory.createVideoTrack("screen0", videoSource)
        swapOutgoingVideo(screenTrack)
        isScreenSharing = true
    }

    /** Stop screen sharing and restore the camera (if this was a video call). */
    fun stopScreenShare(context: Context) {
        if (!isScreenSharing) return
        stopActiveCapture()
        screenCapturer = null
        val camera = createCameraCapturer(context)
        if (camera != null) {
            cameraCapturer = camera
            startCapturer(camera, isScreencast = false, width = 640, height = 480, fps = 30)
            val camTrack = factory.createVideoTrack("video0", videoSource)
            swapOutgoingVideo(camTrack)
        } else {
            swapOutgoingVideo(null)
        }
        isScreenSharing = false
    }

    private fun swapOutgoingVideo(newTrack: VideoTrack?) {
        val oldTrack = localVideoTrack
        localVideoTrack = newTrack
        val sender = videoSender
        if (sender != null) {
            sender.setTrack(newTrack, false)
        } else if (newTrack != null) {
            peerConnection?.let { videoSender = it.addTrack(newTrack) }
        }
        oldTrack?.dispose()
    }

    suspend fun createOffer(): SessionDescription? {
        val pc = peerConnection ?: return null
        return suspendCreateSdp { pc.createOffer(it, MediaConstraints()) }
    }

    suspend fun createAnswer(): SessionDescription? {
        val pc = peerConnection ?: return null
        return suspendCreateSdp { pc.createAnswer(it, MediaConstraints()) }
    }

    suspend fun setLocalDescription(sdp: SessionDescription) {
        val pc = peerConnection ?: return
        suspendSetSdp { pc.setLocalDescription(it, sdp) }
    }

    suspend fun setRemoteDescription(sdp: SessionDescription) {
        val pc = peerConnection ?: return
        suspendSetSdp { pc.setRemoteDescription(it, sdp) }
    }

    fun addIceCandidate(candidate: IceCandidate) {
        peerConnection?.addIceCandidate(candidate)
    }

    fun toggleMute(): Boolean {
        val track = localAudioTrack ?: return false
        track.setEnabled(!track.enabled())
        return !track.enabled()
    }

    fun toggleCamera(): Boolean {
        val track = localVideoTrack ?: return false
        track.setEnabled(!track.enabled())
        return track.enabled()
    }

    fun hangUp() {
        stopActiveCapture()
        screenCapturer = null
        cameraCapturer = null
        isScreenSharing = false
        remoteVideoTrack = null
        peerConnection?.close()
        peerConnection = null
        localStream = null
        localAudioTrack = null
        localVideoTrack = null
        videoSender = null
    }

    fun release() {
        hangUp()
        eglBase.release()
    }

    private suspend fun suspendCreateSdp(block: (SdpObserver) -> Unit): SessionDescription? {
        return kotlinx.coroutines.suspendCancellableCoroutine { cont ->
            block(object : SdpObserver {
                override fun onCreateSuccess(sdp: SessionDescription) { cont.resume(sdp) {} }
                override fun onCreateFailure(err: String) { cont.resume(null) {} }
                override fun onSetSuccess() {}
                override fun onSetFailure(err: String) {}
            })
        }
    }

    private suspend fun suspendSetSdp(block: (SdpObserver) -> Unit) {
        kotlinx.coroutines.suspendCancellableCoroutine { cont ->
            block(object : SdpObserver {
                override fun onCreateSuccess(sdp: SessionDescription) {}
                override fun onCreateFailure(err: String) {}
                override fun onSetSuccess() { cont.resume(Unit) {} }
                override fun onSetFailure(err: String) { cont.resume(Unit) {} }
            })
        }
    }
}
