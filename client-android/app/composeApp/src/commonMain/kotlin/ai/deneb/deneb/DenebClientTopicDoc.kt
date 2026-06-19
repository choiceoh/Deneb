package ai.deneb.deneb

import ai.deneb.deneb.generated.TopicDocOut
import ai.deneb.deneb.generated.TopicDocWriteOut
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

// RPC facade for the per-topic knowledge doc (workspace/topics/<key>.md), injected
// into the system prompt's Static block. Mirrors DenebClientPrompts.kt: the prompts
// corner reuses its editor UI for this single topic document, but routes load/save
// here because the topic is file-backed (topics/<key>.md), not prompt-override-JSON
// backed — saving it through the prompts store would never reach the .md, breaking
// injection. The gateway resolves the file from the current topic key; the client
// only ever sends content (+ applyNow), never a path.

/** Reads the current topic's doc. Returns an empty-content [TopicDocOut] (key + name
 *  populated, content "") when the file does not exist yet, so the editor opens blank
 *  rather than erroring. null only on transport/unavailable failure. */
suspend fun DenebGatewayClient.fetchTopicDoc(): TopicDocOut? = callRpc<TopicDocOut>("miniapp.topicdocs.read_current", buildJsonObject {})

/** Upserts the current topic's doc. [applyNow] drops the session-frozen topic
 *  snapshots so the edit lands this session (the RPC analog of slash `--now`);
 *  the default defers to next session to keep the Static prompt cache stable.
 *  The gateway rejects empty/oversized content, so guard those before calling. */
suspend fun DenebGatewayClient.saveTopicDoc(content: String, applyNow: Boolean = false): TopicDocWriteOut? = callRpc<TopicDocWriteOut>(
    "miniapp.topicdocs.write_current",
    buildJsonObject {
        put("content", content)
        put("applyNow", applyNow)
    },
)
