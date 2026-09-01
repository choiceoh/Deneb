#!/usr/bin/env python3
"""design-lint.py — mechanical checks for the ADR 0007 design principles.

docs/adr/0007-design-north-star.md says a principle has to be *judgeable*, because
the ones that were not got ignored: the settings tree used the heaviest type token
as a section header in eleven places and every one of them passed review. Nothing
was wrong with the reviewers — there was no rule to point at.

This file is where a principle becomes a rule. It is deliberately small: only
principles that survive contact with a regex live here, and the rest stay judged by
eye against the preview goldens.

    scripts/dev/design-lint.py          # the gate (also `make design-lint`)

Entry points: main, rule_scaffolding_outweighs_content, rule_haptic_vocabulary,
rule_no_duplicate_tap, rule_pressable_owns_tap.
Verify: scripts/dev/design-lint.py
Boundary: this owns *mechanical* design rules for client-android only. Component
choice and verification procedure live in docs/agent-rules/native-design-system.md;
the principles themselves live in the ADR and win when they disagree.
"""

from __future__ import annotations

import re
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
SETTINGS_TREE = REPO / "client-android/app/composeApp/src/commonMain/kotlin/ai/deneb/deneb"

# Principle 1 forbids scaffolding that outweighs content. Across a whole screen that
# is a judgement call, but inside the settings tree it collapses to one fact: the
# content IS the controls, so an 18sp/600 heading can only ever be a label shouting
# over the thing it labels.
HEAVY_TOKEN = "DenebType.cardTitle"
SETTINGS_FILES = ("Config", "DenebConfigScreen")

CLIENT_SRC = REPO / "client-android/app/composeApp/src"

# native-design-system.md: "햅틱은 시맨틱 어휘로만 … 탭=tap, 커밋=confirm, 파괴=reject,
# 토글=toggle(on), PTR 트리거=refresh". The vocabulary was complete and named in the
# rule; what was missing was anything that noticed a call site skipping it. The
# 2026-09-01 sweep found ten — four silent switches, two switches buzzing tap()
# where the toggle type exists, two stepped sliders with no notch tick, and
# double-tap zoom. The browser's pull-to-refresh had been silent the same way.
#
# Only interactions whose haptic the rule ASSIGNS are checked, and the ones it
# assigns silence to are deliberately absent: a continuous Slider must not tick
# (segmentTick is for notches), and back/cancel/dismiss stay quiet by convention.
#
# Every wanted token carries its receiver dot on purpose. The first draft of this
# rule matched a bare "toggle(" and silently passed ConfigWormholeTab, whose
# handler calls a LOCAL function named toggle() — a lint that reports fewer
# violations than a hand count is worse than no lint, because the number looks
# authoritative.
HAPTIC_RULES = (
    ("Switch(", "onCheckedChange", (".toggle(", ".toggleOn(", ".toggleOff("),
     "스위치는 toggle(on) — 상태가 뒤집히는 것이지 눌린 게 아니다"),
    ("Checkbox(", "onCheckedChange", (".toggle(", ".toggleOn(", ".toggleOff("),
     "체크박스도 상태 전환 — toggle(on) (부모 행이 처리하면 onCheckedChange = null)"),
    ("Slider(", "onValueChange", (".segmentTick(", ".segmentFrequentTick("),
     "노치 슬라이더는 segmentTick() — steps 없는 연속 슬라이더는 넣지 말 것"),
    ("detectTapGestures(", "onDoubleTap", (".toggle(", ".toggleOn(", ".toggleOff(", ".tap("),
     "더블탭 줌은 상태 전환 — toggle(on)"),
)


def rule_scaffolding_outweighs_content() -> list[str]:
    """Principle 1 — the heaviest type token must not label a settings screen."""
    hits: list[str] = []
    for path in sorted(SETTINGS_TREE.glob("*.kt")):
        if not path.name.startswith(SETTINGS_FILES):
            continue
        for n, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
            if HEAVY_TOKEN in line:
                hits.append(f"{path.relative_to(REPO)}:{n}")
    return hits


def _balanced(text: str, start: int, open_ch: str, close_ch: str) -> str:
    """The substring from the first [open_ch] at/after [start] to its match."""
    i = text.find(open_ch, start)
    if i < 0:
        return ""
    depth = 0
    for j in range(i, len(text)):
        if text[j] == open_ch:
            depth += 1
        elif text[j] == close_ch:
            depth -= 1
            if depth == 0:
                return text[i : j + 1]
    return text[i:]


def rule_haptic_vocabulary() -> list[str]:
    """The semantic haptic the design rule assigns must actually be called."""
    hits: list[str] = []
    for path in sorted(CLIENT_SRC.rglob("*.kt")):
        rel = str(path.relative_to(REPO))
        # Previews never receive touches, and a test asserts logic, not feel.
        if "Test" in path.name or "RenderPreview" in path.name:
            continue
        text = path.read_text(encoding="utf-8")
        for anchor, param, wanted, hint in HAPTIC_RULES:
            at = 0
            while (at := text.find(anchor, at)) >= 0:
                call = _balanced(text, at + len(anchor) - 1, "(", ")")
                at += len(anchor)
                if param not in call:
                    continue
                # A null handler means the parent row owns the click — and its haptic.
                if f"{param} = null" in call:
                    continue
                # A Slider with no steps is continuous; a tick per pixel is a buzz.
                if anchor == "Slider(" and "steps" not in call:
                    continue
                body = _balanced(call, call.find(param), "{", "}")
                if any(w in body for w in wanted):
                    continue
                line = text.count("\n", 0, at) + 1
                hits.append(f"{rel}:{line}  — {hint}")
    return hits


def rule_no_duplicate_tap() -> list[str]:
    """denebPressable fires tap() itself — a call site doing it too buzzes twice.

    The mirror of rule_haptic_vocabulary: that one catches a missing haptic, this
    one catches the same haptic fired from both the primitive and the site. Same
    convention the rule already states for combinedClickable's long press.
    """
    hits: list[str] = []
    for path in sorted(CLIENT_SRC.rglob("*.kt")):
        if "Test" in path.name or "RenderPreview" in path.name:
            continue
        text = path.read_text(encoding="utf-8")
        at = 0
        while (at := text.find("denebPressable(", at)) >= 0:
            call = _balanced(text, at + len("denebPressable(") - 1, "(", ")")
            at += len("denebPressable(")
            if "haptic = false" in call:
                continue
            # ANY haptic inside the click, not just tap(): the primitive already
            # taps, so a site firing toggle()/confirm() there buzzes twice too —
            # those set haptic = false and own the richer type themselves.
            if not re.search(r"[Hh]aptics?\.\w+\(", call):
                continue
            line = text.count("\n", 0, at) + 1
            hits.append(f"{path.relative_to(REPO)}:{line}")
    return hits


def rule_pressable_owns_tap() -> list[str]:
    """denebPressable must fire the tap itself — ~26 surfaces depend on it.

    The other two rules read call sites. This one reads the primitive, because
    that is where a single deletion turns every pressable surface silent at once
    and no call-site rule would notice: they are all correct precisely BECAUSE
    they delegate. Verified by removing the call and watching the other rules
    stay green.
    """
    path = CLIENT_SRC / "commonMain/kotlin/ai/deneb/ui/DenebMotion.kt"
    if not path.exists():
        return [f"{path.relative_to(REPO)} 없음 — denebPressable 이 옮겨갔다면 이 규칙도 옮겨라"]
    text = path.read_text(encoding="utf-8")
    body = _balanced(text, text.find("fun Modifier.denebPressable"), "{", "}")
    if re.search(r"[Hh]aptics?\.tap\(\)", body):
        return []
    return [f"{path.relative_to(REPO)}  — denebPressable 이 tap() 을 부르지 않는다"]


def main() -> int:
    primitive = rule_pressable_owns_tap()
    if primitive:
        print("design-lint: 누름 프리미티브가 촉각을 잃었다\n")
        for h in primitive:
            print(f"  {h}")
        print(
            "\ndenebPressable 은 ~26개 표면의 탭 햅틱을 대신 소유한다. 여기서 지우면"
            "\n호출부는 전부 '올바른' 채로 앱 전체가 조용해지고, 호출부 규칙은 아무것도"
            "\n눈치채지 못한다."
        )
        return 1

    dupes = rule_no_duplicate_tap()
    if dupes:
        print(f"design-lint: 햅틱 중복 {len(dupes)}건 — denebPressable 이 이미 울린다\n")
        for d in dupes:
            print(f"  {d}")
        print(
            "\ndenebPressable(onClick=…) 은 스스로 tap() 을 발사한다. 호출부의 수동 tap() 을"
            "\n지우거나, 클릭이 탭보다 풍부한 의미면(토글 등) haptic = false 로 끄고 그쪽을 부르라."
        )
        return 1

    haptic = rule_haptic_vocabulary()
    if haptic:
        print(f"design-lint: 햅틱 어휘 누락 {len(haptic)}건 — 룰이 지정한 촉각이 호출되지 않는다\n")
        for h in haptic:
            print(f"  {h}")
        print(
            "\n어휘는 ui/components/Haptics.kt에 이미 있다. 빠진 건 호출부다."
            "\n의도적 무음(뒤로·취소·dismiss·연속 슬라이더)은 이 규칙이 검사하지 않는다."
            "\n\n근거: docs/agent-rules/native-design-system.md — 햅틱은 시맨틱 어휘로만"
        )
        return 1

    hits = rule_scaffolding_outweighs_content()
    if not hits:
        print("design-lint: ADR 0007 원리 위반 없음 · 햅틱 어휘 누락 없음")
        return 0

    print(f"design-lint: 원리 1 위반 {len(hits)}건 — 비계가 콘텐츠보다 무겁다\n")
    for h in hits:
        print(f"  {h}")
    print(
        f"\n{HEAVY_TOKEN}(18sp/600)은 화면에서 가장 무거운 활자다. 설정 화면의 콘텐츠는"
        "\n컨트롤이므로, 그걸 붙인 제목은 자기가 이름 붙인 대상보다 커진다."
        "\n\n  섹션 헤더 → DenebSectionLabel(\"…\")   (12sp 트랙트, 컨테이너 바깥)"
        "\n  행 제목   → DenebType.rowTitle / rowTitleStrong"
        "\n\n근거: docs/adr/0007-design-north-star.md 원리 1"
    )
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
