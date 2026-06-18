---
description: 로컬 GPU 사이드카 모델 운영 현황 (OCR/ASR/추출/임베딩) — 엔드포인트·기동·배선·폴백
globs: ["gateway-go/internal/pipeline/chat/tools/paddleocr.go", "gateway-go/internal/pipeline/chat/tools/asr.go", "gateway-go/internal/pipeline/chat/tools/gmail_attachment.go", "gateway-go/internal/pipeline/chat/web/web_html.go", "gateway-go/internal/ai/modelrole/**", "gateway-go/internal/pipeline/pilot/**", "gateway-go/cmd/wormhole/**", "scripts/deploy/start-wormhole.sh", "scripts/deploy/wormhole.service"]
---

# Sidecar Models (GPU 부가 모델 운영 현황)

> Deneb 는 메인 챗 LLM 외에도 **로컬 GPU(DGX Spark, gx10)에서 상주 서빙되는 전용 모델들**을 호출한다. 대부분 vLLM 의 OpenAI 호환 `/v1` 엔드포인트지만, 일부(VibeVoice-ASR)는 전용 서비스로 상주한다. 외부 API 호출을 피하고 단일 머신에서 자급한다는 프로젝트 원칙(로컬 추론 우선)을 따른다. 이 파일은 "어떤 모델이, 어디서, 어떻게" 돌아가는지의 단일 진실원이다.

## 현황 표

| 모델 | 역할 | 기본 엔드포인트 | 코드 진입점 | 비고 |
|---|---|---|---|---|
| **PaddleOCR-VL-1.6** (0.9B) | 문서 OCR (스캔 PDF·이미지 첨부) | `http://127.0.0.1:18011/v1` | `chat/tools/paddleocr.go` | 상주 서빙. tesseract 폴백 있음. ↓ 상세 |
| **VibeVoice-ASR** (9B) | 음성 전사 + 화자분리 + 타임스탬프 (최대 60분·50+개 언어·한국어) | `http://127.0.0.1:18013` (`POST /v1/transcribe`, OpenAI 비호환) | `chat/tools/asr.go` | transformers+FastAPI 상주. 핫워드로 고유명사 교정. `miniapp.capture.audio` 캡처 배선(#1847). ↓ 상세 |
| 메인 챗 LLM | 대화/분석/도구호출 | provider config (Anthropic/OpenRouter/vLLM 등) | `pipeline/chat/run_provider.go` | modelrole `main`. 로컬일 때 기본 `http://127.0.0.1:8000/v1` |
| lightweight 서브 LLM | gmailpoll/genesis/pilot 등 잡일꾼 | modelrole `lightweight` | `pipeline/pilot/localai.go` | 메인보다 작은 모델, 백그라운드 작업용 |
| NuExtract3-FP8 | 구조화 추출 (스키마 기반) | (config-driven, 코드 하드코딩 없음) | — | `~/models/NuExtract3-FP8`. 현재 게이트웨이 코드에서 직접 참조 없음 |
| granite-embedding-311m / nomic-embed-text-v2-moe / BGE-M3 | 임베딩 (압축/검색) | config-driven | compaction 임베딩 폴백 경로 | `~/models/` 보관 |

> **modelrole 기본값**: `gateway-go/internal/ai/modelrole/registry.go` — `DefaultVllmBaseURL = "http://127.0.0.1:8000/v1"`, `DefaultVllmModel = "gemma4"`. 역할(main/lightweight/fallback)별 실제 모델은 `~/.deneb/deneb.json` 의 provider/modelRole 설정이 결정한다. 코드는 이름을 하드코딩하지 않는다.

---

## PaddleOCR-VL (Deneb 의 OCR 엔진)

### 무엇 / 왜
- 0.9B 비전-언어 모델 (NaViT 인코더 + ERNIE-4.5-0.3B). 한국어 업무 문서(표·수식·혼합 숫자·도장)에서 tesseract 대비 압도적 정확도. OmniDocBench v1.6 SOTA.
- BF16 가중치라 unified memory 를 거의 안 먹어 메인 LLM 과 공존 가능. 워밍 후 **~1s/page**, 콜드 첫 요청만 ~19s(CUDA-graph 워밍업, 부팅 1회).

### 서버 (상주)
- 런처: **`~/start-paddleocr-vl.sh`** (호스트, **레포 밖** — 배포 머신 로컬 파일). 컨테이너 `paddleocr-vl`, port **18011**, `--restart unless-stopped`.
- 이미지: 로컬 `vllm-node:latest` (vLLM 0.21.1, 2026-05-26 빌드) 가 `PaddleOCRVLForConditionalGeneration` 을 **네이티브 등록** → PaddlePaddle 프레임워크 불필요. 순수 OpenAI 호환.
- 가중치: `~/models/PaddleOCR-VL-1.6` (hf download). 1.82 GiB BF16.
- **메모리 예산 (2026-06-02 하향)**: `--gpu-memory-utilization 0.03` + `--max-model-len 8192`. unified 점유 **~2.8GB** (이전 0.15 는 122GB 의 15% ≈ **16GB 를 통째로 KV 풀로 선점** → 0.3B 급 디코더엔 10GB+ 가 빈 예약 낭비였음). 0.03 budget(~3.6GB) − 가중치 1.82 − 비-torch 오버헤드 ~1.6 = KV ~0.47GB → 8192 토큰 한 시퀀스(~0.14GB)에 동시 3.37x. OCR 은 페이지 단위라 16384 컨텍스트 불필요. ⚠️ **0.03 에서 max-model-len 16384 는 기동 실패** (KV 0.28GB < 필요 0.28GB, 간발의 차) — 8192 로 낮춰야 들어맞음. 런처가 `GPU_MEM_UTIL`·`PADDLEOCR_MAX_MODEL_LEN` env override 지원.

### 코드 통합
- `gateway-go/internal/pipeline/chat/tools/paddleocr.go`:
  - `paddleOCR(ctx, img, task)` — `/v1/chat/completions` 에 `image_url`(base64 data URI) + 태스크 프롬프트 전송. 태스크: `"OCR:"` / `"Table Recognition:"` / `"Formula Recognition:"` / `"Chart Recognition:"`.
  - `ocrImageBytes(ctx, img)` — **단일 OCR 진입점**. PaddleOCR-VL 우선, 실패 시 tesseract 폴백.
- `gmail_attachment.go` 의 `imageOCR`(이미지 첨부)와 `pdfOCR` 페이지 루프(스캔 PDF)가 `ocrImageBytes` 경유.
- **폴백 설계**: 서버가 꺼져 있으면 connection refused 로 즉시 실패 → tesseract(kor+eng) 로 graceful degradation. 즉 OCR 은 서버 없어도 깨지지 않고 품질만 낮아진다.
- **엔드포인트 override**: 환경변수 `DENEB_OCR_VL_URL` (기본 `http://127.0.0.1:18011`). 테스트/비표준 배포용.

### 운영 명령
```bash
# 기동/재기동
~/start-paddleocr-vl.sh
# 상태
docker ps --filter name=paddleocr-vl
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:18011/health   # 200 이면 정상
# 로그
docker logs --tail 50 paddleocr-vl
```

### 라이브 검증 (Go 경로)
```bash
# CI 에선 skip (GPU 없음). 호스트에서 실제 서버 대상 e2e:
DENEB_OCR_VL_LIVE=1 DENEB_OCR_VL_IMG=/path/to.png DENEB_OCR_VL_URL=http://127.0.0.1:18011 \
  go test -run TestPaddleOCR_Live ./internal/pipeline/chat/tools/
```

---

## VibeVoice-ASR (음성 전사 엔진)

### 무엇 / 왜
- 9B 음성 ASR 모델 (VibeVoice 음향/의미 토크나이저 24kHz + Qwen2 디코더, MIT). **ASR + 화자분리 + 타임스탬프를 단일 패스**로 출력 (최대 60분/64K 토큰, 50+개 언어·한국어).
- Deneb 비서 모드의 회의/통화/음성메모 캡처용. Whisper + pyannote 2단 파이프라인을 단일 모델로 대체. 검증: 영어 2화자 화자분리·타임스탬프 정확, 한국어 일반어 CER≈0, RTF ~0.5–0.7.

### 서버 (상주)
- 런처: **`~/start-vibevoice-asr.sh`** (호스트, **레포 밖**). 컨테이너 `vibevoice-asr`, port **18013**, `--restart unless-stopped`.
- 서빙: PaddleOCR 와 달리 **vLLM serve 아님.** MS 의 vLLM 플러그인은 vLLM v0.14.1 타깃인데 로컬 `vllm-node` 는 0.21.1 → transformers eager + 얇은 FastAPI 래퍼로 상주 (단일 사용자엔 충분).
- 이미지: `vibevoice-asr:latest` = `FROM vllm-node:latest` + accelerate/soundfile/soxr/ffmpeg/fastapi. 빌드 컨텍스트 `~/vibevoice-asr/` (Dockerfile + `vibevoice_server.py`).
- 가중치: `~/models/VibeVoice-ASR-HF` (16GB BF16, hf download — 이 호스트에선 `HF_HUB_DISABLE_XET=1` 필요). 로드 ~80s(부팅 1회).
- **커밋 헤드룸**: strict overcommit(`vm.overcommit_memory=2`) 호스트라 16GB 상주를 위해 `vm.overcommit_ratio` 50→80 영구 상향(`/etc/sysctl.d/99-deneb-vibevoice.conf`). OOM 가드(min_free_kbytes/watermark)는 미변경.

### API
- `POST /v1/transcribe` (**OpenAI 비호환**): multipart `file` 또는 form `path`, 선택 `hotwords`·`chunk_size`·`max_new_tokens`. 응답 = `segments`(speaker/start/end/content) + `transcription` + `rtf`. `GET /health` 로 readiness.
- Telegram 음성(`.oga`/opus)·m4a·mp3·wav 자동 디코딩 (soundfile → ffmpeg 폴백).
- **핫워드 권장**: 한국어 일반어는 사실상 무오류지만 고유명사(거래처·제품·인명)는 bare ASR 이 틀린다 → Deneb 연락처/거래 KB 를 `hotwords` 로 주입하면 교정됨 (검증 완료: 탑솔라/데네브).

### 코드 통합 (#1847)
- `gateway-go/internal/pipeline/chat/tools/asr.go`:
  - `transcribeAudio(ctx, audio, filename, hotwords)` — `/v1/transcribe` 에 멀티파트 `file`(+ 선택 `hotwords`) 전송, `{segments, transcription}` 파싱. segment `speaker` 가 문자열 라벨 또는 숫자 인덱스 **둘 다** 와서 `flexStr` 로 수용(라이브 테스트가 잡은 함정).
  - `transcribeAudioText(ctx, audio, mimeType)` — **단일 전사 진입점**. 화자분리+타임스탬프로 포맷(`[mm:ss 화자N] …`), segment 없으면 flat transcription 폴백.
- `chat/tools/asr_export.go` 의 `TranscribeAudio` 래퍼가 패키지-프라이빗 진입점 노출 → `miniapp.capture.audio` 브리지 RPC(`handler/chat/miniapp_bridge.go`, `deps.Transcribe != nil` 일 때만 등록)가 공유 녹음을 전사해 한 agent turn 실행. PaddleOCR 의 `miniapp.capture.image` 와 동형.
- **네이티브 경로**: 안드로이드가 오디오 파일을 공유(`ACTION_SEND audio/*`)하면 `captureAudio` → `miniapp.capture.audio`.
- **폴백 없음**: OCR 의 tesseract 같은 로컬 ASR 폴백이 없어 서버 다운 시 connection refused → 명확한 에러 surface (graceful degradation = 명확한 실패).
- **override**: 환경변수 `DENEB_ASR_URL`(기본 `http://127.0.0.1:18013`), `DENEB_ASR_HOTWORDS`(고유명사 교정 bias, 선택).
- 참고: 안드로이드 **음성 캡처(#1843)** 는 온디바이스 시스템 STT(짧은 명령, 권한 불필요)라 이 서버와 무관. 긴 녹음·화자분리·핫워드 교정이 필요한 **오디오 공유 캡처(#1847)** 가 이 사이드카를 쓴다.

### 운영 명령
```bash
~/start-vibevoice-asr.sh
docker ps --filter name=vibevoice-asr
curl -s http://127.0.0.1:18013/health    # {"status":"ready",...}
curl -s -F file=@meeting.oga -F "hotwords=탑솔라, 데네브, 김민준 부장" \
  http://127.0.0.1:18013/v1/transcribe
docker logs --tail 50 vibevoice-asr
```

### 라이브 검증 (Go 경로)
```bash
# CI 에선 skip (GPU 없음). 호스트에서 실제 서버 대상 e2e:
DENEB_ASR_LIVE=1 DENEB_ASR_AUDIO=/path/to.wav DENEB_ASR_URL=http://127.0.0.1:18013 \
  go test -run TestTranscribeAudio_Live ./internal/pipeline/chat/tools/
```

---

## Hindsight (장기 기억 서비스) — 은퇴 (2026-06-15)

Hindsight(Hermes 계열 FastAPI+pgvector 장기기억 서비스)는 **2026-06-15 게이트웨이 회상에서 은퇴**했다. puppet 회상 측정 결과 순기여 ~0 — 합성 점수(0.60–0.92)가 wiki/diary 의 BM25 밴드(wiki ≥1.6, diary 3–9)보다 낮아 wiki·diary 가 히트하면 항상 랭킹 탈락했고, surface 될 때도 wiki 페이지 요약과 같은 사실을 중복 주입했다. recall 소스·retain recorder·`domain/hindsight` 클라이언트·knowledge hindsight 어댑터·`DENEB_HINDSIGHT_*` env·시스템 프롬프트 서비스 블록 모두 제거. 장기기억은 이제 **wiki(큐레이션·시맨틱)+diary(원문)+polaris(세션)** 가 담당한다.

- **호스트 정리(운영자 작업)**: `cd ~/hindsight && docker compose down` 으로 컨테이너(8888/pgvector) 내림. systemd 의 `DENEB_HINDSIGHT_URL` Environment 도 제거(있어도 코드가 더는 안 읽음). 데이터는 `~/hindsight/hindsight-backup-20260610.sql.gz` 백업에 보존 — 되살리려면 백업 복원 + 코드 revert.
- **작업 기억은 wiki/diary/polaris로 흡수**: Hindsight 이름의 서비스·스킬·프롬프트 섹션은 더 이상 쓰지 않는다. 작업 연속성은 wiki/diary/polaris/graphify가 담당하고, 모순·대체 관계는 wiki의 `supersedes`/`superseded_by` 흐름으로 남긴다.

---

## wormhole (모델 라우터 — Deneb 모델 접근의 단일 관문)

### 무엇 / 왜
- 사이드카 *모델*이 아니라 모델 *라우터*. OpenAI/Anthropic 호환 단일 엔드포인트(`:18800`) 뒤로 로컬 vLLM + 클라우드(claude 등)를 **모델명으로** 멀티플렉싱하는 우리 자체 Go 바이너리(`gateway-go/cmd/wormhole`). 원래 목적=외부 클라(Claude Code·스크립트) 단일 URL. **이제 Deneb 자신의 모델 호출도 wormhole 경유**로 통합(2026-06-14, 사용자 결정 "메인 포함 전부").
- 이득: 단일 엔드포인트 + 업스트림 키 단일 금고 + SparkFleet 자동발견(`:18900`) + 로컬→클라우드 auto 폴백 + 프라이버시 가드. 상세 설계는 [[project_wormhole]].

### ★★ APC 불가침 (메인 경로의 절대 규칙)
> 메인 챗(dsv4)은 vLLM APC(byte-prefix 캐시)에 극도로 민감하다(`.claude/rules/prompt-cache.md` §1.5). wormhole을 메인 앞에 두려면 **바이트 투명**해야 한다.

- **Deneb 가 쓰는 wormhole 엔트리는 `toggleKwarg` 를 절대 달지 마라.** `toggleKwarg` 가 있으면 wormhole 이 effort 라우팅으로 `chat_template_kwargs` 를 **주입**해 렌더 프롬프트를 바꾼다 → APC 파괴 + Deneb 자체 effort 라우팅(`run_capability.go`)과 **이중화 충돌**(injectKwarg 가 기존 값 덮어씀). 엔트리에 toggleKwarg 가 없으면 `applyThinking` 이 즉시 return → **순수 패스스루**.
- **이름 일치**: 엔트리 `name == upstreamModel == vLLM 서빙 모델명`, deneb.json 이 그 name 을 보냄 → `rewriteModel` 미발동 → 바이트 동일. (model 필드는 렌더 프롬프트에 안 들어가 rewrite 자체는 APC-safe 지만, 무변경이 가장 안전.)
- 결론: **effort 라우팅은 Deneb 가 단독 수행**(튜닝됨·파이프라인 통합), wormhole 은 메인에 대해 dumb passthrough. (외부 클라용 effort 라우팅을 살리려면 별도 toggleKwarg 엔트리 또는 향후 per-request opt-out 헤더.)

### ★ SPOF (핫패스가 된 wormhole)
- 메인을 wormhole 로 태우면 **wormhole 다운 = 메인 다운**. **현재 운영(2026-06-14): main/lightweight/tiny + fallback/analysis(클라우드 glm-5.2) 전부 wormhole 경유** (사용자 "클라우드 호출 모아"). 즉 wormhole 이 모델 레이어의 단일 관문.
- 핵심 구분: **흔한 실패(업스트림 모델 다운)는 여전히 커버됨** — main(dsv4@srv2) 죽으면 게이트웨이 서킷브레이커→fallback role→wormhole(살아있음)→다른 업스트림(zai). 안 커버되는 건 **wormhole 프로세스 자체 사망**뿐인데, 얇은 프록시 + `Restart=on-failure`(≈5s respawn) 로 자가치유. 더 강한 격리를 원하면 fallback 하나를 직결로 빼면 됨(그 경우 SPOF 0, 단 키 중복).
- wormhole 은 `Restart=on-failure` systemd 서비스로 상주(아래).

### 서버 (상주)
- 빌드: **`make wormhole`** → `dist/wormhole`. 서비스: **`scripts/deploy/wormhole.service`**(systemd, `Restart=on-failure`, `MemoryMax=512M`, journal) 또는 수동 **`scripts/deploy/start-wormhole.sh {start|stop|restart|status}`**.
- 설정: **`~/.wormhole/config.json`**(레포 밖, 시크릿 포함). 템플릿 = `gateway-go/cmd/wormhole/config.example.json`. `token` + 각 model `key` 는 `${ENV}` 확장. 포트 기본 `:18800`.
- Deneb-백엔드용 config 골격(메인 dsv4 = no-toggleKwarg 패스스루):
  ```json
  {
    "listen": ":18800",
    "token": "${WORMHOLE_TOKEN}",
    "sparkfleet": { "url": "http://127.0.0.1:18900" },
    "models": [
      { "name": "dsv4", "url": "http://127.0.0.1:8000/v1", "upstreamModel": "<vLLM 서빙명>" }
    ]
  }
  ```

### Deneb 배선 (deneb.json — 호스트 prod config)
- `models.providers` 에 wormhole 추가 + role 을 거기로:
  ```json5
  "models": {
    "providers": { "wormhole": { "baseUrl": "http://127.0.0.1:18800/v1", "apiKey": "${WORMHOLE_TOKEN}" } },
    "modelRole": { "main": "wormhole:dsv4" /* fallback 은 직결 유지 */ }
  }
  ```
- 게이트웨이는 OpenAI 호환 provider 로 wormhole 을 그냥 호출(`run_provider.go`, 코드 변경 0). provider `headers` 도 지원하니 향후 opt-out 헤더가 필요하면 거기로.

### 클라우드 호출 통합 (구독 LLM 을 wormhole 경유, ★openai 프로토콜 권장, 2026-06-15)
> 구독 클라우드(zai/glm·mimo)를 wormhole 로 모을 때 **각 프로바이더의 OpenAI 호환 엔드포인트로 openai 라우팅**하라. anthropic 으로 모으면 아래 loopback-anthropic 마찰에 빠진다.

- wormhole config cloud 엔트리(openai): `{name, url(openai base), upstreamModel, "key":"${ENV}" 또는 리터럴}` — protocol 생략(openai 기본). **no toggleKwarg**(APC/effort 규칙). wormhole 이 url 뒤에 `/chat/completions` 만 붙이니 url 은 그 base.
  - **zai/glm**: ★**코딩플랜 전용** `https://api.z.ai/api/coding/paas/v4` (일반 `…/api/paas/v4` 는 잔액부족 429). 키 `${ZAI_API_KEY}`.
  - **mimo**: `https://token-plan-sgp.xiaomimimo.com/v1`. 리터럴 키.
  - **kimi**: openai 엔드포인트가 **Coding-Agent 전용 403** → openai 불가, **직결 유지**(직결 anthropic remote provider 는 피커에서 정상 그린).
- **env 키 배선**: wormhole.service `EnvironmentFile=-/home/choiceoh/.deneb/.env` 로 `${ZAI_API_KEY}` 등 주입(게이트웨이와 동일 소스). 리터럴 키는 config 직접.
- **deneb.json 배선**: openai 라 **별도 provider 불필요** — 기존 `wormhole`(openai) provider 의 `models` 에 glm-5.2/mimo 추가, role 전환 `fallbackModel → wormhole/glm-5.2`.
- 검증: `curl :18800/v1/models` 에 glm/mimo 가 뜨고 `:18800/v1/chat/completions -d '{"model":"glm-5.2",…}'` 200.

> ★★**왜 anthropic 말고 openai 인가 (2026-06-15 교훈)**: anthropic 으로 모으려면 별도 `wormhole-anthropic` provider(baseUrl `:18800` 무 /v1, 클라가 `/v1/messages` 부착, wormhole 은 url 뒤 `/messages`)가 필요한데 이건 **loopback-anthropic 라 `/v1/models` 가 없다(404)**. 그런데 모델 피커 프로브 **그리고** modelrole 레지스트리 모델 해석 둘 다 `/v1/models` 에 의존 → glm 이 피커에 안 뜨고 fallback 이 역할 목록·resolve 실패(라우팅은 됨, 브라우징/해석 불가). **백엔드가 openai 호환 엔드포인트를 가지면 그걸로 openai 라우팅하는 게 cross-protocol provider 보다 깔끔** — /v1/models 가 있어 피커·레지스트리가 그대로 동작. (anthropic-only 백엔드는 직결 remote provider 로 두면 피커는 non-200=reachable 로 그린.)

### 운영 명령
```bash
make wormhole                                    # 빌드
systemctl --user enable --now wormhole.service   # 상주 (또는 scripts/deploy/start-wormhole.sh start)
curl -s http://127.0.0.1:18800/health            # ok
curl -s http://127.0.0.1:18800/v1/models -H "Authorization: Bearer $WORMHOLE_TOKEN"  # 라우팅 테이블(config+발견)
```

### 라이브 검증 (메인 경로 cutover 시 필수)
- **바이트 패스스루 증명**: 같은 요청을 `:18800`(wormhole) 과 `:8000`(직결 vLLM) 에 보내 vLLM `prefix_cache` 메트릭이 동일하게 적중하는지 확인 → wormhole 이 APC 를 깨지 않음을 보장.
- **멀티턴 APC**: cutover 후 `scripts/dev/live-test.sh logs-grep prefix_cache` 또는 엔진 `/metrics` 의 `vllm:prefix_cache_hits_total` 가 정상 누적되는지(직결 때 대비 적중률 유지) 확인. 떨어지면 즉시 롤백(deneb.json role 직결 복구 + 게이트웨이 재시작).

## 새 사이드카 모델 추가 시 체크리스트
- [ ] vLLM(또는 호환) 서버를 OpenAI `/v1` 로 띄우고, 호스트 런처 스크립트(`~/start-*.sh`)를 `--restart unless-stopped` 로 작성
- [ ] 코드는 엔드포인트를 **환경변수 override + 합리적 로컬 기본값**으로 받기 (PaddleOCR-VL 의 `DENEB_OCR_VL_URL` 패턴)
- [ ] 로컬 서버 다운 시 **graceful degradation 경로** 확보 (폴백 또는 명확한 에러)
- [ ] HTTP 호출은 `pkg/httputil.NewClient(timeout)` 사용
- [ ] 이 표에 행 추가 + 운영 명령 기재
