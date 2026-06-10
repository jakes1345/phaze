package world.phazechat.app.ui

import androidx.compose.material3.*
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

val PhazeBrand = Color(0xFF863BFF)
val PhazeBrandDark = Color(0xFFA677FF)
val PhazeSuccess = Color(0xFF10B981)
val PhazeDanger = Color(0xFFDC2626)

private val DarkColors = darkColorScheme(
    primary = PhazeBrandDark,
    onPrimary = Color.White,
    surface = Color(0xFF111111),
    onSurface = Color(0xFFF5F5F7),
    background = Color.Black,
    onBackground = Color(0xFFF5F5F7),
    surfaceVariant = Color(0xFF1C1C1E),
    onSurfaceVariant = Color(0xFF8E8E93),
    outline = Color(0xFF1C1C1E),
    error = PhazeDanger,
)

private val LightColors = lightColorScheme(
    primary = PhazeBrand,
    onPrimary = Color.White,
    surface = Color.White,
    onSurface = Color(0xFF1A1A1A),
    background = Color(0xFFF5F5F7),
    onBackground = Color(0xFF1A1A1A),
    surfaceVariant = Color(0xFFE5E5EA),
    onSurfaceVariant = Color(0xFF8E8E93),
    outline = Color(0xFFE5E5EA),
    error = PhazeDanger,
)

// "Skype 7" theme pack — classic white + Skype-blue retro look.
val SkypeBlue = Color(0xFF00AFF0)
private val Skype7Colors = lightColorScheme(
    primary = SkypeBlue,
    onPrimary = Color.White,
    surface = Color.White,
    onSurface = Color(0xFF1B2733),
    background = Color(0xFFE8EEF3),
    onBackground = Color(0xFF1B2733),
    surfaceVariant = Color(0xFFE3E9EE),
    onSurfaceVariant = Color(0xFF7A8A99),
    outline = Color(0xFFCFD8E0),
    error = PhazeDanger,
)

@Composable
fun PhazeTheme(theme: String = "dark", content: @Composable () -> Unit) {
    val scheme = when (theme) {
        "light" -> LightColors
        "skype7" -> Skype7Colors
        else -> DarkColors
    }
    MaterialTheme(
        colorScheme = scheme,
        typography = Typography(),
        content = content
    )
}
