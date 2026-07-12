# Mail Archive Overlay 변경 지도

이 패키지는 immutable IMAP archive 위에 native mail의 read/archive/trash 상태와
message locator를 원자적으로 영속하는 단일 책임을 가진다.

## 계약

- `store.go`의 `Store`, `MessageState`, `NewStore`가 유일한 production surface다.
- 의존 방향은 `mailarchive → overlay → pkg/atomicfile`이다. overlay에서 상위
  mailarchive나 IMAP/Gmail transport를 import하지 않는다.
- locator 기록은 read/archive/trash flag와 `UpdatedAtMS`를 바꾸지 않는다.
- read/archive/trash mutation은 기존 locator와 flag를 보존한다.
- snapshot은 독립 복사본이다. 같은 정규화 path를 가리키는 `Store` 인스턴스는
  프로세스 내 경로별 lock을 공유해 read-modify-write update를 잃지 않는다.
- 상태 파일은 `0600`, 부모 디렉터리는 `0700`, 쓰기는 `atomicfile`을 사용한다.
- 손상되거나 없는 파일은 빈 overlay로 복구하지만, 쓰기 실패는 호출자에게 반환한다.

## 집중 검증

`cd gateway-go && go test -race -count=1 ./internal/platform/mailarchive/overlay`
