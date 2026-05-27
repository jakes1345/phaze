package world.phazechat.app.data

import android.content.Context
import android.util.Log
import org.webrtc.*

class CallManager(context: Context) {

    companion object {
        private const val TAG = "CallManager"
    }

    private val eglBase = EglBase.create()
    val eglContext: EglBase.Context get() = eglBase.eglBaseContext

    private val factory: PeerConnectionFactory

    var peerConnection: PeerConnection? = null; private set
    var localStream: MediaStream? = null; private set
    var localVideoTrack: VideoTrack? = null; private set
    var localAudioTrack: AudioTrack? = null; private set

    var onIceCandidate: ((IceCandidate) -> Unit)? = null
    var onRemoteStream: ((MediaStream) -> Unit)? = null
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
                onRemoteStream?.invoke(stream)
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
            override fun onAddTrack(receiver: RtpReceiver?, streams: Array<out MediaStream>?) {}
        })
        return peerConnection
    }

    fun startLocalMedia(context: Context, withVideo: Boolean) {
        val audioSource = factory.createAudioSource(MediaConstraints())
        localAudioTrack = factory.createAudioTrack("audio0", audioSource)

        if (withVideo) {
            val videoCapturer = createCameraCapturer(context)
            if (videoCapturer != null) {
                val surfaceHelper = SurfaceTextureHelper.create("CaptureThread", eglBase.eglBaseContext)
                val videoSource = factory.createVideoSource(videoCapturer.isScreencast)
                videoCapturer.initialize(surfaceHelper, context, videoSource.capturerObserver)
                videoCapturer.startCapture(640, 480, 30)
                localVideoTrack = factory.createVideoTrack("video0", videoSource)
            }
        }

        localStream = factory.createLocalMediaStream("local")
        localAudioTrack?.let { localStream?.addTrack(it) }
        localVideoTrack?.let { localStream?.addTrack(it) }

        peerConnection?.let { pc ->
            localAudioTrack?.let { pc.addTrack(it) }
            localVideoTrack?.let { pc.addTrack(it) }
        }
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
        peerConnection?.close()
        peerConnection = null
        localStream = null
        localAudioTrack = null
        localVideoTrack = null
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
