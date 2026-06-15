package world.phazechat.app.ui

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.ui.platform.LocalContext
import android.content.Intent
import android.net.Uri
import world.phazechat.app.BuildConfig

@Composable
fun SettingsScreen(
    me: String,
    mood: String = "",
    displayName: String = "",
    onUpdateProfile: ((String, String) -> Unit)? = null,
    onEnable2FA: (() -> Unit)? = null,
    onConfirm2FA: ((String) -> Unit)? = null,
    onDisable2FA: ((String) -> Unit)? = null,
    onCancel2FA: (() -> Unit)? = null,
    twoFactorUri: String? = null,
    twoFactorStatus: String? = null,
    twoFactorBackupCodes: List<String>? = null,
    onDismissBackupCodes: (() -> Unit)? = null,
    theme: String = "dark",
    onSetTheme: ((String) -> Unit)? = null,
    snow: Boolean = false,
    onSetSnow: ((Boolean) -> Unit)? = null,
    onSignOut: () -> Unit,
    linkCode: String? = null,
    linkStatus: String? = null,
    linkError: String? = null,
    onGenerateLinkCode: (() -> Unit)? = null,
    onApproveDevice: ((String) -> Unit)? = null,
    onClearLinkStatus: (() -> Unit)? = null,
    // E2EE Key Backup
    keyBackupStatus: String? = null,
    keyBackupError: String? = null,
    onBackupKeys: ((String) -> Unit)? = null,
    onRestoreKeys: ((String) -> Unit)? = null,
    onClearKeyBackupStatus: (() -> Unit)? = null,
    onDeleteAccount: ((String) -> Unit)? = null,
) {
    var editMood by remember { mutableStateOf(mood) }
    var editName by remember { mutableStateOf(displayName.ifEmpty { me }) }
    var saved by remember { mutableStateOf(false) }
    val context = LocalContext.current

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

        if (onSetTheme != null) {
            Text("APPEARANCE", fontSize = 12.sp, fontWeight = FontWeight.Bold, color = MaterialTheme.colorScheme.onSurfaceVariant, letterSpacing = 1.sp)
            Spacer(Modifier.height(8.dp))
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                listOf("dark" to "Dark", "light" to "Light", "skype7" to "Skype 7").forEach { (key, label) ->
                    val selected = theme == key
                    if (selected) {
                        Button(onClick = { onSetTheme(key) }, modifier = Modifier.weight(1f), contentPadding = PaddingValues(vertical = 8.dp)) { Text(label, fontSize = 13.sp) }
                    } else {
                        OutlinedButton(onClick = { onSetTheme(key) }, modifier = Modifier.weight(1f), contentPadding = PaddingValues(vertical = 8.dp)) { Text(label, fontSize = 13.sp) }
                    }
                }
            }
            if (onSetSnow != null) {
                Spacer(Modifier.height(8.dp))
                Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                    Text("❄ Snowflakes", fontSize = 14.sp, modifier = Modifier.weight(1f))
                    Switch(checked = snow, onCheckedChange = { onSetSnow(it) })
                }
            }
            Spacer(Modifier.height(24.dp))
            HorizontalDivider()
            Spacer(Modifier.height(16.dp))
        }

        Text("SECURITY", fontSize = 12.sp, fontWeight = FontWeight.Bold, color = MaterialTheme.colorScheme.onSurfaceVariant, letterSpacing = 1.sp)
        Spacer(Modifier.height(8.dp))

        var totpCode by remember { mutableStateOf("") }
        var disablePw by remember { mutableStateOf("") }

        if (twoFactorUri != null) {
            // Enrollment pending: show the secret to add to an authenticator app,
            // then confirm with a generated code.
            val secret = remember(twoFactorUri) {
                Regex("secret=([A-Z0-9]+)").find(twoFactorUri)?.groupValues?.get(1) ?: twoFactorUri
            }
            Text("Add this secret to your authenticator app:", fontSize = 13.sp)
            Spacer(Modifier.height(6.dp))
            SelectionContainer {
                Text(secret, fontSize = 15.sp, fontWeight = FontWeight.Bold, fontFamily = androidx.compose.ui.text.font.FontFamily.Monospace)
            }
            Spacer(Modifier.height(10.dp))
            OutlinedTextField(
                value = totpCode, onValueChange = { totpCode = it.filter { c -> c.isDigit() }.take(6) },
                label = { Text("6-digit code") }, singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            Spacer(Modifier.height(8.dp))
            Row(Modifier.fillMaxWidth()) {
                Button(onClick = { onConfirm2FA?.invoke(totpCode); totpCode = "" }, modifier = Modifier.weight(1f)) {
                    Text("Confirm")
                }
                Spacer(Modifier.width(8.dp))
                TextButton(onClick = { onCancel2FA?.invoke() }) { Text("Cancel") }
            }
        } else {
            if (onEnable2FA != null) {
                OutlinedButton(onClick = onEnable2FA, modifier = Modifier.fillMaxWidth()) {
                    Text("Enable Two-Factor Auth (TOTP)")
                }
            }
            if (onDisable2FA != null) {
                Spacer(Modifier.height(8.dp))
                OutlinedTextField(
                    value = disablePw, onValueChange = { disablePw = it },
                    label = { Text("Password (to disable 2FA)") }, singleLine = true,
                    visualTransformation = androidx.compose.ui.text.input.PasswordVisualTransformation(),
                    modifier = Modifier.fillMaxWidth(),
                )
                Spacer(Modifier.height(4.dp))
                TextButton(onClick = { onDisable2FA(disablePw); disablePw = "" }, modifier = Modifier.fillMaxWidth()) {
                    Text("Disable 2FA", color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
        }
        if (twoFactorStatus != null) {
            Spacer(Modifier.height(6.dp))
            Text(twoFactorStatus, fontSize = 12.sp, color = MaterialTheme.colorScheme.primary)
        }
        if (!twoFactorBackupCodes.isNullOrEmpty()) {
            Spacer(Modifier.height(12.dp))
            Card(
                colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant),
                modifier = Modifier.fillMaxWidth(),
            ) {
                Column(modifier = Modifier.padding(14.dp)) {
                    Text("Backup Codes", fontWeight = FontWeight.Bold, fontSize = 14.sp)
                    Spacer(Modifier.height(4.dp))
                    Text("Save these codes somewhere safe. Each can be used once if you lose your authenticator.", fontSize = 12.sp, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    Spacer(Modifier.height(10.dp))
                    SelectionContainer {
                        Column {
                            twoFactorBackupCodes.chunked(2).forEach { row ->
                                Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                                    row.forEach { code ->
                                        Text(code, fontFamily = androidx.compose.ui.text.font.FontFamily.Monospace, fontSize = 14.sp, modifier = Modifier.weight(1f))
                                    }
                                }
                            }
                        }
                    }
                    Spacer(Modifier.height(10.dp))
                    TextButton(onClick = { onDismissBackupCodes?.invoke() }, modifier = Modifier.align(Alignment.End)) {
                        Text("I've saved these")
                    }
                }
            }
        }

        Spacer(Modifier.height(24.dp))
        HorizontalDivider()
        Spacer(Modifier.height(16.dp))

        Text("LINKED DEVICES", fontSize = 12.sp, fontWeight = FontWeight.Bold, color = MaterialTheme.colorScheme.onSurfaceVariant, letterSpacing = 1.sp)
        Spacer(Modifier.height(8.dp))

        if (onGenerateLinkCode != null) {
            Card(
                modifier = Modifier.fillMaxWidth().padding(bottom = 12.dp),
                colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.5f)),
            ) {
                Column(modifier = Modifier.padding(16.dp)) {
                    Text("Link a New Device", fontWeight = FontWeight.Bold, fontSize = 14.sp)
                    Spacer(Modifier.height(4.dp))
                    Text(
                        "Generate a one-time code to authorize another device to log in to your account.",
                        fontSize = 12.sp,
                        color = MaterialTheme.colorScheme.onSurfaceVariant
                    )
                    Spacer(Modifier.height(12.dp))
                    if (linkCode != null) {
                        Card(
                            modifier = Modifier.fillMaxWidth().align(Alignment.CenterHorizontally),
                            colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.secondaryContainer)
                        ) {
                            Text(
                                text = linkCode,
                                modifier = Modifier.padding(12.dp).fillMaxWidth(),
                                textAlign = TextAlign.Center,
                                fontWeight = FontWeight.ExtraBold,
                                fontSize = 20.sp,
                                letterSpacing = 1.sp
                            )
                        }
                        Spacer(Modifier.height(8.dp))
                    }
                    Button(
                        onClick = onGenerateLinkCode,
                        modifier = Modifier.fillMaxWidth()
                    ) {
                        Text(if (linkCode != null) "Regenerate Code" else "Generate Link Code")
                    }
                }
            }
        }

        if (onApproveDevice != null) {
            var approveInput by remember { mutableStateOf("") }
            Card(
                modifier = Modifier.fillMaxWidth().padding(bottom = 12.dp),
                colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.5f)),
            ) {
                Column(modifier = Modifier.padding(16.dp)) {
                    Text("Approve Another Device", fontWeight = FontWeight.Bold, fontSize = 14.sp)
                    Spacer(Modifier.height(4.dp))
                    Text(
                        "Enter the Link Code or QR Token shown on the other device to authorize it.",
                        fontSize = 12.sp,
                        color = MaterialTheme.colorScheme.onSurfaceVariant
                    )
                    Spacer(Modifier.height(12.dp))
                    OutlinedTextField(
                        value = approveInput,
                        onValueChange = { approveInput = it },
                        label = { Text("Link Code / QR Token") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth()
                    )
                    Spacer(Modifier.height(12.dp))
                    Button(
                        onClick = {
                            onApproveDevice(approveInput)
                            approveInput = ""
                        },
                        modifier = Modifier.fillMaxWidth(),
                        enabled = approveInput.isNotBlank()
                    ) {
                        Text("Approve Device")
                    }

                    if (linkStatus != null) {
                        Spacer(Modifier.height(8.dp))
                        Text(linkStatus, color = PhazeSuccess, fontSize = 13.sp)
                    }
                    if (linkError != null) {
                        Spacer(Modifier.height(8.dp))
                        Text(linkError, color = PhazeDanger, fontSize = 13.sp)
                    }
                    if (linkStatus != null || linkError != null) {
                        Spacer(Modifier.height(4.dp))
                        TextButton(onClick = { onClearLinkStatus?.invoke() }) {
                            Text("Clear status", fontSize = 11.sp)
                        }
                    }
                }
            }
        }

        Spacer(Modifier.height(24.dp))
        HorizontalDivider()
        Spacer(Modifier.height(16.dp))

        Text("E2EE KEY BACKUP", fontSize = 12.sp, fontWeight = FontWeight.Bold, color = MaterialTheme.colorScheme.onSurfaceVariant, letterSpacing = 1.sp)
        Spacer(Modifier.height(4.dp))
        Text(
            "Your encryption keys are stored only on this device. Back them up with a PIN so you can restore them on a new device without losing access to past messages.",
            fontSize = 12.sp,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(12.dp))

        if (onBackupKeys != null) {
            var backupPin by remember { mutableStateOf("") }
            Card(
                modifier = Modifier.fillMaxWidth().padding(bottom = 12.dp),
                colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.5f)),
            ) {
                Column(modifier = Modifier.padding(16.dp)) {
                    Text("Backup Keys", fontWeight = FontWeight.Bold, fontSize = 14.sp)
                    Spacer(Modifier.height(4.dp))
                    Text(
                        "Choose a PIN to encrypt your keys before uploading.",
                        fontSize = 12.sp,
                        color = MaterialTheme.colorScheme.onSurfaceVariant
                    )
                    Spacer(Modifier.height(12.dp))
                    OutlinedTextField(
                        value = backupPin,
                        onValueChange = { backupPin = it },
                        label = { Text("Backup PIN (min 4 chars)") },
                        singleLine = true,
                        visualTransformation = androidx.compose.ui.text.input.PasswordVisualTransformation(),
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
                        modifier = Modifier.fillMaxWidth()
                    )
                    Spacer(Modifier.height(12.dp))
                    Button(
                        onClick = {
                            onBackupKeys(backupPin)
                            backupPin = ""
                        },
                        modifier = Modifier.fillMaxWidth(),
                        enabled = backupPin.length >= 4
                    ) { Text("Backup My Keys") }
                }
            }
        }

        if (onRestoreKeys != null) {
            var restorePin by remember { mutableStateOf("") }
            Card(
                modifier = Modifier.fillMaxWidth().padding(bottom = 12.dp),
                colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.5f)),
            ) {
                Column(modifier = Modifier.padding(16.dp)) {
                    Text("Restore Keys", fontWeight = FontWeight.Bold, fontSize = 14.sp)
                    Spacer(Modifier.height(4.dp))
                    Text(
                        "Enter your backup PIN to fetch and decrypt your stored keys.",
                        fontSize = 12.sp,
                        color = MaterialTheme.colorScheme.onSurfaceVariant
                    )
                    Spacer(Modifier.height(12.dp))
                    OutlinedTextField(
                        value = restorePin,
                        onValueChange = { restorePin = it },
                        label = { Text("Backup PIN") },
                        singleLine = true,
                        visualTransformation = androidx.compose.ui.text.input.PasswordVisualTransformation(),
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
                        modifier = Modifier.fillMaxWidth()
                    )
                    Spacer(Modifier.height(12.dp))
                    OutlinedButton(
                        onClick = {
                            onRestoreKeys(restorePin)
                            restorePin = ""
                        },
                        modifier = Modifier.fillMaxWidth(),
                        enabled = restorePin.length >= 4
                    ) { Text("Restore My Keys") }
                }
            }
        }

        if (keyBackupStatus != null) {
            Text(keyBackupStatus, color = PhazeSuccess, fontSize = 13.sp)
            Spacer(Modifier.height(4.dp))
        }
        if (keyBackupError != null) {
            Text(keyBackupError, color = PhazeDanger, fontSize = 13.sp)
            Spacer(Modifier.height(4.dp))
        }
        if (keyBackupStatus != null || keyBackupError != null) {
            TextButton(onClick = { onClearKeyBackupStatus?.invoke() }) {
                Text("Dismiss", fontSize = 11.sp)
            }
        }

        Spacer(Modifier.height(24.dp))
        HorizontalDivider()
        Spacer(Modifier.height(16.dp))

        Text("SUPPORT PHAZE", fontSize = 12.sp, fontWeight = FontWeight.Bold, color = MaterialTheme.colorScheme.onSurfaceVariant, letterSpacing = 1.sp)
        Spacer(Modifier.height(8.dp))
        Text(
            "Phaze is free and equal for everyone. If you want to chip in, supporters get a 💜 badge.",
            fontSize = 13.sp,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(8.dp))
        Button(
            onClick = {
                val intent = Intent(Intent.ACTION_VIEW, Uri.parse("https://buymeacoffee.com/phazeworld"))
                context.startActivity(intent)
            },
            modifier = Modifier.fillMaxWidth(),
            colors = ButtonDefaults.buttonColors(containerColor = PhazeBrandDark),
        ) {
            Text("💜 Support Phaze")
        }

        Spacer(Modifier.height(16.dp))
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

        if (onDeleteAccount != null) {
            Spacer(Modifier.height(12.dp))
            var showDelete by remember { mutableStateOf(false) }
            TextButton(
                onClick = { showDelete = true },
                modifier = Modifier.fillMaxWidth(),
                colors = ButtonDefaults.textButtonColors(contentColor = PhazeDanger),
            ) {
                Text("Delete Account")
            }

            if (showDelete) {
                var pw by remember { mutableStateOf("") }
                AlertDialog(
                    onDismissRequest = { showDelete = false },
                    title = { Text("Delete account?") },
                    text = {
                        Column {
                            Text(
                                "This permanently erases your account, messages, and data. " +
                                    "This cannot be undone. Enter your password to confirm.",
                                fontSize = 14.sp,
                            )
                            Spacer(Modifier.height(12.dp))
                            OutlinedTextField(
                                value = pw,
                                onValueChange = { pw = it },
                                label = { Text("Password") },
                                singleLine = true,
                                visualTransformation = PasswordVisualTransformation(),
                                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
                                modifier = Modifier.fillMaxWidth(),
                            )
                        }
                    },
                    confirmButton = {
                        TextButton(
                            onClick = { onDeleteAccount(pw); showDelete = false },
                            enabled = pw.isNotBlank(),
                            colors = ButtonDefaults.textButtonColors(contentColor = PhazeDanger),
                        ) { Text("Delete forever") }
                    },
                    dismissButton = {
                        TextButton(onClick = { showDelete = false }) { Text("Cancel") }
                    },
                )
            }
        }

        Spacer(Modifier.height(32.dp))
        Row(
            horizontalArrangement = Arrangement.Center,
            modifier = Modifier.fillMaxWidth(),
        ) {
            TextButton(onClick = {
                val intent = Intent(Intent.ACTION_VIEW, Uri.parse("https://phazechat.world/privacy"))
                context.startActivity(intent)
            }) { Text("Privacy Policy", fontSize = 12.sp) }
            Text("·", fontSize = 12.sp, modifier = Modifier.align(Alignment.CenterVertically))
            TextButton(onClick = {
                val intent = Intent(Intent.ACTION_VIEW, Uri.parse("https://phazechat.world/terms"))
                context.startActivity(intent)
            }) { Text("Terms of Service", fontSize = 12.sp) }
        }
        Text(
            "Phaze v${BuildConfig.VERSION_NAME} · Encrypted chat for everyone",
            fontSize = 12.sp,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.align(Alignment.CenterHorizontally),
        )
        Spacer(Modifier.height(16.dp))
    }
}
