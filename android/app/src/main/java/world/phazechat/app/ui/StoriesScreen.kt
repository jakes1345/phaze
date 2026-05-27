package world.phazechat.app.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import coil.compose.AsyncImage

data class Story(
    val id: String,
    val author: String,
    val mediaUrl: String,
    val mediaKind: String = "image",
    val createdAt: String = "",
)

@Composable
fun StoriesRow(
    stories: List<Story>,
    me: String,
    onViewStory: (String) -> Unit,
    onAddStory: () -> Unit,
) {
    val byAuthor = stories.groupBy { it.author }
    val authors = byAuthor.keys.toList()

    LazyRow(
        contentPadding = PaddingValues(horizontal = 12.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        item {
            Column(
                horizontalAlignment = Alignment.CenterHorizontally,
                modifier = Modifier.clickable { onAddStory() }.width(64.dp),
            ) {
                Box(
                    modifier = Modifier
                        .size(56.dp)
                        .clip(CircleShape)
                        .background(MaterialTheme.colorScheme.surfaceVariant),
                    contentAlignment = Alignment.Center,
                ) {
                    Text("+", fontSize = 24.sp, fontWeight = FontWeight.Bold, color = PhazeBrandDark)
                }
                Spacer(Modifier.height(4.dp))
                Text("Your story", fontSize = 11.sp, maxLines = 1, overflow = TextOverflow.Ellipsis, textAlign = TextAlign.Center)
            }
        }
        items(authors) { author ->
            Column(
                horizontalAlignment = Alignment.CenterHorizontally,
                modifier = Modifier.clickable { onViewStory(author) }.width(64.dp),
            ) {
                Box(
                    modifier = Modifier
                        .size(56.dp)
                        .clip(CircleShape)
                        .border(2.dp, PhazeBrand, CircleShape)
                        .background(MaterialTheme.colorScheme.surfaceVariant),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(author.firstOrNull()?.uppercase() ?: "?", fontSize = 20.sp, fontWeight = FontWeight.Bold, color = PhazeBrandDark)
                }
                Spacer(Modifier.height(4.dp))
                Text(if (author == me) "You" else author, fontSize = 11.sp, maxLines = 1, overflow = TextOverflow.Ellipsis, textAlign = TextAlign.Center)
            }
        }
    }
}

@Composable
fun StoryViewer(
    stories: List<Story>,
    onClose: () -> Unit,
) {
    var index by remember { mutableIntStateOf(0) }
    val story = stories.getOrNull(index)

    Box(
        modifier = Modifier.fillMaxSize().background(Color.Black).clickable {
            if (index < stories.size - 1) index++ else onClose()
        },
    ) {
        if (story != null) {
            AsyncImage(
                model = "https://phazechat.world${story.mediaUrl}",
                contentDescription = null,
                modifier = Modifier.fillMaxSize(),
                contentScale = ContentScale.Fit,
            )
            // Top bar
            Row(
                modifier = Modifier.fillMaxWidth().padding(16.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Avatar(story.author, 32)
                Spacer(Modifier.width(8.dp))
                Text(story.author, color = Color.White, fontWeight = FontWeight.Bold, fontSize = 14.sp)
                Spacer(Modifier.weight(1f))
                TextButton(onClick = onClose) { Text("✕", color = Color.White, fontSize = 18.sp) }
            }
            // Progress dots
            Row(
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp).align(Alignment.TopCenter),
                horizontalArrangement = Arrangement.spacedBy(2.dp),
            ) {
                stories.forEachIndexed { i, _ ->
                    Box(
                        modifier = Modifier
                            .weight(1f)
                            .height(2.dp)
                            .background(if (i <= index) Color.White else Color.White.copy(alpha = 0.3f))
                    )
                }
            }
        }
    }
}
