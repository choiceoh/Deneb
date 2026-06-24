# Deneb 측 적용 아티팩트 (ready-to-apply)

이 폴더는 Andromeda → Deneb 모노레포 이관의 **Deneb 레포 쪽 변경**을 미리 복붙용으로
정리해 둔 것이다. 계획·근거는 [`../MONOREPO-MIGRATION.md`](../MONOREPO-MIGRATION.md),
이 폴더는 그 §3·§5를 **그대로 적용 가능한 정확한 블록/워크플로**로 구체화한 것.

> 이 파일들은 **Andromeda 레포에서는 아무 동작도 하지 않는다** — 순수 참고 문서다.
> 실제 적용은 subtree 머지 후 **Deneb 레포 안에서** 한다.

## 파일

- [`release-please.md`](release-please.md) — Deneb `.release-please-config.json`에 추가할
  `andromeda` 패키지 블록 + `.release-please-manifest.json` 엔트리.
- [`workflows.md`](workflows.md) — Deneb로 옮길 CI(verify + in-repo wire-drift)와 릴리즈
  빌드 잡(andromeda 컴포넌트 릴리즈 시에만 발화).

## 적용 순서 (MONOREPO-MIGRATION.md §8 체크리스트와 동일)

1. Deneb 레포에 시크릿 `TAURI_SIGNING_PRIVATE_KEY` / `_PASSWORD` 추가 — **andromeda와 동일 키**
   (키를 바꾸면 기존 설치본이 영구히 업데이트 불능; §4a).
2. subtree 머지: `andromeda → Deneb/andromeda` (§2).
3. `release-please.md` 적용 (config 블록 + manifest 엔트리).
4. `workflows.md` 적용 (CI 워크플로 추가 + 릴리즈 워크플로에 andromeda 빌드 잡 추가).
5. Andromeda 레포의 기존 `release-please-config.json` / `.release-please-manifest.json` /
   루트 `release.yml`은 폐기(Deneb 설정에 흡수됨). CI(`ci.yml`)는 Deneb로 이전됐으니 제거.

## 적용 전 반드시 확인

- **태그 연속성**: 기존 andromeda 태그는 이미 `andromeda-v*` 형식이다(예: `andromeda-v0.0.16`).
  그래서 Deneb에서도 `component: "andromeda"`로 **같은 prefix를 유지**해야 태그/체인지로그가
  끊기지 않고, 게이트웨이 `v*`와도 충돌하지 않는다.
- **manifest 버전**: `release-please.md`의 manifest 엔트리는 **이관 시점의 최신 릴리즈 버전**과
  일치시킬 것 (이 문서 작성 시점 = `0.0.16`).
- **release-please 출력 키**: `workflows.md`의 빌드 잡 게이팅은 per-component 출력
  `andromeda--release_created` / `andromeda--id` / `andromeda--tag_name`를 쓴다. 이는
  release-please-action v4 manifest 모드 규약이며, Deneb의 release-please-action 버전에
  맞춰 **첫 실행에서 release-please 잡 로그로 실제 키를 한 번 확인**할 것.
