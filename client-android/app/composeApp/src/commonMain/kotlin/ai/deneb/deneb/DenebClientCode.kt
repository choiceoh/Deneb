package ai.deneb.deneb

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

/**
 * Coding-mode surface of [DenebGatewayClient] (`miniapp.code.*`): the rail of
 * git-worktree sessions and their actions. Each session is one worktree/branch;
 * its chat (session key `code:<id>`) drives the edits, so the screen lists/acts
 * here and hands off to the chat path to keep working.
 *
 * The handlers return plain JSON objects (map payloads), so the client decodes
 * with hand-written @Serializable DTOs — the same approach the Andromeda desktop
 * client uses for this surface. NOT //deneb:wire generated: the domain types live
 * in another Go package (kotlin-models-gen only scans the handler package), and
 * this surface was deliberately kept map-based + hand-typed on both clients.
 *
 * Reads return null on a transport failure so the screen can offer retry instead
 * of a misleading "empty"; writes return a Korean error message (or null on OK)
 * via [rpcWrite], matching the calendar/model write idiom.
 */

@Serializable
data class CodeRepo(
    val owner: String = "",
    val name: String = "",
)

@Serializable
data class CodeCheckpoint(
    val sha: String = "",
    val summary: String = "",
    val at: String = "",
)

@Serializable
data class CodeSession(
    val id: String = "",
    val repo: CodeRepo = CodeRepo(),
    val title: String = "",
    val status: String = "",
    val branch: String = "",
    val dir: String = "",
    val chatSessionKey: String = "",
    val checkpoints: List<CodeCheckpoint> = emptyList(),
    val createdAt: String = "",
    val updatedAt: String = "",
)

@Serializable
data class CodeVerifyStep(
    val label: String = "",
    val cmd: String = "",
    val ok: Boolean = false,
    val output: String = "",
)

@Serializable
data class CodeVerifyResult(
    val kind: String = "",
    val passed: Boolean = false,
    val steps: List<CodeVerifyStep> = emptyList(),
)

/** miniapp.code.verify response: the updated session + the step-by-step result. */
@Serializable
data class CodeVerify(
    val session: CodeSession = CodeSession(),
    val result: CodeVerifyResult = CodeVerifyResult(),
)

@Serializable
private data class CodeSessionsEnvelope(val sessions: List<CodeSession> = emptyList())

@Serializable
private data class CodeReposEnvelope(val repos: List<CodeRepo> = emptyList())

@Serializable
private data class CodeSessionEnvelope(val session: CodeSession = CodeSession())

@Serializable
private data class CodePrEnvelope(val url: String = "")

/** All live coding sessions (the rail); closed ones are filtered server-side.
 *  Null on a fetch failure. */
suspend fun DenebGatewayClient.fetchCodeSessions(): List<CodeSession>? = callRpc<CodeSessionsEnvelope>("miniapp.code.sessions", buildJsonObject {})?.sessions

/** The operator's GitHub repos for the start picker. Empty (not null) when gh is
 *  unauthenticated — the form falls back to manual owner/repo entry. Null only on
 *  a transport failure. */
suspend fun DenebGatewayClient.fetchCodeRepos(): List<CodeRepo>? = callRpc<CodeReposEnvelope>("miniapp.code.repos", buildJsonObject {})?.repos

/** Start a worktree + session for [owner]/[name]. A blank [taskId]/[title] is
 *  auto-generated server-side. Returns the new session (its `chatSessionKey`
 *  opens the chat) or null on failure. */
suspend fun DenebGatewayClient.startCodeSession(
    owner: String,
    name: String,
    taskId: String? = null,
    title: String? = null,
): CodeSession? = callRpc<CodeSessionEnvelope>(
    "miniapp.code.start",
    buildJsonObject {
        put("owner", owner)
        put("name", name)
        if (!taskId.isNullOrBlank()) put("taskId", taskId)
        if (!title.isNullOrBlank()) put("title", title)
    },
)?.session

/** One session's current state by id. Null on miss/failure. */
suspend fun DenebGatewayClient.fetchCodeStatus(id: String): CodeSession? = callRpc<CodeSessionEnvelope>("miniapp.code.status", buildJsonObject { put("id", id) })?.session

/** The pull-request URL for a session's branch, or "" when none exists yet (a
 *  normal state, never an error). Null only on a transport failure. */
suspend fun DenebGatewayClient.fetchCodePrUrl(id: String): String? = callRpc<CodePrEnvelope>("miniapp.code.pr", buildJsonObject { put("id", id) })?.url

/** Run the worktree's build/test and flip the session status. Returns the
 *  updated session + step-by-step result, or null on failure. */
suspend fun DenebGatewayClient.verifyCodeSession(id: String): CodeVerify? = callRpc<CodeVerify>("miniapp.code.verify", buildJsonObject { put("id", id) })

/** Commit the worktree's current changes as a checkpoint with a Korean summary.
 *  Returns the updated session, or null on failure. */
suspend fun DenebGatewayClient.checkpointCodeSession(id: String, summary: String? = null): CodeSession? = callRpc<CodeSessionEnvelope>(
    "miniapp.code.checkpoint",
    buildJsonObject {
        put("id", id)
        if (!summary.isNullOrBlank()) put("summary", summary)
    },
)?.session

/** Step back one checkpoint (or discard uncommitted edits). Returns the updated
 *  session, or null on failure. */
suspend fun DenebGatewayClient.undoCodeSession(id: String): CodeSession? = callRpc<CodeSessionEnvelope>("miniapp.code.undo", buildJsonObject { put("id", id) })?.session

/** Push the session's branch to GitHub. Null on success, a Korean error on failure. */
suspend fun DenebGatewayClient.pushCodeSession(id: String): String? = rpcWrite("miniapp.code.push", buildJsonObject { put("id", id) })

/** Delete the worktree + branch (destructive). Null on success, a Korean error on failure. */
suspend fun DenebGatewayClient.discardCodeSession(id: String): String? = rpcWrite("miniapp.code.discard", buildJsonObject { put("id", id) })

/** Archive the session (hide from the rail; keep the worktree/branch + any PR).
 *  Null on success, a Korean error on failure. */
suspend fun DenebGatewayClient.closeCodeSession(id: String): String? = rpcWrite("miniapp.code.close", buildJsonObject { put("id", id) })
