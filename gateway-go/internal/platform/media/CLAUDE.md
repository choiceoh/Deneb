# Media 수집 변경 지도

이 패키지는 외부 media byte 수집, YouTube transcript/metadata, 비디오 frame
추출을 소유한다. 내용 해석과 vision 판단은 상위 pipeline이 담당하며 여기서는
안전하고 bounded한 원자료만 반환한다.

## 진입점과 소유권

- `fetch.go`의 `Fetch`, `FetchOptions`, `MediaFetchError`가 HTTP media
  다운로드와 구조화된 실패를 제공한다.
- `youtube.go`와 `youtube_native.go`의 `ExtractYouTubeTranscript`,
  `ExtractYouTubeTranscriptNative`, `YouTubeResult`가 native caption 우선,
  yt-dlp/ASR fallback 경계를 소유한다.
- `watch.go`의 `WatchVideo`, `WatchOptions`, `WatchResult`가 local/YouTube
  영상을 frame과 transcript로 변환한다. frame 저수준 추출은 `frames.go`가
  담당한다.

## 의존 방향과 불변조건

- 의존 방향은 `pipeline/tools → platform/media → core/pkg + HTTP/CLI`다.
  media는 chat, pilot, RPC handler를 import하지 않으며 분석 prompt를 소유하지
  않는다.
- `Fetch`는 최초 URL과 모든 redirect에서 http/https만 허용하고 private,
  tailnet, cloud-metadata, IPv4-embedded IPv6 목적지를 반드시 차단한다. caller가
  custom `http.Client`를 줘도 redirect 검사를 우회하면 안 된다.
- Content-Length와 실제 body 모두 `MaxBytes`로 제한하고 원인은
  `MediaFetchError.Unwrap`으로 보존한다. URL credential을 오류에 노출하지 않는다.
- `WatchVideo`는 frame 수·다운로드 크기·subprocess 시간을 제한한다.
  scene 탐지가 실패하면 bounded even sampling으로 degrade하며 frame과 timestamp
  순서는 항상 대응해야 한다.

## 테스트와 집중 검증

- `fetch_contract_test.go`의 `TestFetchRejectsInvalidInitialURLsBeforeTransport`,
  `TestFetchValidatesEveryRedirectWithCustomClient`, `TestFetchSizeLimitsContract`
  계열이 SSRF와 byte 한도를 검증한다.
- `watch_scene_test.go`의 `TestWatchLocalFile_SceneAlignedFrames`와
  `TestDetectSceneChangeTimestamps_WindowOffset`, `youtube_native_test.go`의
  `TestSelectCaptionTrack`이 frame/caption 선택 계약을 고정한다.

`cd gateway-go && go test -count=1 ./internal/platform/media`
