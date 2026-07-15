package chrono

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

type mockRegistrar struct {
	tools []toolport.ToolDef
}

func (m *mockRegistrar) RegisterTool(def toolport.ToolDef) {
	m.tools = append(m.tools, def)
}

func TestRegisterScheduleAndRoutineToolsCreatesExpectedContracts(t *testing.T) {
	calendarReg := &mockRegistrar{}
	RegisterCalendarTool(calendarReg, &tooldeps.CalendarDeps{
		Client: func() (tooldeps.CalendarReader, error) { return nil, nil },
	})
	var calendar *toolport.ToolDef
	for i := range calendarReg.tools {
		if calendarReg.tools[i].Name == "calendar" {
			calendar = &calendarReg.tools[i]
			break
		}
	}
	if calendar == nil || calendar.Fn == nil || calendar.InputSchema["type"] != "object" {
		t.Fatalf("calendar contract invalid: %+v", calendar)
	}
	if calendar.Deferred {
		t.Fatalf("calendar should be eager")
	}

	routineReg := &mockRegistrar{}
	RegisterRoutineTools(routineReg, &tooldeps.ChronoDeps{}, t.TempDir(), t.TempDir(), nil)
	for _, name := range []string{"files", "morning_letter", "evening_letter"} {
		var def *toolport.ToolDef
		for i := range routineReg.tools {
			if routineReg.tools[i].Name == name {
				def = &routineReg.tools[i]
				break
			}
		}
		if def == nil || def.Fn == nil || def.InputSchema["type"] != "object" {
			t.Fatalf("%s contract invalid: %+v", name, def)
		}
		if !def.Deferred {
			t.Fatalf("%s should be deferred", name)
		}
	}
}
