# {절차명} SOP — procedural SKILL.md 골격

> 그릴로 채운 뒤 `skills/operations/<slug>/SKILL.md`로 쓴다. {중괄호}는 인터뷰로 채울 자리.
> 빈 단계·필드는 추측으로 메우지 말고 `미확인`으로 남긴다(−1pp 방지).

```markdown
---
name: {slug}
version: "1.0.0"
category: operations
description: "{절차명} SOP — {한 줄 요지}. Use when: {이 절차를 따라야 하는 트리거}. NOT for: {아닌 경우}."
metadata:
  {
    "deneb":
      {
        "emoji": "📋",
        "tags": ["SOP", "{업무}", "{단계}"],
        "related_skills": [],
      },
  }
user-invocable: true
---

# {절차명} (SOP)

> 출처: 사용자 인터뷰 · 작성일 {YYYY-MM} · 확신도 {상/중/하}
> 기관 특유 절차 — 추측 금지, 모르는 칸은 `미확인`.

## When to Use
- {이 절차를 따라야 하는 상황·트리거}

## 단계 체크리스트
1. **{단계1}** — {무엇을 확인/수행}. 확인 필드: {값/기준}. 게이트(통과 조건): {…}.
2. **{단계2}** — …

## 필수 서류 / 산출물
- [ ] {서류·산출물}

## 승인 / 결재
- {누가 · 언제 · 무엇을 승인}

## 실패 모드 / 흔한 누락
- {자주 틀어지는 지점, 빠뜨리기 쉬운 것}

## 미확인 (unknown)
- {아직 못 채운 단계·필드 — 다음 인터뷰로 보완}
```
