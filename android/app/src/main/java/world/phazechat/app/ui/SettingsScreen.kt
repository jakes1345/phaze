package world.phazechat.app.ui

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

@Composable
fun SettingsScreen(
    me: String,
    mood: String = "",
    displayName: String = "",
    onUpdateProfile: ((String, String) -> Unit)? = null,
    onEnable2FA: (() -> Unit)? = null,
    onDisable2FA: (() -> Unit)? = null,
    onSignOut: () -> Unit,
) {
    var editMood by remember { mutableStateOf(mood) }
    var editName by remember { mutableStateOf(displayName.ifEmpty { me }) }
    var saved by remember { mutableStateOf(false) }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(24.dp),
    ) {
        Text("Settings", fontSize = 24.sp, fontWeight = FontWeight.ExtraBold)
        Spacer(Modifier.height(24.dp))

        // Profile card
        Card(
            modifier = Modifier.fillMaxWidth(),
            colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant),
        ) {
            Row(
                modifier = Modifier.padding(16.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Avatar(me, 48, "Online")
                Spacer(Modifier.width(16.dp))
                Column {
                    Text(me, fontWeight = FontWeight.Bold, fontSize = 16.sp)
                    Text("Online", color = PhazeSuccess, fontSize = 13.sp)
                }
            }
        }

        Spacer(Modifier.height(24.dp))
        Text("PROFILE", fontSize = 12.sp, fontWeight = FontWeight.Bold, color = MaterialTheme.colorScheme.onSurfaceVariant, letterSpacing = 1.sp)
        Spacer(Modifier.height(8.dp))

        OutlinedTextField(
            value = editName,
            onValueChange = { editName = it },
            label = { Text("Display Name") },
            singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )
        Spacer(Modifier.height(8.dp))

        OutlinedTextField(
            value = editMood,
            onValueChange = { editMood = it },
            label = { Text("Mood / Status") },
            placeholder = { Text("What are you up to?") },
            singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )
        Spacer(Modifier.height(8.dp))

        if (onUpdateProfile != null) {
            Button(
                onClick = {
                    onUpdateProfile(editName.trim(), editMood.trim())
                    saved = true
                },
                modifier = Modifier.fillMaxWidth(),
            ) { Text("Save Profile") }
            if (saved) {
                Text("✓ Saved", color = PhazeSuccess, fontSize = 13.sp, modifier = Modifier.padding(top = 4.dp))
            }
        }

        Spacer(Modifier.height(24.dp))
        HorizontalDivider()
        Spacer(Modifier.height(16.dp))

        Text("SECURITY", fontSize = 12.sp, fontWeight = FontWeight.Bold, color = MaterialTheme.colorScheme.onSurfaceVariant, letterSpacing = 1.sp)
        Spacer(Modifier.height(8.dp))

        if (onEnable2FA != null) {
            OutlinedButton(onClick = onEnable2FA, modifier = Modifier.fillMaxWidth()) {
                Text("Enable Two-Factor Auth (TOTP)")
            }
        }
        if (onDisable2FA != null) {
            TextButton(onClick = onDisable2FA, modifier = Modifier.fillMaxWidth()) {
                Text("Disable 2FA", color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }

        Spacer(Modifier.height(24.dp))
        HorizontalDivider()
        Spacer(Modifier.height(16.dp))

        Text("ACCOUNT", fontSize = 12.sp, fontWeight = FontWeight.Bold, color = MaterialTheme.colorScheme.onSurfaceVariant, letterSpacing = 1.sp)
        Spacer(Modifier.height(8.dp))

        OutlinedButton(
            onClick = onSignOut,
            modifier = Modifier.fillMaxWidth(),
            colors = ButtonDefaults.outlinedButtonColors(contentColor = PhazeDanger),
        ) {
            Text("Sign Out")
        }

        Spacer(Modifier.height(32.dp))
        Text(
            "Phaze v1.0.0 · Encrypted chat for everyone",
            fontSize = 12.sp,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.align(Alignment.CenterHorizontally),
        )
        Spacer(Modifier.height(16.dp))
    }
}
