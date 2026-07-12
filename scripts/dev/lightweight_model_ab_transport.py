"""OpenAI-compatible and production extract transports for lightweight-model-ab."""

import json
import os
import sys
import time
import urllib.error
import urllib.request

# --- Per-candidate request shaping (mirror of modelrole.ThinkingOffDirectiveFor) ---


def _is_reasoning_model(model: str) -> bool:
    """modelrole.ProfileFor(profile.go)의 reasoning 분류 미러 — 항상 사고 채널을
    여는(끌 수 없는) 모델 패밀리."""
    m = model.lower()
    if "step3" in m or "step-3" in m:
        return True
    if ("qwen3" in m or "qwen36" in m or "qwen35" in m) and "instruct" not in m:
        return True
    return any(k in m for k in ("qwq", "deepseek-r1", "deepseek-reasoner", "gpt-oss"))


def thinking_off_extra_body(model: str):
    """프로덕션 텍스트 콜(pilot.CallRoleLLM)이 자동 적용하는 thinking-off 셰이핑의
    Python 미러. 단일 진실원: gateway-go/internal/ai/modelrole/thinking.go
    (ThinkingOffDirectiveFor) — 그쪽 분기가 바뀌면 여기도 갱신할 것. 3분기:

      1) dual-mode deepseek-v4 → chat_template_kwargs.thinking=false (템플릿 토글 철자)
      2) 끌 수 없는 사고형(step3/qwq/r1/비-instruct qwen3 등) → None
         (enable_thinking 부착은 thinking-only 템플릿에서 400 위험)
      3) 그 외 비사고형 → chat_template_kwargs.enable_thinking=false (Qwen 계열 철자)

    프로바이더 게이트(modelcaps.ServesVllmBacked)는 생략: 이 배터리의 문서화된
    대상이 wormhole/raw vLLM이라 항상 vLLM-backed로 간주한다. 클라우드 후보처럼
    chat_template_kwargs를 거부하는 엔드포인트는 --no-thinking-off로 끈다.
    """
    m = model.lower()
    if "deepseek-v4" in m or "deepseek_v4" in m:
        return {"chat_template_kwargs": {"thinking": False}}
    if _is_reasoning_model(m):
        return None
    return {"chat_template_kwargs": {"enable_thinking": False}}


# --- OpenAI-compatible client (stdlib only) ---------------------------------


def chat_once(base_url, api_key, model, system, user, max_tokens, timeout, response_format=None, extra_body=None):
    payload_body = {
        "model": model,
        # 프로덕션 텍스트 콜(CallRoleLLM/callLocalLLMJSON)과 동일한 system/user 분리 —
        # 단일 user 메시지로 뭉치면 후보가 프로덕션과 다른 프롬프트 형상으로 측정된다.
        "messages": [
            {"role": "system", "content": system},
            {"role": "user", "content": user},
        ],
        "temperature": 0,
        "max_tokens": max_tokens,
    }
    if response_format:
        payload_body["response_format"] = response_format
    if extra_body:
        # 후보별 thinking-off 셰이핑 + 운영자 --extra-body-* 병합 결과.
        payload_body.update(extra_body)
    headers = {"Content-Type": "application/json"}
    if api_key:  # 빈 Bearer 헤더는 무인증과 다르게 취급하는 서버가 있다 — 아예 생략
        headers["Authorization"] = f"Bearer {api_key}"
    req = urllib.request.Request(
        base_url.rstrip("/") + "/chat/completions",
        data=json.dumps(payload_body).encode("utf-8"),
        headers=headers,
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=timeout) as resp:  # noqa: S310 — local/tailnet endpoints only
        payload = json.load(resp)
    content = payload["choices"][0]["message"]["content"] or ""
    usage = payload.get("usage") or {}
    out_tokens = usage.get("completion_tokens") or max(1, len(content) // 3)
    return content, out_tokens


# 재시도할 가치가 있는 상태만 — 4xx(400/401/404 등)는 재시도해도 같은 답이고
# 서버에 이중 부하만 준다.
TRANSIENT_HTTP = {408, 429, 500, 502, 503, 504}


def chat_with_retry(base_url, api_key, model, system, user, max_tokens, timeout, response_format=None, extra_body=None):
    # 레이턴시는 재시도·백오프를 포함한 벽시계 전체 — 플레이키한 모델이 실패 시간을
    # 숨기고 건강한 avg_latency_ms를 보고하면 운영 판단이 왜곡된다.
    start = time.monotonic()
    json_rejected = False

    def attempt():
        nonlocal json_rejected
        try:
            return chat_once(base_url, api_key, model, system, user, max_tokens, timeout, response_format, extra_body)
        except urllib.error.HTTPError as e:
            # guided-decoding 미지원 서버의 response_format 거부 → 형식 없이 1회
            # 폴백하되 반드시 json_rejected로 표면화한다: 프로덕션 gmail stage1
            # (callLocalLLMJSON, mailanalysis/pipeline.go)은 항상 json_object를 보내고
            # formatless 폴백이 없으므로, JSON 모드를 거부하는 엔드포인트는 이
            # 배터리를 무벌점 통과해도 프로덕션에선 매 추출이 실패한다.
            if response_format is not None and e.code == 400:
                json_rejected = True
                print(
                    f"  ! {model}: response_format rejected (400) — falling back without JSON mode (score capped)",
                    file=sys.stderr,
                )
                return chat_once(base_url, api_key, model, system, user, max_tokens, timeout, None, extra_body)
            raise

    def done(content, out_tokens):
        return content, int((time.monotonic() - start) * 1000), out_tokens, json_rejected

    try:
        return done(*attempt())
    except urllib.error.HTTPError as e:
        if e.code not in TRANSIENT_HTTP:
            raise
        print(f"  ! transient HTTP {e.code} for {model} — retrying once", file=sys.stderr)
    except (urllib.error.URLError, TimeoutError, OSError) as e:
        print(f"  ! transient error for {model}: {e} — retrying once", file=sys.stderr)
    time.sleep(2)
    return done(*attempt())


# --- Production-path extract (POST /api/eval/extract) -----------------------


def resolve_client_token(env_name: str) -> str:
    """eval-extract 인증 토큰: env 우선, 없으면 게이트웨이 호스트의
    ~/.deneb/client_token 파일 (clientauth와 동일 소스)."""
    tok = os.environ.get(env_name, "").strip()
    if tok:
        return tok
    try:
        with open(os.path.expanduser("~/.deneb/client_token"), encoding="utf-8") as f:
            return f.read().strip()
    except OSError:
        return ""


def eval_extract_once(eval_url, token, model, mail, timeout):
    """프로덕션 추출 경로 실행 (server_http_eval.go handleEvalExtract, kind=deal):
    실제 프롬프트 + jsonutil 파싱 + 후처리를 통과한 '소비 결과'를 반환한다.
    result가 None이면 "not a deal" 판정."""
    payload = json.dumps({"kind": "deal", "input": mail, "model": model}).encode("utf-8")
    headers = {"Content-Type": "application/json"}
    if token:
        headers["X-Deneb-Client-Token"] = token  # clientauth.Header
    req = urllib.request.Request(
        eval_url.rstrip("/") + "/api/eval/extract", data=payload, headers=headers, method="POST"
    )
    with urllib.request.urlopen(req, timeout=timeout) as resp:  # noqa: S310 — local/tailnet gateway only
        body = json.load(resp)
    if not body.get("ok"):
        raise RuntimeError(f"eval extract failed: {body.get('error', 'unknown')}")
    return body.get("result")


def eval_extract_with_retry(eval_url, token, model, mail, timeout):
    start = time.monotonic()
    try:
        result = eval_extract_once(eval_url, token, model, mail, timeout)
    except (urllib.error.URLError, TimeoutError, OSError, RuntimeError) as e:
        print(f"  ! transient eval-extract error: {e} — retrying once", file=sys.stderr)
        time.sleep(2)
        result = eval_extract_once(eval_url, token, model, mail, timeout)
    return result, int((time.monotonic() - start) * 1000)
