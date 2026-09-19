"""Deneb adapters for the shared ./dev agent protocol.

Keep execution in the existing tools. This catalog owns discoverability,
working directories, prerequisites, effects, and examples, not their behavior.
"""
from dataclasses import dataclass


@dataclass(frozen=True)
class Tool:
    id: str
    summary: str
    command: tuple[str, ...] = ()
    source: str = ""
    cwd: str = "."
    effects: str = "read"
    docs: str = "docs/tools/agent-dev.md"
    examples: tuple[tuple[str, ...], ...] = ((),)
    requires: tuple[str, ...] = ()
    packages: tuple[str, ...] = ()
    paths: tuple[str, ...] = ()
    timeout: int = 60
    execution: str = "local"
    notes: str = ""
    keywords: str = ""


TOOLS = (
    Tool("env.status", "Inspect local tools, Python packages and repository requirements",
         execution="builtin", keywords="환경 설치 버전 상태"),
    Tool("env.setup", "Synchronize this host's isolated Deneb development environment",
         ("{python}", "scripts/dev/devenv_sync.py", "--apply", "--local"),
         source="scripts/dev/devenv_sync.py", effects="local-development-environment-write", timeout=2400,
         examples=((), ("--verify",)), docs="docs/tools/development-sync.md", keywords="환경 설치 동기화"),
    Tool("env.sync", "Synchronize the Mac, four servers and optional WSL development host",
         ("{python}", "scripts/dev/devenv_sync.py", "--apply"), source="scripts/dev/devenv_sync.py",
         effects="remote-development-environment-write", timeout=3600, requires=("ssh",),
         examples=(("--verify",), ("--hosts", "srv1", "srv2")), docs="docs/tools/development-sync.md",
         notes="The Mac is local when run on macOS. A Linux coordinator handles the five Linux hosts; the Mac's launchd job is separate. Existing feature branches and serving checkouts are preserved.",
         keywords="6대 서버 맥 환경 동기화"),
    Tool("env.check", "Compare each development host with the committed toolchain manifest",
         ("{python}", "scripts/dev/devenv_sync.py", "--check"), source="scripts/dev/devenv_sync.py",
         effects="remote-read-and-local-report", timeout=180, requires=("ssh",),
         examples=((), ("--local",)), docs="docs/tools/development-sync.md", keywords="6대 환경 상태 버전"),
    Tool("env.timer", "Install the explicit daily origin/main development sync job",
         ("{python}", "scripts/dev/devenv_sync.py", "--install-timer"), source="scripts/dev/devenv_sync.py",
         effects="install-local-recurring-job", docs="docs/tools/development-sync.md",
         notes="Explicit operator action only: install on the Linux coordinator and on the Mac after env.setup. Ordinary sync never enables a timer.",
         keywords="환경 자동 동기화 타이머"),
    Tool("env.doctor", "Run the existing development prerequisite diagnostic",
         ("bash", "scripts/check-dev-env.sh"), source="scripts/check-dev-env.sh",
         timeout=120, notes="The underlying script is advisory and always exits zero. Missing prerequisites are reported as incomplete.",
         keywords="환경 진단"),
    Tool("check", "Run the Go gateway checks and deterministic audit tests",
         ("make", "check"), source="Makefile", effects="build-and-test",
         requires=("go", "golangci-lint"), timeout=1800, keywords="게이트 검사 테스트"),
    Tool("ci.fast", "Run the existing gate for paths changed against origin/main",
         ("make", "ci/fast"), source="Makefile",
         requires=("make",), effects="build-and-test", timeout=1800,
         notes="Use only for a clear single-lane diff. Shared surfaces require ci; Andromeda requires andromeda.verify. CI_CHECK_BASE overrides the comparison base. The scripts lane's shell fixtures require Linux/GNU behavior.",
         keywords="변경 검증 검사 빠른"),
    Tool("ci", "Run the full existing local gate",
         ("make", "ci"), source="Makefile", effects="build-and-test", timeout=3600,
         examples=((), ("ARGS=--go",), ("ARGS=--kotlin",), ("ARGS=--scripts",)),
         keywords="전체 검증 검사"),
    Tool("python.test", "Run support, audit and evaluation Python behavior tests",
         ("make", "python-test"), source="Makefile", effects="test", packages=("PyYAML",),
         timeout=1800, notes="The full suite includes operational shell fixtures that require Linux/GNU behavior. Use Linux CI or a Linux container for that lane on macOS.",
         keywords="파이썬 테스트"),
    Tool("python.lint", "Lint Python scripts with the selected interpreter",
         ("{python}", "-m", "ruff", "check", "scripts"), packages=("ruff",),
         keywords="파이썬 린트"),
    Tool("shell.lint", "Run ShellCheck on tracked shell scripts",
         ("make", "shell-lint"), source="Makefile", requires=("shellcheck",), timeout=120,
         notes="Missing ShellCheck blocks execution instead of accepting the native target's skip as success.",
         keywords="셸 린트"),
    Tool("docs.lint", "Validate source references in agent documentation",
         ("make", "doc-ref-lint"), source="Makefile",
         timeout=120, keywords="문서 참조 검사"),
    Tool("gateway.build", "Build the Go gateway", ("make", "go"), source="Makefile",
         requires=("go",), effects="build", timeout=1800, keywords="게이트웨이 빌드"),
    Tool("gateway.test", "Run uncached Go tests", ("make", "go-test"), source="Makefile",
         requires=("go",), effects="test", timeout=1800, keywords="게이트웨이 테스트"),
    Tool("andromeda.verify", "Typecheck, lint, format-check, test and build the desktop client",
         ("pnpm", "verify"), source="andromeda/package.json", cwd="andromeda",
         requires=("node",), paths=("andromeda/node_modules",), effects="build-and-test",
         timeout=1800, keywords="데스크톱 검증"),
    Tool("andromeda.test", "Run desktop client behavior tests", ("pnpm", "test"),
         source="andromeda/package.json", cwd="andromeda", requires=("node",),
         paths=("andromeda/node_modules",), effects="test", timeout=600,
         keywords="데스크톱 테스트"),
    Tool("native.check", "Run Kotlin native client checks", ("make", "kotlin-check"),
         source="Makefile", requires=("java",), effects="build-and-test", timeout=3600,
         notes="Requires the Android SDK and the native lane setup in CLAUDE.md.",
         keywords="모바일 안드로이드 코틀린 검사"),
    Tool("live.status", "Inspect the development gateway process",
         ("bash", "scripts/dev/live-test.sh", "status"), source="scripts/dev/live-test.sh",
         requires=("curl",), effects="dev-server-request",
         keywords="서버 상태"),
    Tool("live.smoke", "Check health and readiness of the development gateway",
         ("bash", "scripts/dev/live-test.sh", "smoke"), source="scripts/dev/live-test.sh",
         requires=("curl",), effects="dev-server-request", timeout=120,
         keywords="서버 라이브 검증"),
    Tool("live.restart", "Rebuild and restart the development gateway",
         ("bash", "scripts/dev/live-test.sh", "restart"), source="scripts/dev/live-test.sh",
         requires=("go",), effects="restart-dev-server", timeout=1800,
         notes="Uses the existing live-test configuration and state. Run only when the task calls for a dev server restart.",
         keywords="서버 재시작"),
    Tool("live.logs", "Read development gateway logs",
         ("bash", "scripts/dev/live-test.sh", "logs"), source="scripts/dev/live-test.sh",
         examples=(("50",),), keywords="서버 로그"),
    Tool("rpcmap", "Resolve RPC, tool and event names to source handlers",
         ("{python}", "scripts/dev/rpcmap.py"), source="scripts/dev/rpcmap.py",
         examples=(("miniapp.people.list", "--json"), ("--handler", "peopleList")),
         keywords="핸들러 메서드 탐색"),
    Tool("codegraph", "Inspect source symbols, callers and change impact", ("codegraph",),
         effects="arguments-dependent", examples=(("node", "peopleList"), ("impact", "peopleList")),
         notes="Use the existing per-worktree graph. Index creation or upgrades are separate actions.",
         keywords="코드 관계 영향 탐색"),
    Tool("codegraph.doctor", "Check the existing CodeGraph wiring and index",
         ("{python}", "scripts/dev/codegraph_doctor.py", "--strict"),
         source="scripts/dev/codegraph_doctor.py", timeout=120, keywords="코드그래프 진단"),
    Tool("pr.watch", "Wait for pull request checks using the existing gate",
         ("bash", "scripts/dev/pr.sh", "watch"), source="scripts/dev/pr.sh",
         requires=("gh", "git"), effects="remote-read", examples=(("<pr-number>",),),
         timeout=3600, keywords="피알 체크"),
    Tool("pr.land", "Watch checks, squash merge, verify origin/main and clean the remote branch",
         ("bash", "scripts/dev/pr.sh", "land"), source="scripts/dev/pr.sh",
         requires=("gh", "git"), effects="merge-pr-and-delete-remote-branch",
         examples=(("<pr-number>",),), timeout=3600,
         notes="Use only for a requested merge. A timeout cannot roll back remote actions; inspect the PR before retrying.",
         keywords="피알 머지 병합"),
    Tool("rg", "Search literal text", ("rg",), examples=(("--files",),), keywords="문자열 검색"),
    Tool("fd", "Find repository files", ("fd",), effects="arguments-dependent",
         examples=(("AGENTS.md",),), keywords="파일 검색"),
    Tool("jq", "Inspect and transform JSON", ("jq",), effects="arguments-dependent",
         examples=(("--version",),)),
    Tool("git", "Inspect or change repository state", ("git",), effects="arguments-dependent",
         examples=(("status", "--short", "--branch"),)),
    Tool("gh", "Inspect or change GitHub state", ("gh",), effects="arguments-dependent",
         examples=(("pr", "status"),)),
    Tool("wt", "Inspect or manage worktrees", ("wt",), effects="arguments-dependent",
         examples=(("list",),), docs="docs/tools/worktrunk.md"),
    Tool("uv", "Inspect or manage Python environments", ("uv",), effects="arguments-dependent",
         examples=(("--version",),)),
    Tool("hyperfine", "Benchmark command duration", ("hyperfine",), effects="executes-arguments",
         examples=(("--help",),)),
)

BY_ID = {tool.id: tool for tool in TOOLS}
DISCOVERY_DIRS = ("scripts",)
PACKAGE_DIRS = ("andromeda", "even-g2")
