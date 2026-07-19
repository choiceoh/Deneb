#!/usr/bin/env bash
# state-register (Kotlin) launcher — Go/TS state-register의 Kotlin 짝.
#
# Kotlin은 go/types·tsc 같은 경량 무의존 프론트엔드가 없다: 타입 해석에
# 컴파일러 프론트엔드(BindingContext)가 필요하다. 이 도구는 gradle 캐시에
# 이미 있는 kotlin-compiler-embeddable(K1 프론트엔드)로 분석기를 컴파일·실행한다
# — **프로젝트 빌드 그래프에는 아무것도 추가하지 않는다** (캐시 jar를 클래스패스로만 쓴다).
# advisory 도구(CI 게이트 아님); client-android가 이 호스트에서 검증되는 것과 동일 전제.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$HERE/../../.." && pwd)"
GC="$HOME/.gradle/caches"

pick() { find "$GC" -name "$1" 2>/dev/null | grep -v sources | sort -V | tail -1; }

# K1 프론트엔드가 필요 — 2.0.x 임베더블을 선호(2.2+는 K1 제거).
EMB="$(find "$GC" -name 'kotlin-compiler-embeddable-2.0.*.jar' 2>/dev/null | grep -v sources | sort -V | tail -1)"
STDLIB="$(find "$GC" -name 'kotlin-stdlib-2.0.*.jar' 2>/dev/null | grep -v sources | sort -V | tail -1)"
TROVE="$(pick 'trove4j*.jar')"
ANNO="$(find "$GC" -path '*org.jetbrains/annotations*' -name 'annotations-13*.jar' 2>/dev/null | head -1)"
# 런타임 환경(KotlinCoreApplicationEnvironment)이 요구하는 coroutines — 최신이면 됨.
CORO_RT="$(pick 'kotlinx-coroutines-core-jvm-*.jar')"
# 분석 클래스패스(프로젝트 심볼 해석)용 coroutines/immutable — 2.0.x가 읽을 수 있는
# 메타데이터(≤2.0)여야 한다. update{} 람다의 it: StateFlow<T> 해석에 필요.
CORO_AN="$(pick 'kotlinx-coroutines-core-jvm-1.9.*.jar')"
[ -z "$CORO_AN" ] && CORO_AN="$(pick 'kotlinx-coroutines-core-jvm-1.8.*.jar')"
IMM="$(pick 'kotlinx-collections-immutable-jvm-*.jar')"

for v in EMB STDLIB TROVE ANNO CORO_RT; do
  if [ -z "${!v}" ]; then
    echo "state-register-kt: 필요한 jar 없음($v) — client-android를 한 번 빌드해 gradle 캐시를 채우세요." >&2
    exit 1
  fi
done

RUNTIME_CP="$EMB:$STDLIB:$CORO_RT:$TROVE:$ANNO"
COMPILE_CP="$EMB:$STDLIB:$TROVE:$ANNO"
CP_ANALYSIS="$STDLIB:$CORO_AN:$IMM"

# 분석기 컴파일(소스보다 최신 jar가 없으면 재사용).
OUT="${TMPDIR:-/tmp}/deneb-state-register-kt"
mkdir -p "$OUT"
JAR="$OUT/analyzer.jar"
if [ ! -f "$JAR" ] || [ "$HERE/StateRegister.kt" -nt "$JAR" ]; then
  java -cp "$RUNTIME_CP" org.jetbrains.kotlin.cli.jvm.K2JVMCompiler \
    -language-version 1.9 -no-stdlib -classpath "$COMPILE_CP" \
    "$HERE/StateRegister.kt" -d "$JAR" 2>/dev/null
fi

TYPE="${TYPE:-ai.deneb.ui.chat.ChatUiState}"
OUTDOC="${OUTDOC:-$REPO/docs/research/state-register-chat-ui.md}"
SRC="$REPO/client-android/app/composeApp/src/commonMain/kotlin:$REPO/client-android/app/composeApp/src/androidMain/kotlin"

CP_ANALYSIS="$CP_ANALYSIS" java -cp "$RUNTIME_CP:$JAR" StateRegisterKt \
  --type "$TYPE" --src "$SRC" --repo "$REPO" --out "$OUTDOC"
