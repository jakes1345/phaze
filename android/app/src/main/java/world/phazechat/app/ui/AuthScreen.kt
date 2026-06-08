package world.phazechat.app.ui

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

@Composable
fun AuthScreen(
    error: String?,
    onLogin: (String, String) -> Unit,
    onRegister: (String, String, String) -> Unit,
    onLoginWithLinkCode: ((String) -> Unit)? = null,
    onCancelLinkLogin: (() -> Unit)? = null,
    scannedLinkCode: String = "",
    onScanQR: (() -> Unit)? = null,
    onScanGallery: (() -> Unit)? = null,
) {
    var mode by remember { mutableStateOf("login") } // login, register, link
    var username by remember { mutableStateOf("") }
    var password by remember { mutableStateOf("") }
    var email by remember { mutableStateOf("") }
    var linkCode by remember { mutableStateOf("") }
    val focus = LocalFocusManager.current

    LaunchedEffect(scannedLinkCode) {
        if (scannedLinkCode.isNotBlank()) {
            linkCode = scannedLinkCode
            mode = "link"
        }
    }

    val submit = {
        focus.clearFocus()
        if (mode == "link") {
            if (linkCode.isNotBlank()) {
                onLoginWithLinkCode?.invoke(linkCode)
            }
        } else if (username.isNotBlank() && password.isNotBlank()) {
            if (mode == "login") onLogin(username, password) else onRegister(username, email, password)
        }
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(32.dp)
            .imePadding(),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Spacer(Modifier.weight(1f))

        Text("Phaze", fontSize = 36.sp, fontWeight = FontWeight.ExtraBold, color = PhazeBrandDark)
        Spacer(Modifier.height(4.dp))
        Text("Encrypted chat for everyone", color = MaterialTheme.colorScheme.onSurfaceVariant, fontSize = 14.sp)
        Spacer(Modifier.height(32.dp))

        if (mode == "link") {
            Text(
                "Open Phaze on a device you're already signed into → Settings → Link a new device. Enter the code below or scan a QR code.",
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                fontSize = 13.sp,
                textAlign = TextAlign.Center,
                modifier = Modifier.padding(bottom = 16.dp)
            )

            Row(
                modifier = Modifier.fillMaxWidth().padding(bottom = 16.dp),
                horizontalArrangement = Arrangement.spacedBy(8.dp)
            ) {
                if (onScanQR != null) {
                    Button(
                        onClick = onScanQR,
                        modifier = Modifier.weight(1f)
                    ) {
                        Text("📸 Camera")
                    }
                }
                if (onScanGallery != null) {
                    OutlinedButton(
                        onClick = onScanGallery,
                        modifier = Modifier.weight(1f)
                    ) {
                        Text("📂 Gallery")
                    }
                }
            }

            OutlinedTextField(
                value = linkCode,
                onValueChange = { linkCode = it },
                label = { Text("Link Code / QR Token") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Go),
                keyboardActions = KeyboardActions(onGo = { submit() }),
            )
            Spacer(Modifier.height(16.dp))

        } else {
            OutlinedTextField(
                value = username,
                onValueChange = { username = it },
                label = { Text("Username") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Next),
            )
            Spacer(Modifier.height(8.dp))

            if (mode == "register") {
                OutlinedTextField(
                    value = email,
                    onValueChange = { email = it },
                    label = { Text("Email") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Next),
                )
                Spacer(Modifier.height(8.dp))
            }

            OutlinedTextField(
                value = password,
                onValueChange = { password = it },
                label = { Text("Password") },
                singleLine = true,
                visualTransformation = PasswordVisualTransformation(),
                modifier = Modifier.fillMaxWidth(),
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Go),
                keyboardActions = KeyboardActions(onGo = { submit() }),
            )
            Spacer(Modifier.height(16.dp))
        }

        if (error != null) {
            Text(error, color = if (error.contains("Waiting")) PhazeSuccess else MaterialTheme.colorScheme.error, fontSize = 13.sp, textAlign = TextAlign.Center)
            Spacer(Modifier.height(8.dp))
        }

        Button(
            onClick = { submit() },
            modifier = Modifier.fillMaxWidth().height(48.dp),
            enabled = if (mode == "link") linkCode.isNotBlank() else (username.isNotBlank() && password.isNotBlank()),
        ) {
            Text(
                when (mode) {
                    "link" -> "Sign in with Code"
                    "login" -> "Sign In"
                    else -> "Create Account"
                },
                fontWeight = FontWeight.Bold
            )
        }
        Spacer(Modifier.height(12.dp))

        if (mode == "link") {
            TextButton(onClick = {
                mode = "login"
                onCancelLinkLogin?.invoke()
            }) {
                Text("Back to sign in")
            }
        } else {
            TextButton(onClick = { mode = if (mode == "login") "register" else "login" }) {
                Text(if (mode == "login") "Create an account" else "Already have an account? Sign in")
            }
            if (mode == "login" && onLoginWithLinkCode != null) {
                Spacer(Modifier.height(4.dp))
                TextButton(onClick = { mode = "link" }) {
                    Text("Sign in with a link code")
                }
            }
        }

        Spacer(Modifier.weight(1f))
    }
}
