package protocol_test

// This test verifies that the generated protobuf types (in gen/) have fields
// consistent with the hand-written JSON wire types in this package.
// If a field is added to either side without the other, these tests catch it.

import (
	"reflect"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol/gen"
)

// exportedFieldNames returns the set of exported field names for a struct type.
func exportedFieldNames(v any) map[string]bool {
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

// fieldMapping defines how hand-written field names map to generated field names.
// Hand-written types may use different Go names (e.g. ID vs Id, OK vs Ok) or have
// fields not in the proto (e.g. Type discriminator).
type fieldMapping struct {
	handWritten string
	generated   string
}

// assertBidirectional verifies fields exist in both directions, using explicit
// mappings for name differences and skip lists for intentionally unmatched fields.
func assertBidirectional(
	t *testing.T,
	typeName string,
	hwFields map[string]bool,
	genFields map[string]bool,
	mappings []fieldMapping,
	hwSkip []string, // hand-written fields intentionally absent from generated
	genSkip []string, // generated fields intentionally absent from hand-written
) {
	t.Helper()

	hwSkipSet := toSet(hwSkip)
	genSkipSet := toSet(genSkip)

	// Build a name translation map: hand-written name → generated name.
	hwToGen := make(map[string]string)
	genToHw := make(map[string]string)
	for _, m := range mappings {
		hwToGen[m.handWritten] = m.generated
		genToHw[m.generated] = m.handWritten
	}

	// Check hand-written → generated.
	for name := range hwFields {
		if hwSkipSet[name] {
			continue
		}
		genName := name
		if mapped, ok := hwToGen[name]; ok {
			genName = mapped
		}
		if !genFields[genName] {
			t.Errorf("%s: hand-written field %q (expecting generated %q) not found in generated type", typeName, name, genName)
		}
	}

	// Check generated → hand-written.
	for name := range genFields {
		if genSkipSet[name] {
			continue
		}
		hwName := name
		if mapped, ok := genToHw[name]; ok {
			hwName = mapped
		}
		if !hwFields[hwName] {
			t.Errorf("%s: generated field %q (expecting hand-written %q) not found in hand-written type", typeName, name, hwName)
		}
	}
}

func toSet(items []string) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, item := range items {
		s[item] = true
	}
	return s
}

func TestErrorShapeConsistency(t *testing.T) {
	assertBidirectional(t, "ErrorShape",
		exportedFieldNames(protocol.ErrorShape{}),
		exportedFieldNames(gen.ErrorShape{}),
		[]fieldMapping{
			// Hand-written uses pointer prefix naming for optional fields.
		},
		[]string{"Details", "RetryAfterMs"}, // hand-written extras not yet in proto
		nil,                                 // no generated extras
	)
}

func TestRequestFrameConsistency(t *testing.T) {
	assertBidirectional(t, "RequestFrame",
		exportedFieldNames(protocol.RequestFrame{}),
		exportedFieldNames(gen.RequestFrame{}),
		[]fieldMapping{
			{handWritten: "ID", generated: "Id"},
		},
		[]string{"Type"},   // hand-written has discriminator field
		[]string{"Params"}, // generated has Params as google.protobuf.Struct
	)
}

func TestResponseFrameConsistency(t *testing.T) {
	assertBidirectional(t, "ResponseFrame",
		exportedFieldNames(protocol.ResponseFrame{}),
		exportedFieldNames(gen.ResponseFrame{}),
		[]fieldMapping{
			{handWritten: "ID", generated: "Id"},
			{handWritten: "OK", generated: "Ok"},
		},
		[]string{"Type"},    // hand-written has discriminator field
		[]string{"Payload"}, // generated has Payload as google.protobuf.Value
	)
}

func TestEventFrameConsistency(t *testing.T) {
	assertBidirectional(t, "EventFrame",
		exportedFieldNames(protocol.EventFrame{}),
		exportedFieldNames(gen.EventFrame{}),
		nil,
		[]string{"Type"},    // hand-written has discriminator field
		[]string{"Payload"}, // generated has Payload as google.protobuf.Value
	)
}

func TestStateVersionConsistency(t *testing.T) {
	assertBidirectional(t, "StateVersion",
		exportedFieldNames(protocol.StateVersion{}),
		exportedFieldNames(gen.StateVersion{}),
		nil, nil, nil,
	)
}

// TestErrorCodeConsistency verifies that the generated error code string
// constants in errors_gen.go match every non-UNSPECIFIED value in the
// protoc-gen-go ErrorCode enum. Both are generated from proto/gateway.proto
// but via different generators, so this cross-validates correctness.
func TestErrorCodeConsistency(t *testing.T) {
	// Hand-written string constants from errors.go.
	handWritten := []string{
		protocol.ErrNotLinked,
		protocol.ErrNotPaired,
		protocol.ErrAgentTimeout,
		protocol.ErrInvalidRequest,
		protocol.ErrUnavailable,
		protocol.ErrMissingParam,
		protocol.ErrNotFound,
		protocol.ErrUnauthorized,
		protocol.ErrValidationFailed,
		protocol.ErrConflict,
		protocol.ErrForbidden,
		protocol.ErrNodeDisconnected,
		protocol.ErrDependencyFailed,
		protocol.ErrFeatureDisabled,
	}

	// Build a set from the generated enum (skip UNSPECIFIED = 0).
	genCodes := make(map[string]bool)
	for val, name := range gen.ErrorCode_name {
		if val == 0 { // UNSPECIFIED
			continue
		}
		// Generated names are like "ERROR_CODE_NOT_LINKED"; strip the prefix
		// to get the wire-format code "NOT_LINKED".
		const prefix = "ERROR_CODE_"
		wire := name
		if len(name) > len(prefix) {
			wire = name[len(prefix):]
		}
		genCodes[wire] = true
	}

	hwSet := make(map[string]bool, len(handWritten))
	for _, code := range handWritten {
		hwSet[code] = true
	}

	// Check every hand-written code exists in proto.
	for _, code := range handWritten {
		if !genCodes[code] {
			t.Errorf("hand-written error code %q not found in generated ErrorCode enum", code)
		}
	}

	// Check every generated code exists in hand-written constants.
	for code := range genCodes {
		if !hwSet[code] {
			t.Errorf("generated ErrorCode %q not found in hand-written constants (errors.go)", code)
		}
	}

	// Verify counts match.
	if len(handWritten) != len(genCodes) {
		t.Errorf("error code count mismatch: hand-written=%d, generated=%d", len(handWritten), len(genCodes))
	}
}
