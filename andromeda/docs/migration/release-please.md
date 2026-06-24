# release-please — Deneb에 andromeda 패키지 추가

Deneb는 manifest 모드 release-please를 쓴다(`.release-please-config.json` +
`.release-please-manifest.json`, 루트 패키지 `.`). 여기에 **andromeda를 두 번째 패키지로
흡수**한다. release-please는 한 워크플로가 모든 패키지의 릴리즈 PR을 관리하므로, 설정에만
패키지를 추가하면 된다 (빌드 잡은 [`workflows.md`](workflows.md)).

## 1. `.release-please-config.json` → `packages`에 추가

기존 `packages`에 **`andromeda` 키만** 추가한다(`.`/게이트웨이 패키지는 그대로). 키
`"andromeda"`는 레포 루트 기준 경로. 아래는 배치 위치를 보이려 `packages` 래퍼까지 보인 발췌
(strict JSON이라 실제 파일엔 주석 불가):

```json
{
  "packages": {
    "andromeda": {
      "release-type": "node",
      "component": "andromeda",
      "include-v-in-tag": true,
      "bump-minor-pre-major": true,
      "bump-patch-for-minor-pre-major": true,
      "changelog-path": "andromeda/CHANGELOG.md",
      "extra-files": [
        { "type": "json", "path": "andromeda/src-tauri/tauri.conf.json", "jsonpath": "$.version" },
        { "type": "toml", "path": "andromeda/src-tauri/Cargo.toml", "jsonpath": "$.package.version" }
      ]
    }
  }
}
```

- `component: "andromeda"` → 태그 `andromeda-vX.Y.Z`. **기존 andromeda 태그가 이미 이 형식**
  이라 연속성이 유지되고, 게이트웨이 `vX.Y.Z`와도 충돌하지 않는다(필수).
- `bump-minor-pre-major` + `bump-patch-for-minor-pre-major` → pre-1.0에서 feat/fix → patch
  (0.0.x), `!`/BREAKING → minor(0.x.0). andromeda 기존 bump 동작 그대로 이식.
- `extra-files`/`changelog-path`는 **`andromeda/` prefix**(Deneb 설정은 레포 루트 기준). 이
  셋이 Tauri가 버전을 읽는 세 위치 — `package.json`(release-type node가 자동) +
  `tauri.conf.json` + `Cargo.toml` — 를 lockstep으로 묶는다.

## 2. `.release-please-manifest.json`에 엔트리 추가

```json
{
  ".": "4.29.0",
  "andromeda": "0.0.16"
}
```

`.`는 Deneb 기존 값을 그대로 두고(위 `4.29.0`은 예시) `andromeda` 엔트리만 추가한다.
`"andromeda"` 값은 **마지막으로 릴리즈된 andromeda 버전**이어야 release-please가 다음 bump를
정확히 계산한다 — 이 문서 작성 시점 기준 `0.0.16`.

## 3. Andromeda 레포 쪽 정리 (흡수 후)

- `release-please-config.json`(점 없는 v4 단독 설정), `.release-please-manifest.json`,
  루트 `release.yml` → 폐기. 버전 관리는 Deneb의 manifest로 일원화.
- `package.json` / `src-tauri/tauri.conf.json` / `src-tauri/Cargo.toml`의 `version` 필드는
  그대로 두고 release-please가 갱신하게 둔다(`pnpm bump`는 수동 폴백으로만 잔존).

## 검증

- 첫 release PR이 `andromeda/CHANGELOG.md`를 만들고 세 버전 파일을 함께 bump하는지 확인.
- 머지 시 태그가 `andromeda-vX.Y.Z`로 찍히는지(게이트웨이 `v*`와 분리) 확인.
