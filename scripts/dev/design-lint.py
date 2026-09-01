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
rule_no_duplicate_tap, rule_dialog_decision_haptic, rule_primitive_owns_haptic.
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

# A dialog WITH a dismiss button is a decision, and its confirm button is where the
# decision lands: confirm() for a commit (저장·보내기·다운로드·선택기 확인), reject()
# when it destroys or discards (삭제·나가기·작업 취소). The 2026-09-01 sweep found 20
# of 27 silent — every 삭제 dialog buzzed on the button that OPENED it and said
# nothing on the button that deleted, which is the one decision the whole dialog
# exists for. A one-button dialog is an acknowledgement (닫기/확인) and stays silent
# under the dismiss convention, so it is not checked; neither is an empty
# confirmButton = {} (a picker list that commits from its rows).
DIALOG_ANCHORS = ("AlertDialog(", "DatePickerDialog(")
DIALOG_WANTED = re.compile(r"[Hh]aptics?\.(confirm|reject)\(")

# Primitives that own a haptic on behalf of every caller. No call-site rule can see
# these: delete the call inside the primitive and every surface stays 'correct'
# precisely BECAUSE it delegates — verified for denebPressable by removing the call
# and watching the call-site rules stay green. (path under CLIENT_SRC, function
# anchor, wanted call, hint)
PRIMITIVE_RULES = (
    ("commonMain/kotlin/ai/deneb/ui/DenebMotion.kt", "fun Modifier.denebPressable",
     re.compile(r"[Hh]aptics?\.tap\(\)"),
     "denebPressable 이 tap() 을 부르지 않는다 — ~26개 표면의 탭 햅틱을 대신 소유한다"),
    ("commonMain/kotlin/ai/deneb/ui/DenebDesign.kt", "fun DenebSiblingSwipeHost",
     re.compile(r"[Hh]aptics?\.arm\(\)"),
     "DenebSiblingSwipeHost 가 arm() 을 부르지 않는다 — 피드|결재|로그 스와이프의 문턱 틱을 소유한다"),
    ("commonMain/kotlin/ai/deneb/ui/DenebDesign.kt", "fun DenebTitlePivot",
     re.compile(r"[Hh]aptics?\.tap\(\)"),
     "DenebTitlePivot 이 tap() 을 부르지 않는다 — 피벗 라벨의 탭을 소유하고 화면은 맨 람다만 넘긴다"),
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


def rule_dialog_decision_haptic() -> list[str]:
    """A decision dialog's confirm button must carry confirm() or reject()."""
    hits: list[str] = []
    for path in sorted(CLIENT_SRC.rglob("*.kt")):
        if "Test" in path.name or "RenderPreview" in path.name:
            continue
        text = path.read_text(encoding="utf-8")
        for anchor in DIALOG_ANCHORS:
            at = 0
            while (at := text.find(anchor, at)) >= 0:
                call = _balanced(text, at + len(anchor) - 1, "(", ")")
                at += len(anchor)
                if "confirmButton" not in call or "dismissButton" not in call:
                    continue
                body = _balanced(call, call.find("confirmButton"), "{", "}")
                if body.strip("{} \n\t") == "" or DIALOG_WANTED.search(body):
                    continue
                line = text.count("\n", 0, at) + 1
                hits.append(f"{path.relative_to(REPO)}:{line}")
    return hits


def rule_primitive_owns_haptic() -> list[str]:
    """Each primitive in PRIMITIVE_RULES must fire its haptic itself.

    The other rules read call sites. This one reads the primitives, because
    that is where a single deletion turns every delegating surface silent at
    once and no call-site rule would notice.
    """
    hits: list[str] = []
    for rel, anchor, wanted, hint in PRIMITIVE_RULES:
        path = CLIENT_SRC / rel
        if not path.exists():
            hits.append(f"{path.relative_to(REPO)} 없음 — {anchor} 가 옮겨갔다면 이 규칙도 옮겨라")
            continue
        text = path.read_text(encoding="utf-8")
        start = text.find(anchor)
        if start < 0:
            hits.append(f"{path.relative_to(REPO)}  — {anchor} 를 찾지 못했다 (이름이 바뀌었으면 규칙도 바꿔라)")
            continue
        if not wanted.search(_balanced(text, start, "{", "}")):
            hits.append(f"{path.relative_to(REPO)}  — {hint}")
    return hits


def main() -> int:
    primitive = rule_primitive_owns_haptic()
    if primitive:
        print(f"design-lint: 프리미티브가 촉각을 잃었다 {len(primitive)}건\n")
        for h in primitive:
            print(f"  {h}")
        print(
            "\n이 프리미티브들은 호출부 대신 햅틱을 소유한다. 여기서 지우면 호출부는 전부"
            "\n'올바른' 채로 그 표면이 통째로 조용해지고, 호출부 규칙은 아무것도 눈치채지"
            "\n못한다."
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

    dialogs = rule_dialog_decision_haptic()
    if dialogs:
        print(f"design-lint: 결정 다이얼로그 무음 {len(dialogs)}건 — 확인 버튼에 confirm()/reject() 가 없다\n")
        for d in dialogs:
            print(f"  {d}")
        print(
            "\n취소 버튼이 있는 다이얼로그는 결정이고, 결정은 확인 버튼에서 난다 — 연 버튼이 아니라."
            "\n커밋(저장·보내기·선택기 확인)=confirm(), 파괴·폐기(삭제·나가기·작업 취소)=reject()."
            "\n한 버튼짜리 알림(닫기/확인)은 dismiss 관례대로 무음이며 이 규칙이 검사하지 않는다."
        )
        return 1

    hits = rule_scaffolding_outweighs_content()
    if not hits:
        print("design-lint: ADR 0007 원리 위반 없음 · 햅틱 어휘 누락 없음 · 결정 다이얼로그 무음 없음")
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
