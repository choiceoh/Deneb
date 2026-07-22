#!/usr/bin/env python3
"""
lightweight-model-ab.py — A/B battery for the *lightweight/tiny* model roles.

Compares two candidate models on Deneb's actual local-workhorse duties — the
text-only chores the lightweight/tiny roles perform (no tool calling). Each
task mirrors its production counterpart's CONTRACT (prompt shape, output
format, parser semantics), so a battery pass predicts production behavior:

  compaction  [lightweight] 한국어 대화 → 프로덕션 4-섹션 스켈레톤 요약.
              시스템 프롬프트는 compaction/llm.go의 compactionSystemPrompt 원문이며,
              섹션 준수(4개 헤더)까지 채점한다 — 산문 요약은 프로덕션 소비자가
              기대하는 구조가 아니다.
  extract     [tiny] 한국어 업무 메일 → 고정 스키마 JSON (gmail stage1과 동형).
              json_object 모드 강제: 프로덕션 callLocalLLMJSON(mailanalysis/pipeline.go)은
              formatless 폴백이 없으므로, JSON 모드를 400으로 거부하는 엔드포인트는
              폴백으로 채점하되 케이스 점수 40 하드캡 + `json_mode=rejected` 표기
              (콘텐츠가 좋아도 프로덕션을 깨는 후보를 통과시키지 않는다).
              ※ 오프라인 토이 스키마(보낸사람/의도/금액/기한/요청사항)는 프로덕션
              deal 스키마(isDeal/counterparty/docType/amount/date/dueDate/items/summary,
              mailanalysis/pipeline_extractors.go)와 다르다 — 충실도가 필요하면
              --eval-extract-url로 프로덕션 추출 경로를 태워라 (아래).
  title       [tiny] 대화 스니펫 → 짧은 한국어 명사구 제목 (세션 자동 제목과 동형)
  verdict     [lightweight] ① DONE/CONTINUE 한 단어 (장황함·부정 프로브)
              ② 프로덕션 goal judge 계약: goalJudgeSystem 원문 프롬프트에
              {"done":bool,"reason":str} 단일 라인 JSON을 요구하고,
              goal_task.go parseJudgeVerdict 미러로 done 필드 정오를 채점한다.
  triage      [tiny] 알림 YES/NO 트리아지 (server_http_event_ingest.go
              worthFullJudgment 미러 — max_tokens=4, "NO로 시작하지 않으면 전부
              YES"라는 프로덕션 파서 의미론 그대로. 수다형 tiny 모델은 여기서
              잘려 오판된다 — 그것이 프로덕션 동작이다).

Scoring is DETERMINISTIC (fact checklists, JSON parsing, length/format rules) —
no LLM judge — so the number is reproducible and argues for itself. Latency and
output-token counts ride along because verbosity is wall-clock on these paths
(compaction latency has caused real incidents).

Request shaping mirrors production text-role calls (pilot.CallRoleLLM):
  - system/user 분리 메시지 (단일 user 메시지 아님)
  - 후보별 기본 thinking-off extra body — modelrole.ThinkingOffDirectiveFor의 3분기
    미러 (단일 진실원: gateway-go/internal/ai/modelrole/thinking.go). 끄려면
    --no-thinking-off, 키가 겹치면 --extra-body-*가 이긴다.
Remaining deliberate gaps vs production: 배터리는 비스트리밍 HTTP(프로덕션은
Stream=true — 콘텐츠 동일, 벽시계는 어차피 전체 생성 포함), 서버측 timeout kwarg
미주입, 오프라인 extract 채점은 ```-펜스에 10점 감점(프로덕션 jsonutil은 펜스를
벗겨 소비하지만 bare JSON이 명시 계약이므로 스타일 감점 유지).

Usage (on the DGX host; both models served behind wormhole):
  python3 scripts/dev/lightweight-model-ab.py \
      --model-a qwen3.6-35b --model-b agents-a1 \
      [--base-url http://127.0.0.1:18800/v1] [--api-key-env WORMHOLE_TOKEN] [--rounds 1]

Optional production-path extract: 게이트웨이가 살아 있으면 extract 코퍼스를
실제 추출 경로(POST /api/eval/extract, kind=deal — 실프롬프트+jsonutil+후처리)로
라우팅해 "소비 결과"(DealInfo)를 채점한다:
  ... --eval-extract-url http://127.0.0.1:18789     # env DENEB_EVAL_EXTRACT_URL 자동 인식
(인증: $DENEB_CLIENT_TOKEN 또는 ~/.deneb/client_token. 미지정 시 오프라인 토이
스키마가 기본 — 인프라 없이 도는 대신 위의 스키마 갭을 감수한다.)

Self-test of the harness + scoring (no server needed):
  python3 scripts/dev/lightweight-model-ab.py --mock

Output: per-task table + one greppable line per model —
  AB_METRIC model=<name> total=<0-100> compaction=.. extract=.. title=.. verdict=.. triage=.. \
      json_mode=<ok|rejected|eval> avg_latency_ms=.. avg_out_tokens=..
역할별 서브버딕트 — 승격 판단은 역할 단위다 (model-roles.md 임무 배치:
extract·title·triage=tiny, compaction·verdict=lightweight):
  AB_VERDICT_TINY winner=<name|tie> margin=<pts>
  AB_VERDICT_LIGHTWEIGHT winner=<name|tie> margin=<pts>
and a final `AB_VERDICT winner=<name|tie> margin=<pts>[ json_mode_rejected=<models>]` line.

Role doctrine: docs/agent-rules/model-roles.md — tool-heavy roles are promoted via
SparkFleet run_tool_eval; THIS script is the counterpart for the text roles.
"""

import argparse
import json
import os
import sys

from lightweight_model_ab_runner import print_report, run_mock, run_model
from lightweight_model_ab_transport import resolve_client_token

def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--model-a", help="first model name as served (e.g. qwen3.6-35b)")
    ap.add_argument("--model-b", help="second model name as served (e.g. agents-a1)")
    ap.add_argument("--base-url", default="http://127.0.0.1:18800/v1", help="OpenAI-compatible base (default: wormhole)")
    ap.add_argument("--base-url-b", default="", help="model-b용 별도 엔드포인트 (기본: --base-url과 동일 — 후보를 wormhole에 태우기 전 raw vLLM으로 직접 잴 때)")
    ap.add_argument("--api-key-env", default="WORMHOLE_TOKEN", help="env var holding the bearer token")
    ap.add_argument("--rounds", type=int, default=1, help="repeat the battery N times per model")
    ap.add_argument("--timeout", type=float, default=120.0, help="per-call timeout seconds")
    ap.add_argument(
        "--extra-body-a", default="", help='model-a 요청 body에 병합할 JSON (예: \'{"chat_template_kwargs":{"enable_thinking":false}}\') — 자동 thinking-off와 키 충돌 시 이 값이 이긴다'
    )
    ap.add_argument("--extra-body-b", default="", help="model-b 요청 body에 병합할 JSON — 사고형 후보의 서빙 옵션 실험 등")
    ap.add_argument(
        "--no-thinking-off", action="store_true",
        help="후보별 기본 thinking-off 셰이핑(modelrole.ThinkingOffDirectiveFor 미러)을 끈다 — chat_template_kwargs를 거부하는 비-vLLM 엔드포인트용",
    )
    ap.add_argument(
        "--eval-extract-url", default=os.environ.get("DENEB_EVAL_EXTRACT_URL", ""),
        help="게이트웨이 base URL — 지정 시 extract 케이스를 프로덕션 추출 경로(POST /api/eval/extract, kind=deal)로 라우팅. env DENEB_EVAL_EXTRACT_URL로도 지정 가능",
    )
    ap.add_argument(
        "--client-token-env", default="DENEB_CLIENT_TOKEN",
        help="eval-extract 인증 토큰을 담은 env (비었으면 ~/.deneb/client_token 파일 폴백)",
    )
    ap.add_argument("--dump", default="", help="모든 케이스의 모델 출력 원문을 JSON으로 저장할 경로 (정성 리뷰용)")
    ap.add_argument("--mock", action="store_true", help="run the harness self-test against built-in mock models")
    args = ap.parse_args()

    if args.mock:
        return run_mock()
    if not args.model_a or not args.model_b:
        ap.error("--model-a and --model-b are required (or use --mock)")
    api_key = os.environ.get(args.api_key_env, "")
    if not api_key:
        # Log only that auth is missing — never interpolate key/env names that
        # CodeQL (and operators) treat as credential material.
        print("warning: API key env is empty — sending without auth", file=sys.stderr)
    extra_a = json.loads(args.extra_body_a) if args.extra_body_a else None
    extra_b = json.loads(args.extra_body_b) if args.extra_body_b else None
    eval_token = resolve_client_token(args.client_token_env) if args.eval_extract_url else ""
    if args.eval_extract_url and not eval_token:
        print("warning: eval-extract token not found — requests may 401", file=sys.stderr)
    dump = [] if args.dump else None
    common = {
        "eval_extract_url": args.eval_extract_url,
        "eval_token": eval_token,
        "thinking_off": not args.no_thinking_off,
    }
    results = [
        run_model(args.base_url, api_key, args.model_a, args.rounds, args.timeout, extra_a, dump, **common),
        run_model(args.base_url_b or args.base_url, api_key, args.model_b, args.rounds, args.timeout, extra_b, dump, **common),
    ]
    print_report(results)
    if args.dump:
        with open(args.dump, "w", encoding="utf-8") as f:
            json.dump(dump, f, ensure_ascii=False, indent=1)
        print(f"outputs dumped: {args.dump}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
