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

@Composable
fun PhazeTheme(darkTheme: Boolean = true, content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = if (darkTheme) DarkColors else LightColors,
        typography = Typography(),
        content = content
    )
}
