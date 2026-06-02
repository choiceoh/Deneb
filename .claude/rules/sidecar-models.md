---
description: 로컬 GPU 사이드카 모델 운영 현황 (OCR/ASR/추출/임베딩) — 엔드포인트·기동·배선·폴백
globs: ["gateway-go/internal/pipeline/chat/tools/paddleocr.go", "gateway-go/internal/pipeline/chat/tools/asr.go", "gateway-go/internal/pipeline/chat/tools/gmail_attachment.go", "gateway-go/internal/pipeline/chat/web/web_html.go", "gateway-go/internal/ai/modelrole/**", "gateway-go/internal/pipeline/pilot/**"]
---

# Sidecar Models (GPU 부가 모델 운영 현황)

> Deneb 는 메인 챗 LLM 외에도 **로컬 GPU(DGX Spark, gx10)에서 상주 서빙되는 전용 모델들**을 호출한다. 대부분 vLLM 의 OpenAI 호환 `/v1` 엔드포인트지만, 일부(VibeVoice-ASR)는 전용 FastAPI 래퍼로 상주한다. 외부 API 호출을 피하고 단일 머신에서 자급한다는 프로젝트 원칙(로컬 추론 우선)을 따른다. 이 파일은 "어떤 모델이, 어디서, 어떻게" 돌아가는지의 단일 진실원이다.

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

## 새 사이드카 모델 추가 시 체크리스트
- [ ] vLLM(또는 호환) 서버를 OpenAI `/v1` 로 띄우고, 호스트 런처 스크립트(`~/start-*.sh`)를 `--restart unless-stopped` 로 작성
- [ ] 코드는 엔드포인트를 **환경변수 override + 합리적 로컬 기본값**으로 받기 (PaddleOCR-VL 의 `DENEB_OCR_VL_URL` 패턴)
- [ ] 로컬 서버 다운 시 **graceful degradation 경로** 확보 (폴백 또는 명확한 에러)
- [ ] HTTP 호출은 `pkg/httputil.NewClient(timeout)` 사용
- [ ] 이 표에 행 추가 + 운영 명령 기재
