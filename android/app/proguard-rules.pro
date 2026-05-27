# Lazysodium JNA
-keep class com.sun.jna.** { *; }
-keep class com.goterl.lazysodium.** { *; }
-dontwarn com.sun.jna.**

# OkHttp
-dontwarn okhttp3.internal.platform.**
-dontwarn org.conscrypt.**
-dontwarn org.bouncycastle.**
-dontwarn org.openjsse.**

# WebRTC
-keep class org.webrtc.** { *; }
