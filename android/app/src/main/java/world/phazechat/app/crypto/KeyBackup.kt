package world.phazechat.app.crypto

import android.util.Base64
import org.json.JSONObject
import java.security.SecureRandom
import javax.crypto.Cipher
import javax.crypto.SecretKeyFactory
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.PBEKeySpec
import javax.crypto.spec.SecretKeySpec

data class KeyBackupBlob(
    val ciphertext: String,
    val salt: String,
    val iterations: Int
)

object KeyBackup {
    private const val ITERATIONS = 200000
    private const val SALT_BYTES = 16
    private const val IV_BYTES = 12
    private const val KEY_LENGTH = 256

    private fun b64(bytes: ByteArray): String = Base64.encodeToString(bytes, Base64.NO_WRAP)
    private fun unb64(str: String): ByteArray = Base64.decode(str, Base64.NO_WRAP)

    fun encryptKeypair(pub: ByteArray, sec: ByteArray, pin: String): KeyBackupBlob {
        if (pin.length < 4) throw IllegalArgumentException("PIN must be at least 4 characters")
        
        val random = SecureRandom()
        val salt = ByteArray(SALT_BYTES)
        random.nextBytes(salt)

        // PBKDF2 key derivation
        val spec = PBEKeySpec(pin.toCharArray(), salt, ITERATIONS, KEY_LENGTH)
        val factory = SecretKeyFactory.getInstance("PBKDF2WithHmacSHA256")
        val derivedKeyBytes = factory.generateSecret(spec).encoded
        val secretKeySpec = SecretKeySpec(derivedKeyBytes, "AES")

        val iv = ByteArray(IV_BYTES)
        random.nextBytes(iv)

        // Payload JSON
        val payload = JSONObject().apply {
            put("pub", b64(pub))
            put("sec", b64(sec))
        }.toString()

        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        val gcmSpec = GCMParameterSpec(128, iv)
        cipher.init(Cipher.ENCRYPT_MODE, secretKeySpec, gcmSpec)
        val ct = cipher.doFinal(payload.toByteArray(Charsets.UTF_8))

        // Combined = IV + ciphertext
        val combined = ByteArray(iv.size + ct.size)
        System.arraycopy(iv, 0, combined, 0, iv.size)
        System.arraycopy(ct, 0, combined, iv.size, ct.size)

        return KeyBackupBlob(
            ciphertext = b64(combined),
            salt = b64(salt),
            iterations = ITERATIONS
        )
    }

    fun decryptKeypair(blob: KeyBackupBlob, pin: String): NaClKeyPair {
        val salt = unb64(blob.salt)
        val combined = unb64(blob.ciphertext)
        if (combined.size < IV_BYTES + 16) throw IllegalArgumentException("Backup blob malformed")

        val iv = combined.sliceArray(0 until IV_BYTES)
        val ct = combined.sliceArray(IV_BYTES until combined.size)

        val iterations = if (blob.iterations <= 0) ITERATIONS else blob.iterations

        // PBKDF2 key derivation
        val spec = PBEKeySpec(pin.toCharArray(), salt, iterations, KEY_LENGTH)
        val factory = SecretKeyFactory.getInstance("PBKDF2WithHmacSHA256")
        val derivedKeyBytes = factory.generateSecret(spec).encoded
        val secretKeySpec = SecretKeySpec(derivedKeyBytes, "AES")

        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        val gcmSpec = GCMParameterSpec(128, iv)
        cipher.init(Cipher.DECRYPT_MODE, secretKeySpec, gcmSpec)
        
        val decryptedBytes = try {
            cipher.doFinal(ct)
        } catch (e: Exception) {
            throw Exception("Incorrect PIN")
        }

        val json = JSONObject(String(decryptedBytes, Charsets.UTF_8))
        val pub = unb64(json.getString("pub"))
        val sec = unb64(json.getString("sec"))

        if (pub.size != 32 || sec.size != 32) throw Exception("Backup contained malformed keys")
        return NaClKeyPair(pub, sec)
    }
}
