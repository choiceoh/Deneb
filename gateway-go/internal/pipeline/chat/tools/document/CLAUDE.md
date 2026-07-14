# Document extraction 변경 지도

이 패키지는 mail, file store, web fetch가 공유하는 문서 형식 판별, local parser,
OCR fallback과 bounded attachment 표현을 소유한다. 호출자별 문구·tool 등록은
상위 패키지에 남고 여기서는 raw bytes에서 readable text로의 계약만 제공한다.

## 진입점과 소유권

- `api.go`의 `ExtractText`, `ExtractFileText`, `ExtractAttachmentText`,
  `OCRImage`, `OCRPDF`가 목적별 public facade다.
- `document_extract.go`의 `ExtractDocumentText`, `IsExtractableDocument`와 내부
  canonical dispatcher가 MIME/extension을 PDF, XLSX, DOCX, PPTX, CSV, image,
  text parser로 한 번만 분기한다.
- `docparse.go`가 PDF/OOXML parser와 markdown table 보존을,
  `paddleocr.go`가 PaddleOCR-VL → tesseract fallback을 소유한다.
- `attachments.go`의 `Attachment`, `ExtractAttachments`가 여러 첨부의 병렬 추출,
  원래 순서와 출력 예산을 관리한다.

## 의존 방향과 불변조건

- 의존 방향은 `mail/web/files/tools → tools/document → core/pkg + local
  HTTP/CLI`다. document는 상위 `tools`, chat pipeline, mail package를 import하지
  않는다.
- 모든 호출자는 같은 canonical dispatcher를 거쳐야 한다. facade 차이는 의도된
  promotion 정책뿐이다: attachment는 image/plain text를 허용하지만
  `ExtractDocumentText`는 진짜 document와 Markdown만 성공으로 반환한다.
- born-digital PDF는 lossless text를 먼저 보존하고, 비었거나 table page일 때만
  OCR을 사용한다. OCR 결과가 더 구조적이라는 증거가 없으면 원본 text를 교체하지
  않는다.
- untrusted OOXML/CSV는 자연 순서를 보존하면서 row·column 상한을 반드시 적용한다.
  XLSX의 Excel 최대 열을 넘는 ref로 padding allocation을 유발하면 안 된다.
- attachment 추출은 동시성 4, 문서당 20K rune, 전체 48K rune을 넘지 않고 입력
  순서로 렌더링한다. context 취소 후 새 goroutine을 시작하지 않는다.

## 테스트와 집중 검증

- `docparse_test.go`의 `TestExtractDocumentReturnsConsistentTextAcrossCallers`와
  `TestExtractDocumentTextRejectsPlainTextAndUnsupportedBinary`가 canonical dispatcher와 facade 차이를
  고정한다.
- `contracts_test.go`의 `TestXLSXRejectsOversizedColumnReference`,
  `TestAttachmentRenderingRuneBudgets`, `TestExtractDocumentTextPromotionContract`가
  hostile input과 출력 예산을 검증한다.
- `ooxml_characterization_test.go`의
  `TestExtractOOXMLText_PreservesNestedTableAndBoundaryOrdering`과
  `paddleocr_test.go`의 `TestPaddleOCR_FallbackOnError`가 구조 보존과 degrade 경로를
  확인한다.

`cd gateway-go && go test -count=1 ./internal/pipeline/chat/tools/document`
