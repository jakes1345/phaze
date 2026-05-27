package world.phazechat.app.crypto

import android.util.Base64
import com.goterl.lazysodium.LazySodiumAndroid
import com.goterl.lazysodium.SodiumAndroid
import com.goterl.lazysodium.interfaces.Box
import com.goterl.lazysodium.utils.Key
import com.goterl.lazysodium.utils.KeyPair
import java.security.MessageDigest

private const val E2EE_PREFIX = "E2EE:"
private val sodium = LazySodiumAndroid(SodiumAndroid())

data class NaClKeyPair(val publicKey: ByteArray, val secretKey: ByteArray)

fun generateKeyPair(): NaClKeyPair {
    val kp: KeyPair = sodium.cryptoBoxKeypair()
    return NaClKeyPair(kp.publicKey.asBytes, kp.secretKey.asBytes)
}

fun fingerprint(pub: ByteArray): String {
    val digest = MessageDigest.getInstance("SHA-256").digest(pub)
    return digest.take(8).joinToString("") { "%02x".format(it) }
}

fun encryptForPeer(plain: String, peerPub: ByteArray, mySecret: ByteArray): String {
    val msg = plain.toByteArray(Charsets.UTF_8)
    val nonce = ByteArray(Box.NONCEBYTES)
    sodium.randomBytesBuf(nonce, nonce.size)

    val ciphertext = ByteArray(msg.size + Box.MACBYTES)
    sodium.cryptoBoxEasy(
        ciphertext, msg, msg.size.toLong(), nonce,
        Key.fromBytes(peerPub).asBytes,
        Key.fromBytes(mySecret).asBytes
    )

    val combined = nonce + ciphertext
    return E2EE_PREFIX + combined.toHex()
}

fun decryptFromPeer(field: String, senderPub: ByteArray, mySecret: ByteArray): String {
    if (!field.startsWith(E2EE_PREFIX)) return field
    val raw = field.removePrefix(E2EE_PREFIX).fromHex()
    if (raw.size < Box.NONCEBYTES) return ""

    val nonce = raw.sliceArray(0 until Box.NONCEBYTES)
    val ciphertext = raw.sliceArray(Box.NONCEBYTES until raw.size)
    val plaintext = ByteArray(ciphertext.size - Box.MACBYTES)

    val ok = sodium.cryptoBoxOpenEasy(
        plaintext, ciphertext, ciphertext.size.toLong(), nonce,
        Key.fromBytes(senderPub).asBytes,
        Key.fromBytes(mySecret).asBytes
    )
    return if (ok) String(plaintext, Charsets.UTF_8) else ""
}

fun encodePublicKeyB64(pub: ByteArray): String =
    Base64.encodeToString(pub, Base64.NO_WRAP)

fun decodePublicKeyB64(b64: String): ByteArray? {
    return try {
        val data = Base64.decode(b64, Base64.DEFAULT)
        if (data.size == 32) data else null
    } catch (_: Exception) { null }
}

private fun ByteArray.toHex(): String = joinToString("") { "%02x".format(it) }

private fun String.fromHex(): ByteArray {
    val hex = this
    return ByteArray(hex.length / 2) { hex.substring(it * 2, it * 2 + 2).toInt(16).toByte() }
}
