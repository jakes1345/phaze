package world.phazechat.app

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import androidx.core.app.Person
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.util.Log
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import com.google.firebase.messaging.FirebaseMessagingService
import com.google.firebase.messaging.RemoteMessage

class PhazeFCMService : FirebaseMessagingService() {

    companion object {
        private const val TAG = "PhazeFCM"
        const val CHANNEL_ID = "phaze_messages"
    }

    override fun onCreate() {
        super.onCreate()
        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        val channel = NotificationChannel(CHANNEL_ID, "Messages", NotificationManager.IMPORTANCE_HIGH).apply {
            description = "Phaze message notifications"
            enableVibration(true)
            vibrationPattern = longArrayOf(0, 200, 100, 200)
        }
        nm.createNotificationChannel(channel)
    }

    override fun onNewToken(token: String) {
        Log.d(TAG, "FCM token refreshed")
        getSharedPreferences("phaze_prefs", Context.MODE_PRIVATE)
            .edit().putString("fcm_token", token).apply()
    }

    override fun onMessageReceived(message: RemoteMessage) {
        // Android 13+ requires POST_NOTIFICATIONS permission at runtime
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            if (ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS)
                != PackageManager.PERMISSION_GRANTED) return
        }

        val title = message.notification?.title ?: message.data["title"] ?: "Phaze"
        val body = message.notification?.body ?: message.data["body"] ?: ""
        val senderUsername = message.data["sender"] ?: message.data["from"] ?: ""

        val intent = Intent(this, MainActivity::class.java).apply {
            flags = Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP
            if (senderUsername.isNotBlank()) putExtra("open_chat", senderUsername)
        }
        val pendingIntent = PendingIntent.getActivity(
            this, senderUsername.hashCode(), intent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )

        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager

        val builder = NotificationCompat.Builder(this, CHANNEL_ID)
            .setSmallIcon(R.mipmap.ic_launcher)
            .setContentTitle(title)
            .setContentText(body)
            .setAutoCancel(true)
            .setContentIntent(pendingIntent)
            .setPriority(NotificationCompat.PRIORITY_HIGH)

        // Android 11+ Messaging Style for People & Conversations
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P && senderUsername.isNotBlank()) {
            val sender = Person.Builder().setName(senderUsername).build()
            val style = NotificationCompat.MessagingStyle(sender)
                .addMessage(body, System.currentTimeMillis(), sender)
            builder.setStyle(style)
        }

        // Use sender hashCode as notification ID so one notification per conversation
        val notifId = if (senderUsername.isNotBlank()) senderUsername.hashCode() else message.messageId?.hashCode() ?: System.currentTimeMillis().toInt()
        nm.notify(notifId, builder.build())
    }
}
