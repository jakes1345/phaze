package world.phazechat.app

import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Context
import android.util.Log
import androidx.core.app.NotificationCompat
import com.google.firebase.messaging.FirebaseMessagingService
import com.google.firebase.messaging.RemoteMessage

class PhazeFCMService : FirebaseMessagingService() {

    companion object {
        private const val TAG = "PhazeFCM"
        private const val CHANNEL_ID = "phaze_messages"
    }

    override fun onCreate() {
        super.onCreate()
        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        val channel = NotificationChannel(CHANNEL_ID, "Messages", NotificationManager.IMPORTANCE_HIGH).apply {
            description = "Phaze message notifications"
        }
        nm.createNotificationChannel(channel)
    }

    override fun onNewToken(token: String) {
        Log.d(TAG, "FCM token refreshed")
        getSharedPreferences("phaze_prefs", Context.MODE_PRIVATE)
            .edit().putString("fcm_token", token).apply()
    }

    override fun onMessageReceived(message: RemoteMessage) {
        val title = message.notification?.title ?: message.data["title"] ?: "Phaze"
        val body = message.notification?.body ?: message.data["body"] ?: ""

        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        val notification = NotificationCompat.Builder(this, CHANNEL_ID)
            .setSmallIcon(android.R.drawable.ic_dialog_info)
            .setContentTitle(title)
            .setContentText(body)
            .setAutoCancel(true)
            .build()

        nm.notify(System.currentTimeMillis().toInt(), notification)
    }
}
