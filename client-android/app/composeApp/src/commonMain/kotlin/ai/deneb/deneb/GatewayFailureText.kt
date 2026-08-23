package ai.deneb.deneb

/**
 * Korean text for a chat send that failed before an answer existed.
 *
 * The bubble used to carry `error.message` verbatim, which is developer English
 * written for a log — "chat HTTP 401", or ktor's
 * "Connect timeout has expired [url=http://100.x.x.x:18789/...]", which also puts
 * the gateway address in the transcript. What the reader needs is which of three
 * things happened and whether it is worth retrying, so the status code is
 * translated and the raw text goes to the log instead.
 */
internal fun gatewayFailureText(error: Throwable): String = when (val status = gatewayHttpStatus(error.message)) {
    401, 403 -> "⚠️ 게이트웨이가 토큰을 거부했습니다. 설정에서 클라이언트 토큰을 확인해 주세요."
    null -> "⚠️ 게이트웨이에 연결하지 못했습니다. 네트워크와 게이트웨이 주소를 확인해 주세요."
    in 500..599 -> "⚠️ 게이트웨이가 응답하지 못했습니다 (오류 $status). 잠시 후 다시 보내 주세요."
    else -> "⚠️ 게이트웨이가 요청을 처리하지 못했습니다 (오류 $status)."
}

/**
 * The status code out of the transport's own `"chat HTTP 503"` / `"stream HTTP 401"`
 * messages, or null for anything else (connect failure, timeout, parse error) —
 * those never reached a response, which is exactly the distinction the caller needs.
 */
internal fun gatewayHttpStatus(message: String?): Int? {
    val marker = "HTTP "
    val at = message?.indexOf(marker) ?: return null
    if (at < 0) return null
    return message.drop(at + marker.length).takeWhile { it.isDigit() }.toIntOrNull()
}
