---
description: 로컬 GPU 사이드카 모델 운영 현황 (OCR/ASR/추출/임베딩) — 엔드포인트·기동·배선·폴백
globs: ["gateway-go/internal/pipeline/chat/tools/document/paddleocr.go", "gateway-go/internal/pipeline/chat/tools/artifact/asr.go", "gateway-go/internal/pipeline/chat/tools/document/docparse.go", "gateway-go/internal/pipeline/chat/web/web_html.go", "gateway-go/internal/ai/modelrole/**", "gateway-go/internal/pipeline/pilot/**", "gateway-go/cmd/wormhole/**", "scripts/deploy/start-wormhole.sh", "scripts/deploy/wormhole.service"]
---

# Sidecar Models (GPU 부가 모델 운영 현황)

> Deneb 는 메인 챗 LLM 외에도 **로컬 GPU 플릿에서 상주 서빙되는 전용 모델들**을 호출한다. 호스트 배치(2026-07-17 갱신): **srv4** = 게이트웨이·wormhole·diffusiongemma 엔진(:8100)·메일서버, **srv1** = GPU 보조 — PaddleOCR-VL(:18011)·VibeVoice-ASR(:18013)·hy-mt2(:8102)·lfm2.5(:8101)·sparkfleet(:18900) (**qwen3.6(:8000)은 2026-08-06 이후 죽어 있다** — hybrid dsv4 워커와 RAM 경합), **srv2** = dsv4 엔진(:8000). wormhole 의 `qwen3.6-35b-a3b` 엔트리는 srv1 tailnet(`http://100.105.145.6:8000/v1`) + `fleet: true`(SparkFleet 등록 시 노드 이동 자동 추적)로 남아 있으나 **백엔드는 죽어 있고, 이 엔트리를 이름으로 지정한 호출은 전부 fallback 으로 샌다** — 2026-08 에 그 fallback 이 유료 API 여서 12일간 1,346 회 과금됐다(수리: 사슬 재배선 + `metered` 게이트, model-roles.md 도그마 #7). 게이트웨이는 크로스호스트 사이드카를 env 오버라이드(`DENEB_OCR_VL_URL`/`DENEB_ASR_URL` = srv1 tailnet)로 소비한다. 대부분 vLLM 의 OpenAI 호환 `/v1` 엔드포인트지만, 일부(VibeVoice-ASR)는 전용 서비스로 상주한다. 로컬 추론은 **원칙이 아니라 선호**다(운영자 명시, 2026-08-01): 비용·주권·레이턴시가 비슷하면 플릿 자급이 기본값이지만, 클라우드가 실측으로 유의미하게 이기면 채택한다 — main(glm/kimi)·vision(gemini)이 그 선례. 클라우드 옵션을 "로컬 원칙 위반"으로 기각하지 말 것; 기각 논거는 아키텍처(파이프라인 우회 여부)·비용·품질 실측으로. 이 파일은 "어떤 모델이, 어디서, 어떻게" 돌아가는지의 단일 진실원이다.

## 현황 표

> **비-LLM 사이드카**: 상주 브라우저(browse 도구) — **srv4** `scripts/browser/`
> (Playwright 상주 headful Chromium, 프로필 `~/.deneb/browser-profile`, API
> 127.0.0.1:18930). 운영자가 `start-browser-sidecar.sh view`(noVNC)로 로그인해
> 두면 에이전트가 그 세션으로 로그인 벽 페이지를 읽는다(읽기 전용 v1).
> ★**정착(settle) 규칙**(2026-09-03 최종): `wait_ms` 를 통째로 자던 것을 **"렌더 + 콘텐츠 정지 + 네트워크 정지"가 모두
> 성립하면 조기 종료**로 바꿨다(`settlePage`). 세 조건 = 본문 400자 이상 · 그 길이가 350ms 무변화 · 인플라이트 요청 0에
> 400ms 무활동. 여기에 **최소 대기 1.2초 바닥**. `wait_ms` 는 이제 **상한(인내 예산)**이고, 네트워크가 처음 조용해진
> 시점부터 센다(옛 networkidle→sleep 과 동일한 최악값 보존). 골든셋 실측 8.4초→4.3초.
> ⚠️ **가드 셋 다 실측으로 벌었다** — 하나라도 빼면 조용히 본문을 잃는다:
> ① 400자 바닥 없으면 스켈레톤(`...` 3자)이 즉시 안정으로 보여 늦은 본문 3/3 유실.
> ② 네트워크 추적 없으면(=`networkidle` 일회성 대기만) 그 뒤 시작한 fetch 를 못 본다.
> ③ 1.2초 바닥 없으면 **내비 메뉴만으로 400자를 넘긴 페이지**가 기사 fetch 시작 전에 종료된다(실측: 435ms 에 메뉴만 반환).
> 회귀 프로브 `npm run probe:settle`(로컬 합성 3케이스, 네트워크 불필요) — 골든셋이 구조적으로 못 보는 실패 모드다.
> 기동 `start-browser-sidecar.sh start` · 게이트웨이 override `DENEB_BROWSE_URL`(Page Agent의 DENEB_BROWSER_URL과 별개) ·
> 라이브 검증 `DENEB_BROWSE_LIVE=1 go test -run TestToolBrowse_Live ./internal/pipeline/chat/tools`.

| 모델 | 역할 | 기본 엔드포인트 | 코드 진입점 | 비고 |
|---|---|---|---|---|
| **PaddleOCR-VL-1.6** (0.9B) | 문서 OCR (스캔 PDF·이미지 첨부) | `http://127.0.0.1:18011/v1` | `chat/tools/document/paddleocr.go` | 상주 서빙. tesseract 폴백 있음. ↓ 상세 |
| **Gemini 음성 전사** (프론티어 클라우드, 2026-08-01 컷오버) | 음성 전사 + 화자분리 + 타임스탬프 — **현행 primary** | `generativelanguage.googleapis.com` (gemini-3.5-flash, `GOOGLE_API_KEY`) | `chat/tools/artifact/asr_gemini.go` (opt-in `DENEB_ASR_PROVIDER=gemini`, 모델 override `DENEB_ASR_GEMINI_MODEL`) | 운영자 방향 "귀는 프론티어" + 실측(핫워드 **없이** 김민준/탑솔라 정확·회의 슬라이스 RTF~0.14). 핫워드는 프롬프트 컨텍스트로 주입, ≤14MB inline·초과 Files API, 실패 시 MOSS 폴백(reachable일 때만). 라이브 테스트 `DENEB_ASR_GEMINI_LIVE=1` |
| **MOSS-Transcribe-Diarize** (0.9B) | 음성 전사 **폴백** (구 primary) | `http://100.105.145.6:18014` (`POST /v1/transcribe`) | `chat/tools/artifact/asr.go` | 2026-07-18 VibeVoice(9B) 컷오버였으나 **2026-07-20 qwen36-fast(util 0.5)가 srv1 헤드룸을 소진한 뒤 earlyoom 상시 사살로 사실상 사망**(~12일 침묵 고장 — Gemini 컷오버의 직접 계기). 컨테이너는 stop 상태로 보존; srv1 메모리가 풀리면 `~/start-moss-asr.sh`로 복귀 가능. 롤백 = `DENEB_ASR_PROVIDER` 드롭인 제거 |
| 메인 챗 LLM | 대화/분석/도구호출 | provider config (Anthropic/OpenRouter/vLLM 등) | `pipeline/chat/run_provider.go` | modelrole `main`. 로컬일 때 기본 `http://127.0.0.1:8000/v1` |
| lightweight 서브 LLM | mailanalysis(메일폴)/genesis/pilot 등 잡일꾼 | modelrole `lightweight` | `pipeline/pilot/localai.go` | 메인보다 작은 모델, 백그라운드 작업용 |
| NuExtract3-FP8 | 구조화 추출 (스키마 기반) | (config-driven, 코드 하드코딩 없음) | — | `~/models/NuExtract3-FP8`. 현재 게이트웨이 코드에서 직접 참조 없음 |
| **Nemotron-3-Embed-1B-NVFP4** | 임베딩 (위키/다이어리/메일/도구/작업피드 시맨틱 검색·compaction MMR) | `http://127.0.0.1:8002` (어댑터 `scripts/deploy/nemotron-embed-server.py` → eugr vLLM 컨테이너 :8003) | `ai/embedding/client.go`, `domain/embedindex/calibration.go` (게이트웨이 drop-in `DENEB_EMBEDDING_URL`) | 2026-07-18 BGE-M3 컷오버 (2048d, query:/passage: 비대칭). 코사인 스케일이 BGE보다 훨씬 낮음 — **시맨틱 플로어는 전부 모델별 재캘리브레이션 값** (wiki 0.44·diary 0.20·summary 0.25·mail 0.33·fetch-tools 0.30·RSI exemplar 0.32·workfeed 0.47). 알려지지 않은 실모델 지문은 dense-only 유입을 막고, 운영자 재측정 뒤 표면별 `DENEB_*_SEM_FLOOR`로 명시적으로 연다. 설치 `scripts/systemd/setup-nemotron-embed.sh`. 롤백 = drop-in 제거(BGE :8001 유닛 잔존, 캐시 지문 분리) |
| **xprovence-reranker-bgem3-v2** (568M) | 위키·메일·도구 검색 크로스인코더 리랭크 | `http://127.0.0.1:8004` (`scripts/deploy/rerank-server.py`, user 유닛 `~/.config/systemd/user/deneb-rerank.service`) | `ai/rerank/client.go`, `domain/rankblend/blend.go`, 각 검색 표면 rerank 어댑터 (drop-in `DENEB_RERANK_URL`/`_MODEL`/`_FORCE`) | 2026-07-19 실배선 (#3992): 병합골드 P@1 83.7→87.1(+3.4pp)·p95 272ms. 검색 점수와 모델 raw score의 척도를 직접 섞지 않고 리랭커 순위만 정규화해 공통 블렌딩한다. GPU 요청은 비차단 단일-flight이며 바쁘거나 다운이면 기존 검색 순서를 유지하고 `/health` 통계로 busy·latency를 관찰한다. **nemotron-eval venv** 재사용(torch cu130 + transformers **4.51** — xprovence 원격코드가 transformers<5 필수 — + spacy `xx_sent_ud_sm`). fp16 ~1.2GB. Apache 폴백 `--model bge`(bge-reranker-v2-m3; xprovence는 CC BY-NC, 단일사용자 개인배치로 운영자 수용). 롤백 = 게이트웨이 drop-in `~/.deneb/rerank.conf` 삭제 + 재기동 |
| granite-embedding-311m / nomic-embed-text-v2-moe / BGE-M3 | 임베딩 (레거시/롤백) | BGE `:8001` | compaction 임베딩 폴백 경로 | `~/models/` 보관 |

> **modelrole 기본값**: `gateway-go/internal/ai/modelrole/registry.go` — `DefaultVllmBaseURL = "http://127.0.0.1:8000/v1"`, `DefaultVllmModel = "gemma4"`. 역할(main/lightweight/fallback)별 실제 모델은 `~/.deneb/deneb.json` 의 provider/modelRole 설정이 결정한다. 코드는 이름을 하드코딩하지 않는다.

---

## PaddleOCR-VL (Deneb 의 OCR 엔진)

### 무엇 / 왜

- 0.9B 비전-언어 모델 (NaViT 인코더 + ERNIE-4.5-0.3B). 한국어 업무 문서(표·수식·혼합 숫자·도장)에서 tesseract 대비 압도적 정확도. OmniDocBench v1.6 SOTA.
- FP8(동적) 가중치(~1.45GiB)라 unified memory 를 거의 안 먹어 메인 LLM 과 공존. 워밍 후 **~0.7s/page**(디코드 ~249 tok/s), 다페이지는 서버 배칭으로 ~7배(8p 8.6s→1.2s). 콜드 첫 요청만 CUDA-graph 워밍업.

### 서버 (상주)

- 런처: **`~/start-paddleocr-vl.sh`** (★**srv1** 호스트, **레포 밖** 로컬 파일 — 게이트웨이(srv4)는 `DENEB_OCR_VL_URL=http://100.105.145.6:18011` 로 크로스호스트 소비). 컨테이너 `paddleocr-vl`, port **18011**, `--restart unless-stopped`.
- 이미지: **`ghcr.io/spark-arena/dgx-vllm-eugr-nightly:latest`** (2026-07-07 업그레이드, 구 `vllm-node:latest`). `paddleocr_vl` 커스텀 arch 를 trust-remote-code 로 로드, **FP8**(동적, CutlassFP8ScaledMM·SM121) + **ngram** speculative decode 지원. 순수 OpenAI 호환. ⚠️ eugr 엔트리포인트가 vllm 이 아니라 런처가 `--entrypoint bash` + 영속 serve 스크립트(`~/serve-paddleocr-vl.sh`)로 기동 — ngram JSON 이 셸 인용을 안 타게.
- 가중치: `~/models/PaddleOCR-VL-1.6` (hf download). 1.82 GiB 소스, FP8 동적양자화로 로드 시 ~1.45 GiB.
- **메모리 예산 (2026-07-07 상향)**: `--gpu-memory-utilization 0.10` + `--max-num-seqs 8` + `--max-model-len 8192`. FP8 로 가중치가 작아져 util 0.10(~12GB)에서 **동시성 58x**(KV ~8GB) — 다페이지 배칭의 근거(구 0.03/seqs4/BF16 은 동시성 3.4x 로 배칭 사실상 불가였음). srv1 여유 ~28GB 내라 안전. ⚠️ **병목은 디코드**(출력토큰 생성)지 프리필 아님 — NaViT 가 vision 토큰을 ~1.3k 로 정규화해 **입력 해상도와 지연 무관**(이미지 축소 무의미, 측정으로 기각). 디코드가 느린 건 GB10 메모리대역폭(~273GB/s)에 0.9B 가 묶여서(토큰당 read) → FP8 이 반감(160→249 tok/s). 런처가 `GPU_MEM_UTIL`·`PADDLEOCR_MAX_NUM_SEQS`·`PADDLEOCR_IMAGE` env override 지원. **롤백**: `start-paddleocr-vl.sh.bak-pre-fp8`.

### 코드 통합

- `gateway-go/internal/pipeline/chat/tools/document/paddleocr.go`:
  - `paddleOCR(ctx, img, task)` — `/v1/chat/completions` 에 `image_url`(base64 data URI) + 태스크 프롬프트 전송. 태스크: `"OCR:"` / `"Table Recognition:"` / `"Formula Recognition:"` / `"Chart Recognition:"`.
  - `ocrImageBytes(ctx, img)` — **단일 OCR 진입점**. PaddleOCR-VL 우선, 실패 시 tesseract 폴백.
    ★**결과 캐시**(2026-07-18): 동일 바이트 재-OCR(메일폴 분석+챗 열람+재질문)은
    콘텐츠 해시 디스크 캐시(`ocrcache.go`, 기본 `~/.deneb/cache/ocr`,
    `DENEB_OCR_CACHE_DIR` override, 4096엔트리 프룬)에서 0ms 반환. 건강한
    Paddle 결과만 캐시 — tesseract 폴백·루프 잔존 출력은 미캐시(복구 후 재시도
    보장). 포맷 바뀌면 `ocrCacheVersion` 범프.
    ★speculative 튜닝은 소진(2026-07-18 A/B 실측): ngram tokens 3→8 = +1.2%
    (노이즈) → 3 유지. 웜 지연의 실변수는 **GB10 코테넌트 부하**(qwen36 등 동시
    가동 시 동페이지 6.7s→20s 실측) — spec 노브 재시도 금지, 병목은 대역폭 공유.
    ★**반복 루프 폴백**(2026-07-18): 품목 표 밀집 페이지에서 `"OCR:"` 단발 호출이
    같은 행을 max_tokens 소진까지 반복하는 퇴화가 실측됨(발주서 CER 2.58, 재현).
    `looksRepetitionLoop`(실질 라인 ≥12회 반복)가 감지하면 `"Table Recognition:"`
    으로 1회 재시도 후 셀 마크업(`<fcel>`/`<nl>`)을 파이프 행으로 변환해 반환
    (`paddleTableToText`; 같은 페이지 CER 0.153). 샘플링 페널티는 해법 아님
    (repetition_penalty=악화·frequency_penalty=표 내용 소실 실측). 재시도도
    루프면 원문 유지. 라이브 재현: `DENEB_OCR_LIVE_PAGE=<루프 PNG> go test -run Live ./internal/pipeline/chat/tools/document`.
- `tools/document/docparse.go` 의 `imageOCR`(이미지 첨부)와 `pdfOCR`(스캔 PDF)가 `ocrImageBytes` 경유 (구 `gmail_attachment.go` 에서 이사). **`pdfOCR` 페이지 루프는 병렬**(`ocrPageConcurrency`=6, 세마포어+WaitGroup, distinct-slot 쓰기로 순서 보존) — 서버 배칭(`--max-num-seqs 8`)을 활용해 N페이지 스캔을 ~1 배칭 디코드로 접음. 동시성 상한은 서버 seq 한도 밑으로 둬 라이브 챗/메일분석과 GPU 사이드카를 공유해도 몰리지 않게. <!-- docref:ignore -->
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

## MOSS-Transcribe-Diarize (음성 전사 엔진, 현행)

### 무엇 / 왜

- 0.9B end-to-end 오디오 모델 (Whisper-Medium 인코더 + Qwen3-0.6B 디코더, Apache-2.0). **전사+화자분리([S01]…)+타임스탬프 단일 패스**, 90분, 핫워드 프롬프트.
- 2026-07-18 VibeVoice-ASR(9B) 컷오버. 판정 근거(실측): 세바시 정답자막 CER **8.5% vs 9.6%**, RTF **0.26 vs 0.52**, 실회의 25분에서 VibeVoice는 기본 예산 잘림/32k 예산 미완주(RTF>1.8)·반복 환각, MOSS는 안정 완주. 상주 16GB→2GB로 srv1 earlyoom(`--prefer python3`) 킬 리스크 소멸. 상대 확인 사례: Plaud가 "회사"로 뭉갠 거래처명을 "핵사(Hexa)"로 정확히 들음.

### 서버 (상주)

- 런처: **`~/start-moss-asr.sh`** (★srv1, 레포 밖). 컨테이너 `moss-asr`, port **18014**, `--restart unless-stopped`. 이미지 `vibevoice-asr:latest` 재사용(torch/transformers 5.9/fastapi/ffmpeg).
- 코드: `~/moss-asr/moss_server.py` + `~/moss-asr/repo`(OpenMOSS github clone — inference 헬퍼). 가중치 `~/models/MOSS-Transcribe-Diarize`(1.8G).
- API·게이트웨이 배선은 VibeVoice 서버와 **동일 계약**(`POST /v1/transcribe` multipart file/hotwords → segments/transcription/rtf) — `asr.go` 무변경, `DENEB_ASR_URL`만 스왑(`~/.config/systemd/user/deneb-gateway.service.d/sidecar-remote.conf`).
- 함정: 단일 화자 오디오는 `[S01]` 태그 없이 `[t]텍스트[t]`로 나옴 → moss_server가 타임스탬프 페어 폴백 파서로 세그먼트화. 업스트림 헬퍼는 np 배열 truthiness·librosa 부재 이슈가 있어 서버가 soundfile/ffmpeg로 직접 디코드+`process_audio_info` 몽키패치.

## VibeVoice-ASR (구 전사 엔진 — 롤백 대기)

### 무엇 / 왜

- 9B 음성 ASR 모델 (VibeVoice 음향/의미 토크나이저 24kHz + Qwen2 디코더, MIT). **ASR + 화자분리 + 타임스탬프를 단일 패스**로 출력 (최대 60분/64K 토큰, 50+개 언어·한국어).
- Deneb 비서 모드의 회의/통화/음성메모 캡처용. Whisper + pyannote 2단 파이프라인을 단일 모델로 대체. 검증: 영어 2화자 화자분리·타임스탬프 정확, 한국어 일반어 CER≈0, RTF ~0.5–0.7.

### 서버 (상주)

- 런처: **`~/start-vibevoice-asr.sh`** (★**srv1** 호스트, **레포 밖** — 게이트웨이(srv4)는 `DENEB_ASR_URL=http://100.105.145.6:18013` 로 크로스호스트 소비). 컨테이너 `vibevoice-asr`, port **18013**, `--restart unless-stopped`.
- 서빙: PaddleOCR 와 달리 **vLLM serve 아님.** MS 의 vLLM 플러그인은 vLLM v0.14.1 타깃인데 로컬 `vllm-node` 는 0.21.1 → transformers eager + 얇은 FastAPI 래퍼로 상주 (단일 사용자엔 충분).
- 이미지: `vibevoice-asr:latest` = `FROM vllm-node:latest` + accelerate/soundfile/soxr/ffmpeg/fastapi. 빌드 컨텍스트 `~/vibevoice-asr/` (Dockerfile + `vibevoice_server.py`). <!-- docref:ignore -->
- 가중치: `~/models/VibeVoice-ASR-HF` (16GB BF16, hf download — 이 호스트에선 `HF_HUB_DISABLE_XET=1` 필요). 로드 ~80s(부팅 1회).
- **커밋 헤드룸**: strict overcommit(`vm.overcommit_memory=2`) 호스트라 16GB 상주를 위해 `vm.overcommit_ratio` 50→80 영구 상향(`/etc/sysctl.d/99-deneb-vibevoice.conf`). OOM 가드(min_free_kbytes/watermark)는 미변경.

### API

- `POST /v1/transcribe` (**OpenAI 비호환**): multipart `file` 또는 form `path`, 선택 `hotwords`·`chunk_size`·`max_new_tokens`. 응답 = `segments`(speaker/start/end/content) + `transcription` + `rtf`. `GET /health` 로 readiness.
- 음성 메시지 포맷(`.oga`/opus)·m4a·mp3·wav 자동 디코딩 (soundfile → ffmpeg 폴백).
- **핫워드 권장**: 한국어 일반어는 사실상 무오류지만 고유명사(거래처·제품·인명)는 bare ASR 이 틀린다 → Deneb 연락처/거래 KB 를 `hotwords` 로 주입하면 교정됨 (검증 완료: 탑솔라/데네브).

### 코드 통합 (#1847)

- `gateway-go/internal/pipeline/chat/tools/artifact/asr.go`:
  - `transcribeAudio(ctx, audio, filename, hotwords)` — `/v1/transcribe` 에 멀티파트 `file`(+ 선택 `hotwords`) 전송, `{segments, transcription}` 파싱. segment `speaker` 가 문자열 라벨 또는 숫자 인덱스 **둘 다** 와서 `flexStr` 로 수용(라이브 테스트가 잡은 함정).
  - `transcribeAudioText(ctx, audio, mimeType)` — **단일 전사 진입점**. 화자분리+타임스탬프로 포맷(`[mm:ss 화자N] …`), segment 없으면 flat transcription 폴백.
- `chat/tools/artifact/asr_export.go` 의 `TranscribeAudio` 래퍼가 패키지-프라이빗 진입점 노출 → `miniapp.capture.audio` 브리지 RPC(`gateway-go/internal/runtime/rpc/handler/chat/miniapp/miniapp_bridge.go`, `deps.Transcribe != nil` 일 때만 등록)가 공유 녹음을 전사해 한 agent turn 실행. PaddleOCR 의 `miniapp.capture.image` 와 동형.
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

Hindsight(Hermes 계열 FastAPI+pgvector 장기기억 서비스)는 **2026-06-15 게이트웨이 회상에서 은퇴**했다. puppet 회상 측정 결과 순기여 ~0 — 합성 점수(0.60–0.92)가 wiki/diary 의 BM25 밴드(wiki ≥1.6, diary 3–9)보다 낮아 wiki·diary 가 히트하면 항상 랭킹 탈락했고, surface 될 때도 wiki 페이지 요약과 같은 사실을 중복 주입했다. recall 소스·retain recorder·`domain/hindsight` 클라이언트·knowledge hindsight 어댑터·`DENEB_HINDSIGHT_*` env·시스템 프롬프트 서비스 블록 모두 제거. 장기기억은 이제 **wiki(큐레이션·시맨틱)+diary(원문)+polaris(세션)** 가 담당한다. <!-- docref:ignore -->

- **호스트 정리(운영자 작업)**: `cd ~/hindsight && docker compose down` 으로 컨테이너(8888/pgvector) 내림. systemd 의 `DENEB_HINDSIGHT_URL` Environment 도 제거(있어도 코드가 더는 안 읽음). 데이터는 `~/hindsight/hindsight-backup-20260610.sql.gz` 백업에 보존 — 되살리려면 백업 복원 + 코드 revert.
- **작업 기억은 wiki/diary/polaris로 흡수**: Hindsight 이름의 서비스·스킬·프롬프트 섹션은 더 이상 쓰지 않는다. 작업 연속성은 wiki/diary/polaris/graphify가 담당하고, 모순·대체 관계는 wiki의 `supersedes`/`superseded_by` 흐름으로 남긴다.

---

## wormhole (모델 라우터 — Deneb 모델 접근의 단일 관문)

### 무엇 / 왜

- 사이드카 *모델*이 아니라 모델 *라우터*. OpenAI/Anthropic 호환 단일 엔드포인트(`:18800`) 뒤로 로컬 vLLM + 클라우드(claude 등)를 **모델명으로** 멀티플렉싱하는 우리 자체 Go 바이너리(`gateway-go/cmd/wormhole`). 원래 목적=외부 클라(Claude Code·스크립트) 단일 URL. **이제 Deneb 자신의 모델 호출도 wormhole 경유**로 통합(2026-06-14, 사용자 결정 "메인 포함 전부").
- 이득: 단일 엔드포인트 + 업스트림 키 단일 금고 + SparkFleet 자동발견(`:18900`) + 로컬→클라우드 auto 폴백 + 프라이버시 가드. 상세 설계는 [[project_wormhole]].

### ★★ APC 불가침 (게이트웨이 dsv4 경로의 절대 규칙)
>
> 게이트웨이의 dsv4 트래픽은 vLLM APC(byte-prefix 캐시)에 극도로 민감하다(`docs/agent-rules/prompt-cache.md` §1.5). wormhole을 그 앞에 두려면 **바이트 투명**해야 한다. (2026-07-04 현재 main 은 클라우드 glm 이고 dsv4 는 fallback·챗봇 경로지만, 이 규칙은 역할이 아니라 **엔트리** 계약이다 — main 이 로컬로 복귀해도 그대로 성립. 실측 검증(2026-07-04): 동일 요청 직결 vs wormhole 경유가 같은 prefix family 에 합류, 적중 89% 동일.)

- **게이트웨이 dsv4 경로가 쓰는 wormhole 엔트리(`deepseek-v4-flash`)는 `toggleKwarg` 를 절대 달지 마라.** `toggleKwarg` 가 있으면 wormhole 이 effort 라우팅으로 `chat_template_kwargs` 를 **주입**해 렌더 프롬프트를 바꾼다 → APC 파괴 + Deneb 자체 effort 라우팅(`run_capability.go`)과 **이중화 충돌**(injectKwarg 가 기존 값 덮어씀). 엔트리에 toggleKwarg 가 없으면 `applyThinking` 이 즉시 return → **순수 패스스루**.
- **이름 일치**: 엔트리 `name == upstreamModel == vLLM 서빙 모델명`, deneb.json 이 그 name 을 보냄 → `rewriteModel` 미발동 → 바이트 동일. (model 필드는 렌더 프롬프트에 안 들어가 rewrite 자체는 APC-safe 지만, 무변경이 가장 안전.)
- 결론: **effort 라우팅은 Deneb 가 단독 수행**(튜닝됨·파이프라인 통합), wormhole 은 메인에 대해 dumb passthrough. (외부 클라용 effort 라우팅을 살리려면 별도 toggleKwarg 엔트리 또는 향후 per-request opt-out 헤더.)
- **★ 전용 변형 엔트리는 허용 (`thinkingMode`, 2026-07-04).** 같은 업스트림을 가리키는 **별도 이름** 엔트리로 추론 방향을 계약할 수 있다: `"off"` = 무조건 노추론 — 엔트리의 정체성이라 **X-Wormhole-No-Effort 로도 억제되지 않음**(이름으로 고른 소비자의 명시적 선택); `"off-unless-hard"` = **노추론 기본**, 명백한 어려움 신호(hard-signal·첨부·구조화)에서만 추론 유지 — "긴 입력"만으로는 안 켬(실측: dsv4 노추론이 메일 분석에서 동급 품질·5배 속도, 판정 근거는 agents-a1 메모리). APC 논거: 엔트리별 주입이 **일정**해 그 엔트리 소비자끼리 prefix family 일관, 메인 패스스루 바이트 불변. 운영 예: `{"name":"dsv4-nothink","url":"http://100.125.220.117:8000/v1","upstreamModel":"deepseek-v4-flash","toggleKwarg":"thinking","thinkingMode":"off"}` — 2026-08-25 현재 **tiny 역할**이 이 이름을 소비한다(lightweight 는 `deepseek-v4-flash`; translation 은 역할이 아니었고 `agents.translationModel` 키는 게이트웨이가 읽은 적이 없어 정리됐다 — 현재값은 `python3 scripts/dev/model_role.py <role>` 로 실측). ⚠ dsv4 의 진짜 스위치는 `thinking`(`enable_thinking` 은 무시됨 — 실측), 노추론 dsv4 는 산술 취약(메일 분석엔 무해 — 대소비교·보존 작업).

### ★ 이미지 게이트 (`vision`, 2026-07-05)
>
> 텍스트 전용 모델(GLM 텍스트 계열·DeepSeek)에 이미지 content-part 를 보내면 **hard 400** 이 나고, 이미지가 클라이언트 히스토리에 남아 **이후 모든 턴이 같은 400 으로 오염**된다(Kai 업스트림 실측 교훈, SimonSchubert/Kai@5d57ea69). 게이트웨이의 스트립은 deneb.json 이 `vision:false` 를 명시할 때만 작동(빌트인 기본값 없음)하므로, 모든 소비자가 지나는 wormhole 이 빌트인 폴백을 갖는다 (`cmd/wormhole/vision.go`).

- **동작**: 엔트리의 upstreamModel 이 텍스트 전용으로 판정되면 `messages[].content` 배열의 이미지 파트를 텍스트 스텁(`[이미지 첨부 생략 — 텍스트 전용 모델]`)으로 치환. openai(`image_url`)·anthropic(`image`) 양 프로토콜 지원.
- **판정**: GLM 텍스트 계열은 **정확 일치 목록**(vision 변형 `glm-4.6v` 등이 프리픽스를 공유해 프리픽스 매칭 금지), DeepSeek 은 **패밀리 프리픽스**(`vl` 포함 id 제외), unknown 모델은 이미지 통과(멀쩡한 멀티모달에서 깎는 게 더 나쁜 실패). 엔트리 `"vision": true/false` 로 강제 오버라이드 가능.
- **★ APC 논거**: 게이트는 **이미지 파트가 실제로 있는 요청만** 재작성한다 — 이미지 없는 요청은 fast-scan 단락 + 파싱 후에도 원본 바이트 그대로 전달(바이트 불변). 이미지 포함 요청은 어차피 400 이던 트래픽이라 기존 prefix family 를 가르지 않는다.

### ★ 클라우드 모델 추론 프로필 (`reasoning`, glm-5.2; 2026-06-21)
>
> `toggleKwarg`(vLLM `chat_template_kwargs`)는 위 규칙대로 Deneb 엔트리에 금지(APC). 하지만 **클라우드 모델은 추론 제어 방언이 달라** 게이트웨이가 표현하지 못한다 — 그 번역은 wormhole 만 할 수 있다. 그래서 cloud 전용 필드 `reasoning` 을 둔다(`cmd/wormhole/effort.go:reasoningRoute`, `applyReasoning`).

- **왜 필요한가**: GLM-5.2 는 `reasoning_effort` 를 `high|max` 만 인정하고 **명시적 `high` 가 아니면 전부 `max`(최심)로 해석**한다([z.ai 문서](https://docs.z.ai/guides/capabilities/thinking-mode)). Deneb 가 보내는 `reasoning_effort:"low"`(레벨 low)는 GLM 에선 도리어 **MAX** 가 된다(의도 정반대). 게이트웨이의 high-only 가드(`openai.go:reasoningEffortHighOnly`)는 `deepseek-v4` 만 매칭해 glm 을 놓친다.
- **`reasoning:"glm"` 동작**: dsv4 처럼 **Ares 가 턴마다** 판정 — 간단한 턴 → `thinking:{"type":"disabled"}`(끄기, `reasoning_effort` strip), 그 외 → `reasoning_effort:"high"` + `thinking:{"type":"enabled"}`(켜기, 절대 max 안 보냄). 즉 **끄기 / high 두 모드**.
- **no-effort 와의 관계**: `X-Wormhole-No-Effort` 는 **로컬 vLLM `toggleKwarg` 경로만** 억제한다(게이트웨이가 소유·APC). `reasoning` 클라우드 방언은 게이트웨이가 표현 못 하니 **헤더와 무관하게 적용**(이중화 아님). 따라서 toggleKwarg 금지 규칙과 충돌하지 않는다.
- **호스트 적용**: `~/.wormhole/config.json` 의 glm-5.2 엔트리에 `"reasoning": "glm"` 추가 → `make wormhole` → wormhole 재시작. config 예시는 `cmd/wormhole/config.example.json`.

### ★ SPOF (핫패스가 된 wormhole)

- 메인을 wormhole 로 태우면 **wormhole 다운 = 메인 다운**. **현재 운영: main·vision(kimi 직결) 제외 전 역할이 wormhole 경유** — 즉 wormhole 이 모델 레이어의 단일 관문. 역할별 현재 모델은 여기 적지 않는다(썩는다): `python3 scripts/dev/model_role.py <role>` 로 실측한다(역할→모델은 오퍼레이터가 픽커로 수시 변경 — `model-roles.md`).
- 핵심 구분: **흔한 실패(업스트림 모델 다운)는 여전히 커버됨** — 폴백 방향은 현재 **클라우드→로컬**: main(glm, 클라우드) 죽으면 게이트웨이 서킷브레이커→fallback role(dsv4@srv2, 로컬)→wormhole(살아있음) 경유로 낙하. 안 커버되는 건 **wormhole 프로세스 자체 사망**뿐인데, 얇은 프록시 + `Restart=on-failure`(≈5s respawn) 로 자가치유. 더 강한 격리를 원하면 fallback 하나를 직결로 빼면 됨(그 경우 SPOF 0, 단 키 중복).
- wormhole 은 `Restart=on-failure` systemd 서비스로 상주(아래).

### 서버 (상주)

- ★**상주 호스트 = srv4** (게이트웨이와 동일 호스트, 2026-07-06 통일 — 게이트웨이는 `127.0.0.1:18800` 로컬 소비). srv1 의 구 인스턴스는 트래픽 0 확인 후 disable 됨(유닛·config 보존).
- 빌드: **`make wormhole`** → `dist/wormhole`. 서비스: **`scripts/deploy/wormhole.service`**(systemd, `Restart=on-failure`, `MemoryMax=512M`, journal) 또는 수동 **`scripts/deploy/start-wormhole.sh {start|stop|restart|status}`**. <!-- docref:ignore -->
- 설정: **`~/.wormhole/config.json`**(레포 밖, 시크릿 포함). 템플릿 = `gateway-go/cmd/wormhole/config.example.json`. `token` + 각 model `key` 는 `${ENV}` 확장. 포트 기본 `:18800`. 비루프백 listen에서 token이 비면 부팅과 핫리로드 모두 거부한다(누락된 env 토큰도 fail-closed); 무인증 개발 모드는 명시적 loopback listen에서만 허용한다.
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
>
> 구독 클라우드(zai/glm·mimo)를 wormhole 로 모을 때 **각 프로바이더의 OpenAI 호환 엔드포인트로 openai 라우팅**하라. anthropic 으로 모으면 아래 loopback-anthropic 마찰에 빠진다.

- wormhole config cloud 엔트리(openai): `{name, url(openai base), upstreamModel, "key":"${ENV}" 또는 리터럴}` — protocol 생략(openai 기본). **no toggleKwarg**(APC/effort 규칙). wormhole 이 url 뒤에 `/chat/completions` 만 붙이니 url 은 그 base.
  - **zai/glm**: ★**코딩플랜 전용** `https://api.z.ai/api/coding/paas/v4` (일반 `…/api/paas/v4` 는 잔액부족 429). 키 `${ZAI_API_KEY}`.
    - 배선된 모델: `glm-5.2` · `glm-5.3` · **`glm-5.3-flash`**(2026-09-02 추가). 셋 다 컨텍스트 1M · 출력 상한 131,072(`max_tokens` 초과 시 code 1210).
    - flash 는 **깊이별 3엔트리**로 배선한다(2026-09-02) — 호출자가 이름으로 고르고 Ares 판정을 타지 않는다:
      `glm-5.3-flash`(`thinkingMode:"on"` = reasoning_effort high 고정) · `glm-5.3-flash-nothink`(`thinkingMode:"off"` = thinking disabled 고정) ·
      `glm-5.3-flash-local`(srv2 4노드 vLLM, 다운 시 클라우드 flash 로 페일오버). 실측(같은 프롬프트, 엔트리만 교체): 분석형 턴 추론 토큰 **234 → 18**.
      ⚠️ 인사말 같은 개방형 턴은 끄기를 걸어도 100~200 토큰을 추론한다 — GLM 의 최소 추론이 그만큼이라 '끄기'는 상한이지 0 이 아니다.
    - ★**`glm-5.3-flash` 는 코딩플랜 포함이고 쿼터가 glm-5.3 의 3배**(z.ai 공지) — 볼륨 트래픽을 여기로 흘리는 게 플랜을 아끼는 길이다.
    - ★**이미지: flash 만 받는다.** 실측(2026-09-02, 1x1 PNG 파트): `glm-5.3-flash` 정답 응답 / `glm-5.3`·`glm-5.2`·`glm-5.1`·`glm-4.7` 는 전부 400
      `messages.content.type is invalid, allowed values: ['text']`. 이 400 은 이미지가 트랜스크립트에 남아 **이후 턴까지 오염**시키므로 웜홀
      `textOnlyImageModels`(vision.go)가 막는다 — `glm-5.3` 은 빠져 있어 추가했고(#PR), **`glm-5.3-flash` 는 넣으면 안 된다**(같은 세대인데
      base 는 텍스트 전용, flash 는 멀티모달 — glm-4.7 세대와 정반대).
    - 추론 제어(실측): `reasoning_effort` low/high/max 가 실제로 스케일하고(flash 12/31/65 토큰), `thinking:{"type":"disabled"}` 도 최소 추론(~15)으로
      먹는다 — 웜홀 `reasoning:"glm"` 방언 그대로 유효. ⚠️ 단 추론이 0 이 되지는 않아 `max_tokens` 를 20 수준으로 주면 추론만 하다 잘려
      **빈 content + finish_reason=length** 가 나온다(실측). 짧은 답이어도 예산은 200+ 로.
  - **mimo**: `https://token-plan-sgp.xiaomimimo.com/v1`. 리터럴 키.
  - **kimi**: openai 엔드포인트가 **Coding-Agent 전용 403** → openai 라우팅 불가. ★**anthropic 프론트로 wormhole 경유 가능** (2026-07-03 코드 경로 검증, config-only — 아래 런북). ★2026-07-17부터 **구독 키가 wormhole 단일 경로**(deneb.json 직결 제거, `KIMI_API_KEY` env). kimi 전용 프로필(공식 문서 kimi.com/code/docs 대조, 2026-07-17): (a) **UA 변조 금지** — 공식 문서가 "클라이언트 식별자(User-Agent) 변조는 위반, 멤버십 정지 사유"라고 명시; claude-code UA 스푸핑 금지, Go 기본 UA로 정상 동작(구 registry `codingAgentUserAgent`는 직결 시절 유산). wormhole 엔트리별 `headers`는 합법 헤더용으로만. (b) `providers.kimi.routing={"enabled":true}` — anthropic 와이어 `thinking:{"type":"disabled"}`를 kimi가 실측 수용(단순 턴 출력 42→2tok·wall 30%↓)해 effort 라우팅 개통. (c) **K2.7부터 cache_control 수용**(실측: 반복 호출 cache_read_input_tokens 반환, 400 소멸) — `modelcaps.RejectsCacheControl` 스트립 은퇴, 표준 4-마커 캐시 재개(30k+ 시스템 프롬프트가 턴마다 풀-프라이스였던 것 해소). (d) 컨텍스트 **262,144**(K2.7; 우리 구 선언 131k의 2배). (e) 플랜(Allegretto+) 실측: `kimi-for-coding-highspeed`(디코드 5-6배, 쿼터 3배)·`k3` 사용 가능 — 피커 옵션으로 등재.
- ★**kimi anthropic-프론트 런북 (코드 변경 0)**: wormhole 은 `/v1/messages` 를 무번역 패스스루로 서빙하므로, **deneb.json 의 기존 `kimi` provider id 를 유지한 채 `baseUrl` 만 `http://127.0.0.1:18800`(★무 /v1 — anthropic 클라가 `/v1/messages` 를 스스로 붙임), `apiKey` 를 `${WORMHOLE_TOKEN}` 으로** 교체하면 끝. provider id 가 `kimi` 라서 빌트인이 전부 그대로 동작한다: cache_control 스트립(`modelcaps.RejectsCacheControl` — kimi 는 마커에 400), anthropic apiMode 기본값, 픽커 빌트인 모델(kimi-for-coding). wormhole config 에는 `{name:"kimi-for-coding", protocol:"anthropic", url:"<kimi anthropic base, ★/v1 로 끝나야>", upstreamModel:..., key:"${KIMI_API_KEY}"}` 엔트리 추가(validate 가 /v1 누락을 경고). wormhole 인바운드 인증은 `x-api-key` 도 받으므로(`bearerToken`) anthropic 클라 헤더 그대로 통과. 픽커 프로브(`GET :18800/models`)는 404 → reachable-green. ★호스트에 kimi CLI 크리덴셜(`KIMI_CREDENTIALS_FILE`/`KIMI_API_KEY`)이 남아 있어도 안전 — `buildClient` 가 **정적 apiKey 가 설정된 경우 env 토큰 콜백을 달지 않으므로**(리뷰 후속 수정; 이전엔 콜백이 정적 키를 덮어써 kimi 토큰이 wormhole 로 가서 401) WORMHOLE_TOKEN 이 그대로 나간다. 계약 고정 테스트: `modelrole/registry_test.go TestNewRegistryWithOptions_KimiBehindWormhole`. 적용 후 확인: `curl :18800/status` 에 kimi 엔트리(protocol=anthropic) + 해당 역할로 실제 턴 1회.
- **정적 토폴로지 게이트**: `scripts/audit/model_route_topology.py` 는 `~/.deneb/deneb.json` 의 picker/role/provider 노드와 `~/.wormhole/config.json` 의 client-route/upstream 노드를 한 그래프로 검사한다. 역할 모델이 `models.hiddenModels` 에 있거나, Deneb 의 provider/model 키 중 model 부분과 Wormhole 의 client-facing `name` 이 다르거나, protocol/base path 가 어긋나면 실패한다. `upstreamModel` 은 전달 시 재작성 대상일 뿐 client route 이름을 대신하지 않는다. 단일 호스트 `scripts/deploy/deploy.sh` 는 pull 직후·build 전에 이 검사를 실행하므로 실패 시 재시작하지 않는다. 수동 확인은 `python3 scripts/audit/model_route_topology.py`; 긴급 복구에서만 `DENEB_SKIP_MODEL_ROUTE_TOPOLOGY_CHECK=1` 로 명시 우회한다.
- **env 키 배선**: wormhole.service `EnvironmentFile=-/home/choiceoh/.deneb/.env` 로 `${ZAI_API_KEY}` 등 주입(게이트웨이와 동일 소스). 리터럴 키는 config 직접.
- **deneb.json 배선**: openai 라 **별도 provider 불필요** — 기존 `wormhole`(openai) provider 의 `models` 에 glm-5.2/mimo 추가, role 전환 `fallbackModel → wormhole/glm-5.2`.
- 검증: `curl :18800/v1/models` 에 glm/mimo 가 뜨고 `:18800/v1/chat/completions -d '{"model":"glm-5.2",…}'` 200.

> ★★**왜 anthropic 말고 openai 인가 (2026-06-15 교훈, 2026-07-03 갱신)**: anthropic 으로 모으려면 별도 `wormhole-anthropic` provider(baseUrl `:18800` 무 /v1, 클라가 `/v1/messages` 부착, wormhole 은 url 뒤 `/messages`)가 필요한데 이건 **loopback-anthropic 라 `/v1/models` 가 없다(404)**. 당시엔 모델 피커 프로브·modelrole 레지스트리 해석이 `/v1/models` 에 의존해 glm 이 피커에 안 뜨고 resolve 가 실패했다. **백엔드가 openai 호환 엔드포인트를 가지면 그걸로 openai 라우팅하는 게 여전히 1순위** — /v1/models 가 있어 발견·윈도우 프로브까지 전부 공짜. 단 그 뒤 코드가 바뀌어(카탈로그 우선 `resolveModelConfig`, 선언 모델 렌더, 프로브 non-200=reachable-green) **anthropic-only 백엔드는 이제 위 kimi 런북 패턴(빌트인 provider id 유지 + baseUrl 만 wormhole)으로 anthropic 프론트에 태울 수 있다** — cross-protocol 마찰은 openai 백엔드를 굳이 anthropic 으로 모을 때의 문제로 좁혀졌다.

### ★ 사용량 계량 (`/v1/usage`, 2026-07-05)
>
> 단일 관문이라 wormhole 이 "이 달 각 모델이 태운 토큰/요청"의 자연스러운 단일 진실원이다 (ClawRouter 패턴 차용). 이전엔 agent-logs 채굴로만 보이던 glm 소비(~3.2M tok/일)를 GET 한 번으로 답한다.

- **동작**: 업스트림 응답의 **바운드 테일 사본**(64KB)에서 usage 프레임(openai `prompt/completion_tokens`·anthropic `input/output_tokens` 양 방언, 마지막 프레임 승)을 베스트에포트 파싱. 월 윈도우(`YYYY-MM`)로 `~/.wormhole/usage.json` 에 영속(atomic replace, 30s 디바운스) — 재시작해도 월 누계 유지. <!-- docref:ignore -->
- **★ APC/바이트 불변**: 계량은 요청을 절대 안 건드리고(`stream_options.include_usage` 주입 금지 — usage 프레임 없는 스트림은 토큰 0으로 계량, 요청 수는 집계) 응답 스트림도 read-through 사본이라 클라이언트 바이트 무변형.
- **비용/예산(선택)**: 엔트리 `"pricing":{"inputPerMTokUsd":…,"outputPerMTokUsd":…}` 선언 시 추정 비용 계산, 톱레벨 `"monthlyBudgetUsd"` 선언 시 budget/usedPercent 표기. 어디까지나 표시용 — 예산 초과로 요청을 막지 않는다.

### 운영 명령

```bash
make wormhole                                    # 빌드
systemctl --user enable --now wormhole.service   # 상주 (또는 scripts/deploy/start-wormhole.sh start)
curl -s http://127.0.0.1:18800/health            # ok
curl -s http://127.0.0.1:18800/v1/models -H "Authorization: Bearer $WORMHOLE_TOKEN"  # 라우팅 테이블(config+발견)
curl -s http://127.0.0.1:18800/v1/usage -H "Authorization: Bearer $WORMHOLE_TOKEN"   # 이 달 모델별 토큰/요청/비용
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
