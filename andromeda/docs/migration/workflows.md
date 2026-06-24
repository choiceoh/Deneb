# 워크플로 — Deneb로 이관

두 가지: **CI**(andromeda 변경 검증 + wire-drift)와 **릴리즈 빌드 잡**(andromeda 컴포넌트
릴리즈 시 번들 빌드·서명·업로드). 핵심 모노레포 변경:

- `paths:` 필터로 데스크탑/게이트웨이 변경에만 발화 (Deneb의 다른 잡과 분리).
- wire-drift는 **게이트웨이가 같은 레포** → Deneb 클론·`DENEB_DIR` 제거. `gen-wire`가
  cwd(`andromeda`) 기준 `../gateway-go`를 자동 감지(`scripts/gen-wire.mjs` §6).
- 릴리즈 빌드 잡은 **Deneb 기존 release-please 워크플로와 같은 런 안**에 둔다 — release-please가
  `GITHUB_TOKEN`으로 만든 태그는 별도 워크플로를 트리거하지 못하므로(태그 트리거 빌드는 영영
  안 뜸), 같은 워크플로의 후속 잡으로 게이팅해야 한다.

---

## 1. CI — `.github/workflows/andromeda-ci.yml` (신규)

verify는 `andromeda/**`에만, wire-drift는 **`andromeda/**`+`gateway-go/**`**에 발화한다
(게이트웨이 계약이 바뀌면 커밋된 TS 타입과의 드리프트를 다시 검사해야 하므로).

```yaml
name: Andromeda CI

on:
  push:
    branches: [main]
    paths: ["andromeda/**", "gateway-go/**"]
  pull_request:
    paths: ["andromeda/**", "gateway-go/**"]

jobs:
  verify:
    # andromeda 자체 변경에만 의미 — gateway-go만 바뀐 PR에선 wire-drift만 돌면 충분하지만,
    # paths는 워크플로 단위라 여기서도 발화한다. 불필요한 실행을 더 줄이려면 verify를 별도
    # 워크플로(paths: andromeda/**)로 분리할 것.
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: andromeda
    steps:
      - uses: actions/checkout@v4

      - name: Setup pnpm
        uses: pnpm/action-setup@v4
        with:
          package_json_file: andromeda/package.json # action은 working-directory를 안 따름 → 명시

      - name: Setup Node
        uses: actions/setup-node@v4
        with:
          node-version: 22
          cache: pnpm
          cache-dependency-path: andromeda/pnpm-lock.yaml

      - run: pnpm install --frozen-lockfile
      - run: pnpm run typecheck
      - run: pnpm run lint
      - run: pnpm run format:check
      - run: pnpm run test
      - run: pnpm run build

  wire-drift:
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: andromeda
    steps:
      - uses: actions/checkout@v4 # 모노레포 전체 — gateway-go는 ../gateway-go에 존재

      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version: stable

      # gen-wire는 node 빌트인만 쓰므로 pnpm install 불필요. ../gateway-go 자동 감지 →
      # 클론도 DENEB_DIR도 없음 (분리 레포 때의 두 단계가 통째로 사라짐).
      - name: Verify wire types are up to date
        run: node scripts/gen-wire.mjs --check
```

> verify의 불필요 실행이 신경 쓰이면 jobs를 두 워크플로로 쪼갠다:
> `andromeda-ci.yml`(verify, `paths: andromeda/**`) + `andromeda-wire-drift.yml`
> (wire-drift, `paths: ["andromeda/**", "gateway-go/**"]`).

---

## 2. 릴리즈 빌드 — Deneb 릴리즈 워크플로에 잡 추가

Deneb는 이미 release-please 잡이 있는 릴리즈 워크플로를 갖고 있다. 거기에 **(a) andromeda
컴포넌트 출력 노출**과 **(b) andromeda 빌드 잡**만 추가한다.

### (a) release-please 잡에 출력 추가

```yaml
jobs:
  release-please:
    runs-on: ubuntu-latest
    outputs:
      # ↓ andromeda 컴포넌트용 per-package 출력 (키는 패키지 경로 prefix + '--').
      #   release-please-action v4 manifest 모드 규약. 첫 실행에서 잡 로그로 실제 키 확인.
      andromeda_release_created: ${{ steps.release.outputs['andromeda--release_created'] }}
      andromeda_release_id: ${{ steps.release.outputs['andromeda--id'] }}
      andromeda_tag_name: ${{ steps.release.outputs['andromeda--tag_name'] }}
    steps:
      - id: release
        uses: googleapis/release-please-action@v4 # ← Deneb 기존 버전에 맞춤
        with:
          token: ${{ secrets.GITHUB_TOKEN }}
```

### (b) andromeda 빌드 잡 (같은 워크플로에 추가)

```yaml
jobs:
  # ↓ (a)의 release-please 잡과 같은 워크플로의 jobs: 아래에 이 잡을 추가
  andromeda-build:
    needs: release-please
    if: ${{ needs.release-please.outputs.andromeda_release_created == 'true' }}
    strategy:
      fail-fast: false
      matrix:
        include:
          - platform: macos-latest # Apple Silicon
            args: "--target aarch64-apple-darwin"
            rust_target: aarch64-apple-darwin
          - platform: macos-latest # Intel
            args: "--target x86_64-apple-darwin"
            rust_target: x86_64-apple-darwin
          - platform: windows-latest
            args: ""
            rust_target: ""
    runs-on: ${{ matrix.platform }}
    defaults:
      run:
        working-directory: andromeda
    steps:
      - uses: actions/checkout@v4

      - name: Setup pnpm
        uses: pnpm/action-setup@v4
        with:
          package_json_file: andromeda/package.json

      - name: Setup Node
        uses: actions/setup-node@v4
        with:
          node-version: 22
          cache: pnpm
          cache-dependency-path: andromeda/pnpm-lock.yaml

      - name: Setup Rust
        uses: dtolnay/rust-toolchain@stable
        with:
          targets: ${{ matrix.rust_target }}

      - name: Cache Rust build
        uses: swatinem/rust-cache@v2
        with:
          workspaces: andromeda/src-tauri -> target

      - name: Install frontend dependencies
        run: pnpm install --frozen-lockfile

      - name: Build bundles and upload to the release
        uses: tauri-apps/tauri-action@v0
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          # 업데이터 서명 — andromeda와 동일 minisign 키 (Deneb 시크릿에 추가해 둘 것; §4a).
          TAURI_SIGNING_PRIVATE_KEY: ${{ secrets.TAURI_SIGNING_PRIVATE_KEY }}
          TAURI_SIGNING_PRIVATE_KEY_PASSWORD: ${{ secrets.TAURI_SIGNING_PRIVATE_KEY_PASSWORD }}
        with:
          projectPath: andromeda # ← tauri-action은 working-directory를 안 따름 → 명시 필수
          releaseId: ${{ needs.release-please.outputs.andromeda_release_id }}
          args: ${{ matrix.args }}
```

## 주의 / 검증

- **출력 키 확인**: `andromeda--release_created` 등은 release-please-action v4 manifest 규약.
  Deneb의 action 버전에 따라 다를 수 있으니 **첫 릴리즈 PR 머지 후 release-please 잡 로그에서
  실제 출력 키를 한 번 확인**하고 맞춘다.
- **`projectPath` / `cache-dependency-path` / `workspaces`**: `uses:` 액션은 잡의
  `defaults.run.working-directory`를 따르지 않으므로 **레포 루트 기준 경로를 명시**해야 한다
  (`andromeda/...`). `run:` 스텝만 working-directory가 적용된다.
- **서명 키 연속성**: `TAURI_SIGNING_PRIVATE_KEY[_PASSWORD]`는 andromeda와 **동일 키**여야
  기존 설치본이 업데이트를 수락한다(§4a). 키 교체 금지.
- **첫 검증**: Deneb에서 andromeda 릴리즈 1회 → 번들/`latest.json`이 서명·업로드되고,
  **기존 설치본이 실제로 업데이트를 받는지** 확인(이관 전체에서 가장 중요한 체크).
