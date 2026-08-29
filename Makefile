# Deneb Build
#
# Pure Go gateway build (Rust core has been removed).

.PHONY: all \
       go go-run go-dev go-test skill-eval-test go-vet go-fmt go-lint go-clean go-bench go-binary gateway-prod wormhole briefcase briefcase-test briefcase-smoke \
       test clean check check-go fmt generate generate-check quality-gate \
       tool-schemas tool-schemas-check \
       data-gen data-gen-check \
       kotlin-models kotlin-models-check \
       kotlin-check kotlin-spotless kotlin-detekt kotlin-desktop-test kotlin-desktop-smoke-test kotlin-android-compile \
       docs-lint docs-lint-fix \
       ci ci/fast go-test-cached \
       health health-check health-v2 health-v2-check health-v2-deep health-v2-test health-v2-baseline \
       health-v3 health-v3-check health-v3-deep health-v3-test health-v3-baseline \
       rsi-bench rsi-bench-check rsi-bench-deep rsi-bench-test rsi-bench-baseline \
       bench-check bench-refresh \
       runtime-health runtime-health-test python-test python-lint shell-lint shell-behavior-test doc-ref-lint state-register \
       preview render-golden-check render-golden-update native-smoke \
       info

# Version from git tags (release-please format: deneb-vX.Y.Z), injected via ldflags.
# Uses the latest deneb-v* tag by version sort, regardless of current branch ancestry.
DENEB_VERSION := $(shell git tag --sort=-v:refname --list 'deneb-v*' 2>/dev/null | head -1 | sed 's/^deneb-v//')
# Build timestamp for the downgrade guard's same-version tiebreak: two builds
# can carry the same tag version (every deploy between releases does), so the
# guard needs a monotonic-ish stamp to spot a stale-checkout build whose TAGS
# are current but whose CODE is old.
DENEB_BUILD_UNIX := $(shell date +%s)
GO_LDFLAGS := -ldflags '-s -w -X main.Version=$(DENEB_VERSION) -X github.com/choiceoh/deneb/gateway-go/internal/runtime/bootstrap.BuildUnix=$(DENEB_BUILD_UNIX)'

# DGX Spark unified-memory guard for the Go toolchain.
#
# On GB10 the GPU shares system RAM, so resident sidecar models (vLLM, OCR, ASR)
# eat into the headroom the Go toolchain can use. `go build`/`test`/`vet` default
# to one build action per CPU (20 on this box), and each compile/link/test binary
# can peak at a few GB — enough to OOM the host when free memory is already low.
#
# GO_PAR caps the parallel build/test actions, budgeting ~4 GB per action against
# current MemAvailable, clamped to [2, NPROC]. So a busy box (little free RAM)
# falls back toward `-p 2`, while an idle one uses every core. Override explicitly
# with `make go GO_PAR=4`, or export GOGC=50 to trade build speed for a smaller
# heap. CI runs on a dedicated 16-vCPU runner and calls `go` directly (not make),
# so this guard only ever affects local DGX builds — never CI timing.
GO_PAR ?= $(shell \
	mem_gb=$$(awk '/MemAvailable/ {print int($$2/1024/1024)}' /proc/meminfo 2>/dev/null || echo 8); \
	cpu=$$(nproc 2>/dev/null || echo 4); \
	par=$$((mem_gb / 4)); \
	[ $$par -gt $$cpu ] && par=$$cpu; \
	[ $$par -lt 2 ] && par=2; \
	echo $$par)

# Fix NO_PROXY for Claude Code web containers: Go module proxy uses googleapis.com,
# but NO_PROXY includes *.googleapis.com which makes Go bypass the egress proxy and
# attempt direct UDP DNS (blocked). Strip those entries so Go traffic routes through proxy.
ifneq ($(CLAUDE_CODE_PROXY_RESOLVES_HOSTS),)
_CLEAN_NO_PROXY := $(shell echo "$(NO_PROXY)" | sed 's/\*\.googleapis\.com//g; s/\*\.google\.com//g' | sed 's/,,*/,/g; s/^,//; s/,$$//')
GO_ENV := NO_PROXY="$(_CLEAN_NO_PROXY)" no_proxy="$(_CLEAN_NO_PROXY)"
else
GO_ENV :=
endif

# Ensure Go toolchain binaries (golangci-lint, etc.) are on PATH.
export PATH := $(HOME)/go/bin:$(PATH)

# Android SDK location for the native client's gradle gates (spotless/detekt).
# Mirrors the scripts/dev convention (default ~/android-sdk) and is exported so
# the gradle wrapper picks it up — `make ci` / `make kotlin-check` then run the
# Kotlin lint gate with no manual env setup (the gap that let gofmt/spotless CI
# failures slip past local checks). Override with `make kotlin-check ANDROID_HOME=...`.
ANDROID_HOME ?= $(HOME)/android-sdk
export ANDROID_HOME

# Default: build Go gateway.
all: go

# --- Go gateway ---

go:
	cd gateway-go && $(GO_ENV) CGO_ENABLED=0 go build -p $(GO_PAR) $(GO_LDFLAGS) ./...

go-run: go
	cd gateway-go && $(GO_ENV) go run ./cmd/gateway/

# Dev mode: build and run gateway with auto-restart on SIGUSR1 (exit code 75).
# Uses go build instead of go run to avoid signal forwarding issues.
go-dev:
	@echo "Starting Go gateway in dev mode (auto-restart on SIGUSR1)..."
	@while true; do \
		if ! $(GO_ENV) CGO_ENABLED=0 go build -C gateway-go -p $(GO_PAR) $(GO_LDFLAGS) -o /tmp/deneb-gateway-dev ./cmd/gateway/; then \
			echo "[go-dev] Build failed, aborting."; \
			exit 1; \
		fi; \
		/tmp/deneb-gateway-dev $(ARGS); \
		EXIT=$$?; \
		if [ $$EXIT -eq 75 ]; then \
			echo "[go-dev] Restarting gateway (SIGUSR1)..."; \
			sleep 0.5; \
			continue; \
		fi; \
		echo "[go-dev] Gateway exited with code $$EXIT"; \
		exit $$EXIT; \
	done

go-test:
	cd gateway-go && $(GO_ENV) CGO_ENABLED=0 go test -p $(GO_PAR) -count=1 ./...

# Focused skill-eval gate. The authoritative go-test/CI command already runs
# these tests; this target gives skill authors a fast local check for real
# frontmatter trigger fixtures and with-skill/no-skill ablation contracts.
skill-eval-test:
	cd gateway-go && $(GO_ENV) CGO_ENABLED=0 go test -count=1 ./internal/domain/skills \
		-run 'TestBundledSkillTriggerEvals|TestMatchSkillTriggers'
	cd gateway-go && $(GO_ENV) CGO_ENABLED=0 go test -count=1 ./internal/domain/skills/genesis \
		-run 'TestEvaluateAblation|TestTrackerSkillAblation|TestSkillAblationTask|TestBuildReplayExecutorPromptHasExplicitNoSkillControl'

# Cached test variant for the fast inner-loop gate (make ci/fast): drops
# -count=1 so Go's test cache serves unchanged packages and only re-runs what a
# change actually invalidated. Not for the authoritative gate — the cache can
# mask flakes — so plain `go-test` (with -count=1) stays the one CI mirrors.
go-test-cached:
	cd gateway-go && $(GO_ENV) CGO_ENABLED=0 go test -p $(GO_PAR) ./...

go-vet:
	cd gateway-go && $(GO_ENV) go vet -p $(GO_PAR) ./...

go-fmt:
	@cd gateway-go && test -z "$$(go run mvdan.cc/gofumpt@v0.10.0 -l .)" || (echo "Go files need formatting (gofumpt -w .):"; go run mvdan.cc/gofumpt@v0.10.0 -l .; exit 1)

# Lint only new/changed Go code (safe for CI gate on existing codebases).
go-lint:
	cd gateway-go && golangci-lint run --new ./...

# Full lint audit (all existing code). Use for periodic cleanup.
go-lint-all:
	cd gateway-go && golangci-lint run ./...

go-binary:
	cd gateway-go && $(GO_ENV) CGO_ENABLED=0 go build -trimpath -p $(GO_PAR) $(GO_LDFLAGS) -o ../dist/deneb-gateway ./cmd/gateway/


# Build production gateway binary to dist/.
gateway-prod:
	$(MAKE) go-binary
	@echo "Production gateway ready: dist/deneb-gateway"

# Build the wormhole model router binary to dist/ (cmd/wormhole). Managed as a
# sibling service (scripts/deploy/start-wormhole.sh, wormhole.service).
wormhole:
	cd gateway-go && $(GO_ENV) CGO_ENABLED=0 go build -trimpath -p $(GO_PAR) $(GO_LDFLAGS) -o ../dist/wormhole ./cmd/wormhole/
	@echo "wormhole router ready: dist/wormhole"

# Build the standalone, fail-closed Deneb-Briefcase runner.
briefcase:
	@mkdir -p dist
	cd gateway-go && $(GO_ENV) CGO_ENABLED=0 go build -trimpath -p $(GO_PAR) $(GO_LDFLAGS) -o ../dist/deneb-briefcase ./cmd/deneb-briefcase/
	@echo "Deneb-Briefcase ready: dist/deneb-briefcase"

briefcase-test:
	cd gateway-go && $(GO_ENV) CGO_ENABLED=0 go test -p $(GO_PAR) -count=1 \
		./cmd/deneb-briefcase \
		./internal/ai/llm \
		./internal/ai/tokenest \
		./internal/ai/agent \
		./internal/domain/briefcase \
		./internal/domain/wiki \
		./internal/runtime/briefcase \
		./internal/eval/briefcase \
		./internal/eval/briefcase/closedloop \
		./internal/pipeline/chat/toolpreset \
		./internal/pipeline/chat/prompt \
		./internal/pipeline/chat/tools \
		./internal/pipeline/chat \
		./internal/pipeline/polaris \
		./pkg/atomicfile

briefcase-smoke:
	cd gateway-go && $(GO_ENV) go run ./cmd/deneb-briefcase validate \
		--case ../bench/briefcase/portable/budget-supersession-v1
	cd gateway-go && $(GO_ENV) go test -count=1 ./cmd/deneb-briefcase \
		-run 'TestPortableSmokeCaseRemainsSignedAndValid|TestScoreCommand'

go-clean:
	cd gateway-go && go clean ./...

# Run Go benchmarks with memory allocation stats.
go-bench:
	cd gateway-go && $(GO_ENV) go test -bench=. -benchmem -run='^$$' ./...

# --- Combined operations ---

test: go-test
	@echo "Go tests passed"

clean: go-clean
	@echo "Cleaned Go build artifacts"

check-go: go-fmt go-vet go-lint go-test

# Full check: generate-check first (sequential), then Go, deterministic audit
# checks, and the opt-in Korean-response quality regression gate.
# Health Bench 2.0 is out of CI entirely (operator decision 2026-07-24): both the
# pillar ratchet AND the scorer unit tests were removed from PR CI and Nightly Drift.
# The score/ratchet is now manual-only via `make health-v2-check`. The scorer unit
# tests stay in this LOCAL `make check` so edits to the scorer itself don't rot.
check: generate-check check-go runtime-health-test health-v2-test quality-gate
	@echo "All checks passed"

# Fast check: format + vet + lint only (no tests). Good for pre-commit gate.
check/fast: go-fmt go-vet go-lint
	@echo "Fast checks passed (fmt + vet + lint, no tests)"

# Korean-response quality regression gate. It is disabled by default so local
# and CI `make check` runs stay deterministic off-DGX. Set DENEB_QUALITY_GATE=1
# on a live model-capable host to run the end-to-end quality metric and compare
# it against this branch's saved baseline.
quality-gate:
	@bash scripts/dev/quality-gate.sh

# Codebase structural-health score (advisory — like scripts/audit/deadcode-audit.sh,
# NOT part of `make check`/`ci`). Aggregates file-size discipline, layer cohesion,
# test presence, and doc coverage into one 0-100 number + per-dimension detail, so
# structural rot that no hard gate tracks surfaces in review. See scripts/audit/
# codebase-health.py. Add --deep for the (slow) deadcode dimension.
health:
	@python3 scripts/audit/codebase-health.py

# Ratchet gate: fail if the composite health score regressed below the checked-in
# floor (scripts/audit/health-baseline.json). Raise the floor after a genuine
# improvement with: python3 scripts/audit/codebase-health.py --update (operator approval).
health-check:
	@python3 scripts/audit/codebase-health.py --check

# Health Bench 2.0 measures change locality, boundary/contract clarity, test
# behavior, runtime diagnosability, AI change readiness, and role-appropriate
# CI evidence weighted by product impact. It intentionally lives beside v1:
# scores from the two rubrics are not comparable. The default report leads with actionable
# interventions; see docs/agent-rules/codebase-health-v2.md.
health-v2:
	@python3 scripts/audit/codebase-health-v2.py

# Multi-axis ratchet (operator/nightly). PR CI runs this advisory-only; `make
# check` no longer fails closed on it. New *critical* findings and score drops
# still fail when this target is invoked explicitly.
health-v2-check:
	@python3 scripts/audit/codebase-health-v2.py --check

# Executed evidence profile: fresh shuffled tests + coverage, vet, lint, and
# race detection. This is intentionally slower and uses a distinct profile.
health-v2-deep:
	@python3 scripts/audit/codebase-health-v2.py --deep --require-readiness

# Stdlib-only scorer regression and anti-gaming fixtures.
health-v2-test:
	@python3 -m unittest discover -s scripts/audit -p 'test_codebase_health_v2*.py' -v

# validate-or-freeze for doc-embedded code references (Harness Handbook,
# arXiv:2607.13285): agent docs must not silently point at moved/deleted code.
# Strict on BROKEN (missing source paths / dead line anchors); symbol drift
# stays advisory inside the report. Sub-second.
doc-ref-lint:
	@python3 scripts/audit/doc_ref_lint.py --strict

# Cross-stage shared-state read/write map (Harness Handbook state-register
# view). Regenerates the committed session-state blast-radius doc; advisory
# (not a CI gate) — rerun after touching session fields or their consumers.
state-register:
	@cd gateway-go && go run ./cmd/state-register -out ../docs/research/state-register-session.md
	@cd gateway-go && go run ./cmd/state-register -type internal/domain/workfeed.Item \
	  -out ../docs/research/state-register-workfeed.md
	@echo "regenerated docs/research/state-register-{session,workfeed}.md"

# TS twin of state-register: the TypeScript checker over the workstation's
# shared context value (WorkspaceCtx). Uses the andromeda `typescript` devdep,
# no new deps. Advisory (not a CI gate) — rerun after touching the context type.
state-register-ts:
	@cd andromeda && node --experimental-strip-types scripts/state-register.ts \
	  --out ../docs/research/state-register-workstation.md
	@echo "regenerated docs/research/state-register-workstation.md"

# Kotlin twin of state-register: the Kotlin K1 compiler frontend (BindingContext)
# over the chat client's shared UI state (ChatUiState). Uses kotlin-compiler-
# embeddable jars ALREADY in the gradle cache — nothing added to the KMP build
# graph. Advisory. data class는 불변이라 write=.copy(field=…). Requires a prior
# client-android build to populate the cache.
state-register-kt:
	@client-android/tools/state-register/run.sh
	@echo "regenerated docs/research/state-register-chat-ui.md"

# Semantic code search: Nemotron embeddings over CodeGraph symbols plus tracked
# repository chunks, RRF-fused with BM25+FTS. Index lives in .codegraph/
# (gitignored), incremental by node timestamp/content hash.
codesearch-index:
	@cd gateway-go && go run ./cmd/codesearch index

codesearch:
	@cd gateway-go && go run ./cmd/codesearch query "$(Q)"

codesearch-bench:
	@cd gateway-go && go run ./cmd/codesearch bench

# 정본 사실 평면의 라이프사이클 점수 (recall-bench는 page-only이므로 이 평면을
# 측정하지 않는다). 정정·삭제 후 stale 노출 0이 게이트, 검색 노출은 advisory.
fact-bench:
	@cd gateway-go && go run ./cmd/fact-bench

# 직접 기억 문법이 놓친 명령들을 묶어 축 확장 후보로 보여준다 (읽기 전용).
memory-grammar-misses:
	@python3 scripts/dev/memory-grammar-misses.py

# 메모리 파일(레포 밖)의 코드 참조 감사 — advisory, CI 밖. 메모리 규칙의
# "회상된 file:line은 검증하라"를 일괄 실행하는 수동/주기용 진입점.
memory-ref-audit:
	@python3 scripts/audit/doc_ref_lint.py --glob '__none__' \
	  --extra-docs "$(HOME)/.claude/projects/-home-choiceoh-deneb/memory"

# All checked-in Python behavior tests. Keep the two roots isolated so their
# fixture/support modules resolve exactly as they do when invoked directly.
# Ops/dev scripts always; audit suites except Health Bench 2.0 (covered by
# `make health-v2-test` / the code-health CI job — avoid double-running ~60 cases
# and attributing scorer failures to the "Python support tooling" lane).
python-test:
	@cd scripts/audit && \
	  mods=$$(for f in test_*.py; do rest=$${f#test_codebase_health_v2}; if [ "$$rest" = "$$f" ]; then printf '%s ' "$${f%.py}"; fi; done); \
	  python3 -m unittest $$mods -v
	@python3 -m unittest discover -s scripts/dev -p 'test_*.py' -v
	@python3 -m unittest discover -s scripts/eval -p 'test_*.py' -v

# Static analysis for support, audit, and deployment Python. CI installs the
# hash-locked toolchain from requirements-dev.lock before invoking this target.
python-lint:
	@python3 -m ruff check scripts

# Operational scripts are checked at warning severity: correctness and safety
# findings gate delivery, while ShellCheck's style-only suggestions stay advisory.
shell-lint:
	@git ls-files -z 'scripts/*.sh' 'scripts/**/*.sh' | xargs -0 shellcheck --severity=warning

# Deterministic behavioral coverage for the operational topology shell. This is
# named separately in CI so a deployment-script contract failure is visible
# without searching the full Python support-tooling log.
shell-behavior-test:
	@python3 -m unittest discover -s scripts/audit -p 'test_topology_parity_shell.py' -v

# One-way baseline update. Review the report and diff; the command refuses to
# lower the composite or any pillar and refuses new high/critical findings.
health-v2-baseline:
	@python3 scripts/audit/codebase-health-v2.py --update-baseline

# Health Bench 3.0 — structure + runtime + RSI fitness (geometric composite).
# Design: docs/research/health-bench-3.0.md. Scores are not comparable to v2.
health-v3:
	@python3 scripts/audit/health-bench-v3.py

health-v3-check:
	@python3 scripts/audit/health-bench-v3.py --check

health-v3-deep:
	@python3 scripts/audit/health-bench-v3.py --deep --refresh-runtime-cache

health-v3-test:
	@python3 -m unittest discover -s scripts/audit -p 'test_codebase_health_v3*.py' -v

health-v3-baseline:
	@python3 scripts/audit/health-bench-v3.py --update-baseline

# RSI Bench 1.0 — process (acceptor honesty) + utility (land/verdict) geometric
# composite. Design: docs/research/rsi-bench.md. Not comparable to Health 3.0.
rsi-bench:
	@python3 scripts/audit/rsi-bench.py

rsi-bench-check:
	@python3 scripts/audit/rsi-bench.py --check

# Combined structural + RSI ratchet (PR / operator gate).
bench-check: health-v3-check rsi-bench-check

rsi-bench-deep:
	@python3 scripts/audit/rsi-bench.py --deep --refresh-cache

# Force-overwrite health-v3 + rsi-bench snapshots (gitignored). Prefer the
# daily systemd timer on the gateway host: scripts/systemd/setup-bench-refresh.sh
bench-refresh:
	@bash scripts/audit/refresh-bench-snapshots.sh

rsi-bench-test:
	@python3 -m unittest discover -s scripts/audit -p 'test_rsi_bench*.py' -v

rsi-bench-baseline:
	@python3 scripts/audit/rsi-bench.py --update-baseline --migrate-rubric --expect-band 25:50

# Runtime-health score (advisory, on-host only — reads the production gateway's
# journald over a rolling window, so it is NON-deterministic and has NO ratchet
# gate). Sibling to `health`: that scores static structure, this scores the LIVE
# runtime (crashes, error rate, LLM-serving faults, turn/tool reliability,
# latency) from the last 7 days of logs. Run on srv4. See runtime-health.py.
runtime-health:
	@python3 scripts/audit/runtime-health.py

# Recall evaluation-loop health against the LIVE wiki (advisory, operator-run):
# gold-set retrieval quality + production ledger utility + gold coverage of the
# live project roster + a composite recall-health score. Needs the embedding
# server; runs on a throwaway copy so prod is untouched. `ARGS=--emit-gold` also
# prints deterministic gold candidates for uncovered projects (curated append).
recall-health:
	@scripts/dev/recall-health.sh $(ARGS)

# Deterministic parser/scoring regression tests for the advisory runtime metric.
# Safe in CI: fixtures only, never reads the host journal.
runtime-health-test:
	@python3 -m unittest discover -s scripts/audit -p 'test_runtime_health.py' -v

# Cache-aware cost decomposition over ~/.deneb/agent-logs (advisory, read-only):
# per-model uncached/read/write split with price-weighted volume, within-run
# reuse stalls, run-boundary prefix survival, beyond-cutoff tool re-calls.
# Decision metric for context-reduction changes (arXiv:2607.12161 adoption).
cache-cost-audit:
	@python3 scripts/audit/cache-cost-audit.py $(ARGS)

cache-cost-audit-test:
	@python3 -m unittest discover -s scripts/audit -p 'test_cache_cost_audit.py' -v

# deneb-ui card adoption (advisory) — "card authored" vs "adoption miss" from the
# gateway journal, split by session class. The operator-facing rate is the one
# that matters; automated lanes run far higher and hide it when averaged in.
card-adoption:
	@python3 scripts/audit/card-adoption.py $(ARGS)

card-adoption-test:
	@python3 -m unittest discover -s scripts/audit -p 'test_card_adoption.py' -v

# Agent-doc coverage (advisory) — rank gateway-go subsystems by weight and flag
# heavy ones with no module CLAUDE.md and no agent-rule glob, i.e. the subsystems
# most worth a narrative doc. Draft one with Deneb's own model (grounded in source
# + call graph) via: scripts/audit/doc-draft.py --target <pkg> --name <slug>.
# The draft is a *.draft.md an agent curates before it lands. See doc-draft.py.
doc-draft-gaps:
	@python3 scripts/audit/doc-draft.py --list-gaps

# Run all code generation pipelines in dependency order.
generate: tool-schemas data-gen kotlin-models
	@echo "All code generation pipelines completed"

# Verify generated sources are up to date.
# Runs each generation domain independently so failures name the broken group.
generate-check:
	@echo "==> [1/3] tool schemas (tool_schemas.json -> tool_schemas_gen.go)"
	@$(MAKE) tool-schemas-check
	@echo "==> [2/3] data tables (*.json -> *_gen.go)"
	@$(MAKE) data-gen-check
	@echo "==> [3/3] kotlin wire models (Go //deneb:wire -> MiniappWireTypes.kt)"
	@$(MAKE) kotlin-models-check
	@echo "All generation checks passed"

fmt:
	cd gateway-go && go run mvdan.cc/gofumpt@v0.10.0 -w .

# --- Tool schema code generation ---

# Regenerate gateway-go/internal/pipeline/chat/toolwire/schema/tool_schemas_gen.go from tool_schemas.json.
tool-schemas:
	cd gateway-go && go run cmd/tool-schema-gen/main.go \
		-json internal/pipeline/chat/toolwire/schema/tool_schemas.json \
		-out  internal/pipeline/chat/toolwire/schema/tool_schemas_gen.go \
		-pkg  schema

# Verify tool_schemas_gen.go is up to date (fails if json and Go are out of sync).
tool-schemas-check:
	cd gateway-go && go run cmd/tool-schema-gen/main.go \
		-json internal/pipeline/chat/toolwire/schema/tool_schemas.json \
		-out  internal/pipeline/chat/toolwire/schema/tool_schemas_gen.go \
		-pkg  schema
	@git diff --exit-code -- gateway-go/internal/pipeline/chat/toolwire/schema/tool_schemas_gen.go

# --- Data table code generation ---
#
# Universal JSON -> Go var generator for data tables (tool classification).
# Source JSON files live next to their generated Go counterparts.

DATA_GEN = go run cmd/data-gen/main.go
DATA_GEN_TARGETS = \
	internal/pipeline/chat/tool_classification

data-gen:
	@cd gateway-go && for t in $(DATA_GEN_TARGETS); do \
		$(DATA_GEN) -json $${t}.json -out $${t}_gen.go; \
	done

data-gen-check:
	@cd gateway-go && for t in $(DATA_GEN_TARGETS); do \
		$(DATA_GEN) -json $${t}.json -out $${t}_gen.go; \
	done
	@git diff --exit-code -- $(addprefix gateway-go/,$(addsuffix _gen.go,$(DATA_GEN_TARGETS)))

# --- Kotlin wire model code generation ---
#
# Generates the native client's @Serializable wire types from the Go miniapp
# handler structs marked //deneb:wire, so the client and the gateway share one
# source of truth for RPC response shapes. The check target is non-mutating
# (compares against the committed file) and gates schema drift in CI.

KOTLIN_MODELS_SRC = internal/runtime/rpc/handler/handlerminiapp
KOTLIN_MODELS_OUT = ../client-android/app/composeApp/src/commonMain/kotlin/ai/deneb/deneb/generated/MiniappWireTypes.kt
KOTLIN_MODELS_TEST_OUT = ../client-android/app/composeApp/src/commonTest/kotlin/ai/deneb/deneb/generated/MiniappWireDescriptorContractTest.kt
KOTLIN_MODELS_FIELD_TEST_OUT = ../client-android/app/composeApp/src/commonTest/kotlin/ai/deneb/deneb/generated/MiniappWireFieldBoundaryContractTest.kt
KOTLIN_MODELS_NULL_TEST_OUT = ../client-android/app/composeApp/src/commonTest/kotlin/ai/deneb/deneb/generated/MiniappWireNullCompatibilityTest.kt
KOTLIN_MODELS_VALUE_TEST_OUT = ../client-android/app/composeApp/src/commonTest/kotlin/ai/deneb/deneb/generated/MiniappWireValueContractTest.kt
KOTLIN_MODELS_PKG = ai.deneb.deneb.generated

kotlin-models:
	cd gateway-go && go run cmd/kotlin-models-gen/main.go \
		-src $(KOTLIN_MODELS_SRC) \
		-out $(KOTLIN_MODELS_OUT) \
		-test-out $(KOTLIN_MODELS_TEST_OUT) \
		-field-test-out $(KOTLIN_MODELS_FIELD_TEST_OUT) \
		-null-test-out $(KOTLIN_MODELS_NULL_TEST_OUT) \
		-value-test-out $(KOTLIN_MODELS_VALUE_TEST_OUT) \
		-pkg $(KOTLIN_MODELS_PKG)

kotlin-models-check:
	@# Drift-check gateway-go/cmd/kotlin-models-gen/main.go outputs with -check.
	cd gateway-go && go run cmd/kotlin-models-gen/main.go \
		-src $(KOTLIN_MODELS_SRC) \
		-out $(KOTLIN_MODELS_OUT) \
		-test-out $(KOTLIN_MODELS_TEST_OUT) \
		-field-test-out $(KOTLIN_MODELS_FIELD_TEST_OUT) \
		-null-test-out $(KOTLIN_MODELS_NULL_TEST_OUT) \
		-value-test-out $(KOTLIN_MODELS_VALUE_TEST_OUT) \
		-pkg $(KOTLIN_MODELS_PKG) \
		-check

# --- Kotlin client lint gates (native client) ---
#
# Mirror the kotlin-lint.yml CI gate locally: spotlessCheck = ktlint formatting,
# detekt = bug-focused static analysis (config/detekt.yml, baseline in
# config/detekt-baseline.xml). These are GATES — never auto-edit the detekt
# baseline to silence findings (docs/agent-rules/testing.md). Until now they had no
# make target, so the only way to check the native client before push was a manual
# `ANDROID_HOME=... ./gradlew ...`; that gap is what `make ci` closes.
#
# Local runs keep the gradle daemon (faster on repeat); CI uses --no-daemon on
# fresh runners. The daemon only affects speed, not the pass/fail outcome.
KOTLIN_APP_DIR = client-android/app

kotlin-spotless:
	cd $(KOTLIN_APP_DIR) && ./gradlew spotlessCheck --console=plain

kotlin-detekt:
	cd $(KOTLIN_APP_DIR) && ./gradlew detekt --console=plain

kotlin-desktop-test:
	cd $(KOTLIN_APP_DIR) && ./gradlew desktopTest --console=plain --no-configuration-cache

# Backward-compatible alias for scripts that used the former allowlisted smoke target.
kotlin-desktop-smoke-test: kotlin-desktop-test

# Android compile gate. compileKotlinDesktop (the desktop smoke + renderPreviews
# path) only type-checks the desktop source set, so two whole classes of break
# reach publish-apk undetected:
#   - composeApp:compileAndroidMain — commonMain code that references an import or
#     API resolved only on the Android target (an androidMain expect/actual, an
#     Android-only dependency). Compiles for desktop, fails for Android (#2698).
#   - androidApp:compileFossReleaseKotlin — the androidApp module itself (its
#     sources never touch the desktop target). A stale androidApp reference is
#     invisible to every desktop gate (#2702).
# Both are exactly the tasks the fossRelease APK build depends on, so this turns
# "green CI, red publish-apk" into a pre-merge failure. Android compilation does
# NOT need Kotlin/Native, so the linux-aarch64 host warning is irrelevant here.
kotlin-android-compile:
	cd $(KOTLIN_APP_DIR) && ./gradlew :composeApp:compileAndroidMain :androidApp:compileFossReleaseKotlin --console=plain

# Native client gate: formatting + bug-lint + the full desktop JVM suite + Android
# compile (matches kotlin-lint.yml).
kotlin-check: kotlin-spotless kotlin-detekt kotlin-desktop-test kotlin-android-compile
	@echo "Kotlin client checks passed"

# --- Docs lint gate (Mintlify markdown) ---
#
# markdownlint-cli2 over docs/** + README.md; globs and rule config live in the
# repo-root .markdownlint-cli2.jsonc. A pinned version keeps local == CI (docs.yml
# runs `make docs-lint`) and stops a new upstream rule from silently breaking the
# gate. Node/npx only — no repo-root node_modules. `docs-lint-fix` applies the
# auto-fixable subset in place.
MARKDOWNLINT_VERSION = 0.18.1

docs-lint:
	npx --yes markdownlint-cli2@$(MARKDOWNLINT_VERSION)

docs-lint-fix:
	npx --yes markdownlint-cli2@$(MARKDOWNLINT_VERSION) --fix

# --- CI gate mirror (single pre-push command) ---
#
# One command that runs the fast PR gates locally — Go (generate-check, fmt, vet,
# lint, test) AND the native client (spotless, detekt, desktop smoke tests) — and
# reports a per-gate PASS/FAIL summary with offender detail for failures. Unlike
# `make check` it continues past the first failure, so a single run surfaces the
# whole local pre-push set in one pass. Go, Kotlin, and deterministic audit lanes
# run in parallel since gradle startup is the long pole.
#
#   make ci                  # all gates (Go + Kotlin + audit)
#   make ci ARGS=--go        # Go gates only (skip the gradle/Kotlin lane)
#   make ci ARGS=--kotlin    # Kotlin gates only
#   make ci ARGS=--audit     # deterministic audit tests + Health Bench ratchet
#
# This mirrors CI's *fast* gates only — no -race, coverage threshold, or
# integration-tagged tests; run those in CI or via `make go-test` variants.
ci:
	@scripts/dev/ci-check.sh $(ARGS)

# Fast inner-loop gate: path-gates the lanes (skips the Go or Kotlin side when
# its tree is untouched vs origin/main, mirroring CI's own path-gating) and uses
# the Go test cache. Much faster on single-side edits. NOT authoritative — run
# the full `make ci` before the actual push. Override the diff base with
# CI_CHECK_BASE=<ref>.
ci/fast:
	@scripts/dev/ci-check.sh --fast

# --- Native client verification (live / headless) ---
#
# Discoverability for the native-client verification harness that previously
# lived only in raw gradle + scripts/dev/ (and needed a manual ANDROID_HOME).
# These surface it through make; ANDROID_HOME is exported above, so no env setup.
# The gateway side already has good ergonomics via scripts/dev/live-test.sh.

# Render Deneb Compose previews to PNG headlessly (Skia, no Xvfb, no APK) — a
# fast UI eyeball of stateless composables with mock data.
# Output: /tmp/deneb-render/*.png
preview:
	cd $(KOTLIN_APP_DIR) && ./gradlew :composeApp:renderPreviews --console=plain
	@echo "Previews rendered to /tmp/deneb-render/*.png"

# Pixel regression gate over those previews. `preview` draws them; this compares
# them to the committed goldens, because drawing 117 PNGs nobody diffs cannot
# catch a two-pixel shift. Failures write golden|fresh|amplified strips to
# /tmp/deneb-render-diff so the change can be judged, not guessed at.
render-golden-check:
	python3 scripts/dev/render-golden.py check

# Accept the current render as the new baseline. The resulting PNG diff is the
# review artifact: an intended visual change shows up in the PR as before/after.
render-golden-update:
	python3 scripts/dev/render-golden.py update

# Boot the live desktop app and walk the key screens (OCR anchors + screenshots),
# flagging render-time crashes that compile + unit tests miss (e.g. #1959's
# LazyColumn duplicate-key crash). READ-ONLY against the gateway. Needs the live
# harness (Xvfb + a reachable gateway) — a manual pre-release gate, not CI.
# For interactive driving (start/shot/tap/type/stop) use scripts/dev/native-app.sh.
native-smoke:
	scripts/dev/native-app-smoke.sh

# --- Info ---

info:
	@echo "Deneb Build (Pure Go)"
	@echo ""
	@echo "  make go         - Build Go gateway"
	@echo "  make go-dev     - Run Go gateway in dev mode (auto-restart on SIGUSR1)"
	@echo "  make go-binary  - Build Go gateway binary to dist/"
	@echo "  make gateway-prod - Production gateway build"
	@echo "  make test       - Run Go tests"
	@echo "  make go-lint    - Run golangci-lint on Go gateway"
	@echo "  make go-fmt     - Check Go formatting"
	@echo "  make ci         - PRE-PUSH GATE: every CI check (Go + Kotlin + audit), pass/fail summary"
	@echo "                    (ARGS=--go / ARGS=--kotlin / ARGS=--audit to run one lane)"
	@echo "  make ci/fast    - Inner-loop gate: only the changed side (Go/Kotlin), cached tests"
	@echo "  make check      - Go checks + deterministic audit ratchets"
	@echo "  make check/fast - Fast Go checks: fmt + vet + lint, no tests"
	@echo "  make kotlin-check - Native client gate (spotless + detekt)"
	@echo "  make generate         - Run all code generation pipelines"
	@echo "  make generate-check   - Verify all generated files"
	@echo "  make clean      - Clean Go build artifacts"
	@echo "  make go-bench   - Run Go gateway benchmarks"
	@echo ""
	@echo "  Verification & live testing:"
	@echo "  make preview      - Render Compose previews to PNG (/tmp/deneb-render, headless)"
	@echo "  make render-golden-check  - Diff those previews against committed goldens"
	@echo "  make render-golden-update - Accept the current render as the new goldens"
	@echo "  make native-smoke - Live-app OCR smoke walk (Xvfb + gateway; pre-release gate)"
	@echo "  scripts/dev/native-app.sh - Drive the live desktop app (start/shot/tap/type/stop)"
	@echo "  scripts/dev/live-test.sh  - Gateway live test (restart/smoke/chat/logs-errors)"
	@echo "  scripts/check-dev-env.sh  - Check dev prerequisites"
	@echo ""
	@echo "  GO_PAR=$(GO_PAR)  - parallel build/test actions (auto from free RAM; override: make go GO_PAR=4)"
