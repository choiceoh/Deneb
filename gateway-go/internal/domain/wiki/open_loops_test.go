package wiki

import "testing"

func TestParseOpenLoops_FencesDriftAndCap(t *testing.T) {
	// Fenced output with junk casing — the common LLM shape.
	loops, err := parseOpenLoops("```json\n[{\"what\":\"견적서 발송\",\"who\":\"우리\",\"due\":\"2026-06-20\",\"context\":\"미팅 약속\"},{\"what\":\"  \"}]\n```")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(loops) != 1 || loops[0].What != "견적서 발송" {
		t.Fatalf("unexpected loops: %+v", loops)
	}

	// Empty array and bare empty string are both "nothing found".
	if loops, err := parseOpenLoops("[]"); err != nil || len(loops) != 0 {
		t.Errorf("empty array: %v %+v", err, loops)
	}
	if loops, err := parseOpenLoops("```\n```"); err != nil || loops != nil {
		t.Errorf("empty fence: %v %+v", err, loops)
	}

	// Garbage must error (caller logs it as a phase error, cycle continues).
	if _, err := parseOpenLoops("오픈루프 없음"); err == nil {
		t.Error("prose response must fail parsing")
	}

	// Cap at openLoopMaxPerCycle.
	big := "["
	for i := 0; i < 12; i++ {
		if i > 0 {
			big += ","
		}
		big += `{"what":"task"}`
	}
	big += "]"
	loops, err = parseOpenLoops(big)
	if err != nil || len(loops) != openLoopMaxPerCycle {
		t.Errorf("cap failed: %v len=%d", err, len(loops))
	}
}
