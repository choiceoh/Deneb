---
description: 게이트웨이가 호출하는 GPU 사이드카 모델 운영 현황 (OCR/추출/임베딩) — 엔드포인트·기동·폴백
globs: ["gateway-go/internal/pipeline/chat/tools/paddleocr.go", "gateway-go/internal/pipeline/chat/tools/gmail_attachment.go", "gateway-go/internal/pipeline/chat/web/web_html.go", "gateway-go/internal/ai/modelrole/**", "gateway-go/internal/pipeline/pilot/**"]
---

# Sidecar Models (GPU 부가 모델 운영 현황)

> Deneb 는 메인 챗 LLM 외에도 **로컬 GPU(DGX Spark, gx10)에서 vLLM 으로 서빙되는 작은 전용 모델들**을 호출한다. 모두 OpenAI 호환 `/v1` 엔드포인트로 접근하며, 외부 API 호출을 피하고 단일 머신에서 자급한다는 프로젝트 원칙(로컬 추론 우선)을 따른다. 이 파일은 "어떤 모델이, 어디서, 어떻게" 돌아가는지의 단일 진실원이다.

## 현황 표

| 모델 | 역할 | 기본 엔드포인트 | 코드 진입점 | 비고 |
|---|---|---|---|---|
| **PaddleOCR-VL-1.6** (0.9B) | 문서 OCR (스캔 PDF·이미지 첨부) | `http://127.0.0.1:18011/v1` | `chat/tools/paddleocr.go` | 상주 서빙. tesseract 폴백 있음. ↓ 상세 |
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
- 가중치: `~/models/PaddleOCR-VL-1.6` (hf download). `--gpu-memory-utilization 0.15` 로 충분.

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

## 새 사이드카 모델 추가 시 체크리스트
- [ ] vLLM(또는 호환) 서버를 OpenAI `/v1` 로 띄우고, 호스트 런처 스크립트(`~/start-*.sh`)를 `--restart unless-stopped` 로 작성
- [ ] 코드는 엔드포인트를 **환경변수 override + 합리적 로컬 기본값**으로 받기 (PaddleOCR-VL 의 `DENEB_OCR_VL_URL` 패턴)
- [ ] 로컬 서버 다운 시 **graceful degradation 경로** 확보 (폴백 또는 명확한 에러)
- [ ] HTTP 호출은 `pkg/httputil.NewClient(timeout)` 사용
- [ ] 이 표에 행 추가 + 운영 명령 기재
