# Andromeda → Deneb 모노레포 이관 계획

> 상태: **계획(준비 단계)**. 비파괴적 사전 정비만 진행됨. 아래 §2~§5의 실제 이관
> 단계는 승인 후 순서대로 실행한다. 가장 큰 리스크는 **자동 업데이터(§4)** 하나뿐.

## 0. 왜 / 무엇이 이미 됐나

- **목표**: 데스크탑 클라이언트(Andromeda)를 Deneb 모노레포로 합쳐 게이트웨이·네이티브와
  코로케이션 → 계약/기능이 갈라지지 않게, 변경을 한 PR에서.
- **유리한 조건**: 둘 다 **public**, 둘 다 **release-please**. 비전·언어는 다르지만
  (Andromeda=TS, gateway=Go, native=Kotlin) 한 레포로 묶는 데 문제 없음.
- **이미 한 것**: 계약 codegen — `src/gen/miniappWire.ts`가 게이트웨이 `//deneb:wire`
  구조체에서 생성되고 CI `wire-drift`로 강제됨(드리프트 1차 차단). 모노레포가 되면 이
  체크가 클론 없이 in-repo로 더 단순해진다.
- **확인된 변경 표면(이관 시 실제 수정)**: 레포명 고정 참조는 기능상 **딱 둘** —
  업데이터 엔드포인트(`src-tauri/tauri.conf.json`, §4b) · CI `wire-drift`의 Deneb 클론
  (`.github/workflows/ci.yml`, §5~6). 그 외 `choiceoh/andromeda` 출현은 전부
  release-please가 자동 생성하는 `CHANGELOG.md` 링크라 손댈 필요 없음.

## 1. 타깃 레이아웃

```
Deneb/
  gateway-go/        (Go)
  client-android/    (Kotlin/Compose 네이티브)
  andromeda/         (TS/Tauri 데스크탑)  ← 여기로
```

## 2. Subtree 머지 (히스토리 보존)

```bash
# Deneb 레포에서
git remote add andromeda https://github.com/choiceoh/andromeda
git fetch andromeda main
git subtree add --prefix=andromeda andromeda/main
```

- 히스토리를 더 깨끗이 원하면 `git-filter-repo`로 andromeda 히스토리를 `andromeda/`
  경로로 재작성 후 merge.
- 이동 안전성 **(정적 감사 완료)**: 모든 빌드/설정 경로가 상대 기준이라 `andromeda/`
  하위로 내려가도 무변경 동작 — 절대경로·레포 루트 고정 경로·`../../` 사다리 **0건**
  (`/`로 시작하는 건 전부 빌드 경로가 아닌 웹 URL: `index.html`의 `/src/main.tsx`는
  Vite 루트 기준, 나머지는 API 경로).
  - `vite.config.ts`: `import.meta.url` 기준(`./package.json`·`@→./src`).
  - `tsconfig.json`: `baseUrl:"."`·`paths:{"@/*":["src/*"]}`·`include:["src"]` — 전부 상대.
  - `src-tauri/tauri.conf.json`: `frontendDist:"../dist"`·아이콘 상대; `identifier`는 이미 `ai.deneb.andromeda`.
  - `src-tauri/Cargo.toml`: path 의존성 없음. `eslint.config.js`: ignore/files 글롭 상대.
  - `scripts/gen-wire.mjs`: 모노레포 자동 감지(§6). → 남은 건 §5의 최종 빌드 확인 1회뿐.

## 3. release-please — Deneb 모노레포에 2번째 패키지 추가

> 복붙용 정확한 블록: [`migration/release-please.md`](migration/release-please.md).

Deneb는 이미 release-please **v5** + `.release-please-config.json` + `.release-please-manifest.json`
(`.` = `4.29.0`)을 쓴다. Andromeda는 v4 + `release-please-config.json`(점 없음, `.`=`0.0.16`).
→ **Andromeda를 Deneb 매니페스트의 두 번째 패키지로 흡수**한다.

- `.release-please-config.json`의 `packages`에 `andromeda` 추가:
  - `"release-type": "node"`
  - **`"component": "andromeda"`** → 태그가 `andromeda-vX.Y.Z`. **게이트웨이 `vX.Y.Z`와
    충돌 방지(필수)** — 같은 `v*` 네임스페이스를 쓰면 안 됨.
  - `"changelog-path": "andromeda/CHANGELOG.md"`
  - `"extra-files"`: `andromeda/src-tauri/tauri.conf.json`(`$.version`),
    `andromeda/src-tauri/Cargo.toml`(`$.package.version`) — 경로에 `andromeda/` prefix.
  - pre-1.0 bump 옵션(`bump-minor-pre-major` 등) 그대로 이식.
- `.release-please-manifest.json`에 `"andromeda": "0.0.16"` 추가(이관 시점 최신 릴리즈 버전).
- Andromeda의 기존 `release-please-config.json` / `.release-please-manifest.json` /
  루트 `release.yml`은 폐기(Deneb v5 설정으로 흡수).

## 4. ⚠️ 자동 업데이터 — 유일한 진짜 리스크

설치된 데스크탑들은 `tauri.conf.json`에 박힌 엔드포인트
`https://github.com/choiceoh/andromeda/releases/latest/download/latest.json` 를 폴링하고,
받은 번들/`latest.json`을 **minisign `pubkey`로 서명 검증**한다. 두 가지를 반드시 지킨다.

### 4a. 서명 키 연속성 (필수 — 어기면 전 클라이언트 업데이트 불능)

- **같은 minisign 키로 서명해야** 기존 설치본이 새 릴리즈를 수락한다.
- `andromeda/src-tauri/tauri.conf.json`의 `plugins.updater.pubkey`는 **그대로 유지**.
- 개인키 시크릿 `TAURI_SIGNING_PRIVATE_KEY` / `TAURI_SIGNING_PRIVATE_KEY_PASSWORD`를
  **Deneb 레포 시크릿에 추가**(andromeda와 동일 키). 키 백업: `C:/Users/user/.andromeda-release/`.
- 키를 바꾸면 기존 설치본은 영구히 업데이트를 못 받는다 → **절대 키 교체 금지**.

### 4b. 엔드포인트 전환 (전환기 필요)

릴리즈가 Deneb로 가면 새 자산은 `github.com/choiceoh/Deneb/releases/...`. 하지만 기존
설치본은 여전히 **andromeda 엔드포인트**를 본다. 끊기지 않게:

1. **복수 엔드포인트 (✅ 이 준비 PR에서 baking 완료)**: `src-tauri/tauri.conf.json`의
   `updater.endpoints`에 **Deneb 우선 + andromeda 폴백** 둘 다 나열 완료 —
   `["…/Deneb/releases/latest/download/latest.json", "…/andromeda/releases/latest/download/latest.json"]`.
   Tauri updater는 엔드포인트를 **순서대로 시도하고 먼저 성공하는 응답을 사용**하므로, Deneb
   미게시(404) 동안은 andromeda로 떨어져 **현재와 완전히 동일 동작**(설정 UI에 오류 안 뜸 —
   `src/updater.ts`는 `check()`가 throw할 때만 오류 처리, startup은 App.tsx가 catch).
   엔드포인트는 **설치된 바이너리에 박히므로**, 이동 전에 이 빌드를 andromeda로 한 번
   릴리즈하면 설치본들이 Deneb 주소를 미리 갖게 돼 → 이동 후 자동으로 Deneb를 1순위로 폴링
   → 안전한 컷오버. (이게 §4의 유일 리스크를 줄이는 핵심.)
2. **전환기 이중 게시**: Deneb 릴리즈 워크플로가 전환기 동안 **andromeda 레포 릴리즈에도
   `latest.json`(+번들 또는 Deneb 자산을 가리키는 latest.json)을 계속 게시** → andromeda
   엔드포인트만 아는 구버전 설치본도 계속 업데이트. 단 §4b.1을 **이동 전 릴리즈로 전파**해
   두면 대부분의 설치본이 이미 Deneb를 1순위로 알게 돼 이 이중 게시 의존도가 크게 줄어든다
   (이동 전에 한 번도 업데이트하지 않은 설치본에만 안전망으로 필요).
3. **andromeda 레포는 코드 이동 후에도 "릴리즈 전용"으로 당분간 유지**.
4. 충분히 흡수되면(대부분의 설치본이 Deneb 엔드포인트를 가진 빌드로 업데이트되면)
   andromeda 릴리즈/엔드포인트를 폐기.

## 5. CI / 빌드 워크플로 이관

> 복붙용 정확한 워크플로: [`migration/workflows.md`](migration/workflows.md).

- `ci.yml`(verify + wire-drift) → Deneb 워크플로로 이관. verify는 `andromeda/**` 경로에,
  wire-drift는 거기에 더해 `gateway-go/**` 경로에도 발화(게이트웨이 계약이 바뀌면 커밋된 TS
  타입과의 드리프트를 재검사해야 하므로). wire-drift는 게이트웨이가 같은 레포라 Deneb 클론도
  `DENEB_DIR`도 필요 없다 — gen-wire가 `../gateway-go`를 자동 감지(§6).
- `release.yml`의 **build job**(매트릭스: macOS arm64/x64, Windows; tauri-action 서명·업로드)
  → Deneb에서 **andromeda 컴포넌트 릴리즈가 생성될 때만** 발화(release-please의
  `paths_released`/태그 prefix로 게이팅), `working-directory: andromeda`, 같은 서명 키.
- Deneb 기존 워크플로(`release-please.yml`, `publish-apk.yml`)와 트리거가 겹치지 않게
  조건/경로 분리. (release-please는 한 워크플로가 모든 패키지의 릴리즈 PR을 관리하므로,
  andromeda 빌드 잡만 추가하면 됨.)

## 6. gen-wire (codegen) 경로

- 모노레포에선 `gateway-go`가 `andromeda/../gateway-go`. `scripts/gen-wire.mjs`는 이미
  **모노레포(부모에 gateway-go)·형제(`../Deneb/gateway-go`) 둘 다 자동 감지**하도록 준비됨
  (이 준비 PR에서 처리). 따라서 이동 시 추가 작업 없음. wire-drift CI만 클론 제거.

## 7. 툴링 정합

- **node/pnpm**: Andromeda는 node 22 + pnpm. Deneb의 node/pnpm 버전과 맞추거나, `andromeda/`를
  **독립 패키지**(자체 `node_modules`, 자체 lockfile)로 두는 게 가장 단순. pnpm workspace로
  묶는 건 선택(빌드 분리가 명확하면 독립 권장).
- 경로 의존(vite/tsconfig/tauri/`@/` alias)은 `andromeda/` 기준 상대라 이동에 안전.

## 8. 실행 체크리스트 + 되돌리기

순서대로(각 단계 후 검증):

- [x] **(안전) gen-wire 모노레포 대응** — 준비 PR #117.
- [x] **(안전) 경로 상대성 + 변경 표면 정적 감사** — 이동 시 빌드 무변경 확인, 수정 대상 2곳 확정(§0·§2).
- [ ] Deneb 레포에 `TAURI_SIGNING_PRIVATE_KEY` / `_PASSWORD` 시크릿 추가(**andromeda와 동일 키**).
- [ ] subtree 머지: andromeda → `Deneb/andromeda`.
- [ ] release-please: `andromeda` 패키지 추가(`component: "andromeda"`, extra-files, manifest) — `migration/release-please.md`.
- [ ] CI/build 워크플로 이관(`paths: andromeda/**`, andromeda 빌드 잡) — `migration/workflows.md`.
- [x] `tauri.conf.json` `updater.endpoints`에 **Deneb 추가(둘 다 나열)** — 이 준비 PR에서
      baking(Deneb 우선+andromeda 폴백). ⟶ **남은 액션**: 이동 전 andromeda 릴리즈 1회로
      이 빌드를 설치본에 전파(컷오버 안전성의 핵심).
- [ ] 전환기: Deneb 릴리즈가 andromeda `latest.json`도 게시.
- [ ] **검증**: Deneb에서 andromeda 릴리즈 1회 → **기존 설치본이 업데이트를 받는지 확인**(가장 중요).
- [ ] (흡수 후) andromeda 릴리즈/엔드포인트 정리.

**되돌리기**: subtree 머지 전까진 andromeda 레포가 정본이라 언제든 중단 가능. 머지 후에도
andromeda 레포는 릴리즈 연속성을 위해 유지하므로, 문제가 생기면 andromeda에서 다시 릴리즈해
롤백할 여지가 남는다. **키만 잃지 않으면** 복구 가능.
