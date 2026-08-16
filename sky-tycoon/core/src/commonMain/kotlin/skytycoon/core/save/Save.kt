package skytycoon.core.save

import kotlinx.serialization.json.Json
import skytycoon.core.model.GameState

/** 세이브는 그냥 JSON 한 덩어리다. 파일로 내보내고 다시 불러오기 쉽다. */
object Save {
    const val FORMAT_VERSION = 1

    private val json = Json {
        ignoreUnknownKeys = true
        encodeDefaults = true
        prettyPrint = false
    }

    private val pretty = Json {
        ignoreUnknownKeys = true
        encodeDefaults = true
        prettyPrint = true
    }

    fun encode(state: GameState, indent: Boolean = false): String =
        (if (indent) pretty else json).encodeToString(GameState.serializer(), state)

    fun decode(text: String): GameState =
        json.decodeFromString(GameState.serializer(), text)

    fun decodeOrNull(text: String): GameState? = runCatching { decode(text) }.getOrNull()
}
