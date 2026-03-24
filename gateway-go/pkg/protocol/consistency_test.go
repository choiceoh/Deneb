package protocol_test

// This test verifies that the generated protobuf types (in gen/) have fields
// consistent with the hand-written JSON wire types in this package.
// If this test fails, either the proto definitions or the hand-written types
// need to be updated to match.

import (
	"reflect"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol/gen"
)

// fieldNames returns the set of exported field names for a struct type.
func fieldNames(v any) map[string]bool {
	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	names := make(map[string]bool, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.IsExported() {
			names[f.Name] = true
		}
	}
	return names
}

// assertFieldsPresent checks that all expected fields exist in the generated type.
func assertFieldsPresent(t *testing.T, typeName string, got map[string]bool, expected []string) {
	t.Helper()
	for _, name := range expected {
		if !got[name] {
			t.Errorf("generated %s missing expected field %q (present in hand-written type)", typeName, name)
		}
	}
}

func TestErrorShapeFieldConsistency(t *testing.T) {
	fields := fieldNames(&gen.ErrorShape{})
	assertFieldsPresent(t, "ErrorShape", fields, []string{
		"Code", "Message", "Retryable", "Cause",
	})
}

func TestRequestFrameFieldConsistency(t *testing.T) {
	fields := fieldNames(&gen.RequestFrame{})
	assertFieldsPresent(t, "RequestFrame", fields, []string{
		"Id", "Method",
	})
}

func TestResponseFrameFieldConsistency(t *testing.T) {
	fields := fieldNames(&gen.ResponseFrame{})
	assertFieldsPresent(t, "ResponseFrame", fields, []string{
		"Id", "Ok", "Error",
	})
}

func TestEventFrameFieldConsistency(t *testing.T) {
	fields := fieldNames(&gen.EventFrame{})
	assertFieldsPresent(t, "EventFrame", fields, []string{
		"Event", "Seq", "StateVersion",
	})
}

func TestStateVersionFieldConsistency(t *testing.T) {
	fields := fieldNames(&gen.StateVersion{})
	assertFieldsPresent(t, "StateVersion", fields, []string{
		"Presence", "Health",
	})
}

func TestPresenceEntryFieldConsistency(t *testing.T) {
	fields := fieldNames(&gen.PresenceEntry{})
	assertFieldsPresent(t, "PresenceEntry", fields, []string{
		"Host", "Ip", "Version", "Platform", "Tags", "Ts", "Roles", "Scopes",
	})
}
