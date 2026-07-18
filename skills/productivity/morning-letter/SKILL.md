---
name: morning-letter
version: "2.0.0"
category: productivity
description: "매일 아침 모닝레터 생성 및 발송. 날씨, 환율, 구리시세, 일정, 메일, 마감, 전자결재를 한 장의 아침 브리핑으로 만든다. Use when: 모닝레터, morning letter, 아침 브리핑, 오늘의 브리핑, daily briefing. NOT for: 일반 메일 분석, 회신 작성, 장문 회의록 정리."
metadata:
  {
    "deneb":
      {
        "emoji": "🌅",
        "tags": ["briefing", "daily", "morning", "summary"],
        "triggers": ["모닝레터", "아침 브리핑", "오늘의 브리핑", "morning letter"],
        "requires_tools": ["morning_letter"],
      },
  }
---

# Morning Letter

Produce one delivery-ready Korean briefing with no duplicate retrieval or model-authored card markup.

Scheduled `/morning` runs automatically use one bounded model pass to fill semantic content slots, then the server renders the fixed card. Manual invocations use the safe facts-only card below.

## Procedure

1. Call `morning_letter` once with no parameters.
2. Return the response's `delivery` field verbatim as the final answer.

The tool already collects weather, USD/KRW, copper, calendar, recent wiki project signals, mail, deadlines/open questions, and groupware signals; prioritizes urgent items; formats server-side market values; escapes external text; and validates one deneb-ui card. Do not call `calendar`, `mail_archive`, or `wiki` for the same facts. Load extra context only when the user explicitly asks for analysis beyond the standard letter.

## Boundaries

- Do not rewrite, summarize, translate, or wrap `delivery`.
- Do not call `message`; the current chat or cron delivery path sends the final text.
- If `delivery` is absent, give a short Korean plain-text fallback from healthy `sections` only. Never invent missing values or expose tool errors.

## Verification

- Final output has one short Korean head line and exactly one `deneb-ui` fence.
- The market card contains USD/KRW and LME copper only; no EUR or raw `{{...}}` tokens.
- No progress narration, confirmation suffix, `NO_REPLY`, or channel-status guess appears.
