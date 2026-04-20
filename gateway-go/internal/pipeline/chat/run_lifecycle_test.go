package chat

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
)

func TestShouldForceExternalDeliveryFailureNotice(t *testing.T) {
	delivery := &DeliveryContext{Channel: "telegram", To: "telegram:123"}
	toolActivities := []agent.ToolActivity{
		{Name: "message", IsError: true},
	}

	if !shouldForceExternalDeliveryFailureNotice(delivery, toolActivities, "", true) {
		t.Fatal("expected forced notice for silent failed external delivery")
	}
	if !shouldForceExternalDeliveryFailureNotice(delivery, toolActivities, "", false) {
		t.Fatal("expected forced notice for empty failed external delivery")
	}
	if shouldForceExternalDeliveryFailureNotice(delivery, toolActivities, "실패했습니다. 다시 시도해 주세요.", false) {
		t.Fatal("did not expect forced notice when assistant already produced a visible explanation")
	}
}

func TestShouldForceExternalDeliveryFailureNotice_IgnoresUnrelatedCases(t *testing.T) {
	delivery := &DeliveryContext{Channel: "telegram", To: "telegram:123"}

	if shouldForceExternalDeliveryFailureNotice(nil, []agent.ToolActivity{{Name: "message", IsError: true}}, "", true) {
		t.Fatal("did not expect forced notice without a delivery context")
	}
	if shouldForceExternalDeliveryFailureNotice(delivery, []agent.ToolActivity{{Name: "exec", IsError: true}}, "", true) {
		t.Fatal("did not expect forced notice for non-delivery tool errors")
	}
	if shouldForceExternalDeliveryFailureNotice(delivery, []agent.ToolActivity{{Name: "message", IsError: false}}, "", true) {
		t.Fatal("did not expect forced notice when delivery tool succeeded")
	}
}
