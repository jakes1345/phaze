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
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

@Composable
fun AuthScreen(
    error: String?,
    onLogin: (String, String) -> Unit,
    onRegister: (String, String, String) -> Unit,
    onVerifyEmail: (String) -> Unit,
    onResendVerification: (String) -> Unit,
    onCancelVerification: () -> Unit,
    pendingVerification: Boolean = false,
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
    var verifyCode by remember { mutableStateOf("") }
    val focus = LocalFocusManager.current

    LaunchedEffect(scannedLinkCode) {
        if (scannedLinkCode.isNotBlank()) {
            linkCode = scannedLinkCode
            mode = "link"
        }
    }

    val submit = {
        focus.clearFocus()
        when {
            pendingVerification -> {
                if (verifyCode.length == 6) onVerifyEmail(verifyCode)
            }
            mode == "link" -> {
                if (linkCode.isNotBlank()) onLoginWithLinkCode?.invoke(linkCode)
            }
            mode == "register" -> {
                if (username.isNotBlank() && password.isNotBlank() && email.isNotBlank()) {
                    onRegister(username, email, password)
                }
            }
            else -> {
                if (username.isNotBlank() && password.isNotBlank()) onLogin(username, password)
            }
        }
    }

    val isSuccessMsg = error != null && (
        error.contains("Waiting") ||
        error.contains("verified") ||
        error.contains("resent") ||
        error.contains("Check your email")
    )

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

        if (pendingVerification) {
            // ── Email verification step ───────────────────────────────
            Text(
                "We sent a 6-digit code to your email. Enter it below to activate your account.",
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                fontSize = 13.sp,
                textAlign = TextAlign.Center,
                modifier = Modifier.padding(bottom = 16.dp)
            )

            OutlinedTextField(
                value = verifyCode,
                onValueChange = { if (it.length <= 6) verifyCode = it.filter { c -> c.isDigit() } },
                label = { Text("6-digit code") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.NumberPassword, imeAction = ImeAction.Go),
                keyboardActions = KeyboardActions(onGo = { submit() }),
            )
            Spacer(Modifier.height(16.dp))

            if (error != null) {
                Text(
                    error,
                    color = if (isSuccessMsg) PhazeSuccess else MaterialTheme.colorScheme.error,
                    fontSize = 13.sp,
                    textAlign = TextAlign.Center,
                )
                Spacer(Modifier.height(8.dp))
            }

            Button(
                onClick = { submit() },
                modifier = Modifier.fillMaxWidth().height(48.dp),
                enabled = verifyCode.length == 6,
            ) {
                Text("Verify Email", fontWeight = FontWeight.Bold)
            }
            Spacer(Modifier.height(12.dp))

            TextButton(onClick = { onResendVerification(email) }) {
                Text("Resend code")
            }
            TextButton(onClick = {
                verifyCode = ""
                onCancelVerification()
            }) {
                Text("Cancel")
            }

        } else if (mode == "link") {
            // ── Link code login ───────────────────────────────────────
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
                    Button(onClick = onScanQR, modifier = Modifier.weight(1f)) {
                        Text("📸 Camera")
                    }
                }
                if (onScanGallery != null) {
                    OutlinedButton(onClick = onScanGallery, modifier = Modifier.weight(1f)) {
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

            if (error != null) {
                Text(
                    error,
                    color = if (isSuccessMsg) PhazeSuccess else MaterialTheme.colorScheme.error,
                    fontSize = 13.sp,
                    textAlign = TextAlign.Center,
                )
                Spacer(Modifier.height(8.dp))
            }

            Button(
                onClick = { submit() },
                modifier = Modifier.fillMaxWidth().height(48.dp),
                enabled = linkCode.isNotBlank(),
            ) {
                Text("Sign in with Code", fontWeight = FontWeight.Bold)
            }
            Spacer(Modifier.height(12.dp))

            TextButton(onClick = {
                mode = "login"
                onCancelLinkLogin?.invoke()
            }) {
                Text("Back to sign in")
            }

        } else {
            // ── Login / Register ──────────────────────────────────────
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
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Email, imeAction = ImeAction.Next),
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

            if (error != null) {
                Text(
                    error,
                    color = if (isSuccessMsg) PhazeSuccess else MaterialTheme.colorScheme.error,
                    fontSize = 13.sp,
                    textAlign = TextAlign.Center,
                )
                Spacer(Modifier.height(8.dp))
            }

            val buttonEnabled = when (mode) {
                "register" -> username.isNotBlank() && password.isNotBlank() && email.contains("@")
                else -> username.isNotBlank() && password.isNotBlank()
            }

            Button(
                onClick = { submit() },
                modifier = Modifier.fillMaxWidth().height(48.dp),
                enabled = buttonEnabled,
            ) {
                Text(
                    when (mode) {
                        "login" -> "Sign In"
                        else -> "Create Account"
                    },
                    fontWeight = FontWeight.Bold
                )
            }
            Spacer(Modifier.height(12.dp))

            TextButton(onClick = { mode = if (mode == "login") "register" else "login"; username = ""; password = ""; email = "" }) {
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
