package schedule

// Field-isolated JSON contracts for every //deneb:wire DTO in this package.
// These tests protect exact tags, extreme values, ordered collections, null and
// missing-field patch semantics, and strict rejection of incompatible shapes.

import (
	"encoding/json"
	"math"
	"reflect"
	"strconv"
	"testing"
	"time"
)

var wireBoundaryTimeType = reflect.TypeOf(time.Time{})

func wireBoundaryField(t *testing.T, target any, goName string) reflect.Value {
	t.Helper()

	value := reflect.ValueOf(target)
	if value.Kind() != reflect.Pointer || value.IsNil() || value.Elem().Kind() != reflect.Struct {
		t.Fatalf("target must be a non-nil pointer to a struct, got %T", target)
	}
	field := value.Elem().FieldByName(goName)
	if !field.IsValid() {
		t.Fatalf("field %q is absent from %T", goName, target)
	}
	if !field.CanSet() {
		t.Fatalf("field %q on %T is not settable", goName, target)
	}
	return field
}

func setWireBoundaryValue(value reflect.Value, seed, depth int) {
	if !value.IsValid() || !value.CanSet() || depth > 8 {
		return
	}
	if value.Type() == wireBoundaryTimeType {
		value.Set(reflect.ValueOf(time.Date(2038, time.January, 19, 3, 14, seed%60, seed, time.UTC)))
		return
	}

	switch value.Kind() {
	case reflect.Pointer:
		value.Set(reflect.New(value.Type().Elem()))
		setWireBoundaryValue(value.Elem(), seed, depth+1)
	case reflect.Interface:
		value.Set(reflect.ValueOf(map[string]any{
			"seed": float64(seed),
			"text": "값-" + strconv.Itoa(seed),
		}))
	case reflect.String:
		value.SetString("  \x00 한글 café\n\t\"\\ /?#  " + strconv.Itoa(seed))
	case reflect.Bool:
		value.SetBool(seed%2 == 1)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		bits := value.Type().Bits()
		maximum := int64(^uint64(0) >> 1)
		if bits < 64 {
			maximum = int64(uint64(1)<<(bits-1)) - 1
		}
		value.SetInt(maximum - int64(seed))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		bits := value.Type().Bits()
		maximum := ^uint64(0)
		if bits < 64 {
			maximum = uint64(1)<<bits - 1
		}
		value.SetUint(maximum - uint64(seed))
	case reflect.Float32:
		value.SetFloat(float64(math.MaxFloat32) / float64(seed+1))
	case reflect.Float64:
		value.SetFloat(math.MaxFloat64 / float64(seed+1))
	case reflect.Slice:
		items := reflect.MakeSlice(value.Type(), 3, 3)
		for i := 0; i < items.Len(); i++ {
			setWireBoundaryValue(items.Index(i), seed+i+1, depth+1)
		}
		value.Set(items)
	case reflect.Array:
		for i := 0; i < value.Len(); i++ {
			setWireBoundaryValue(value.Index(i), seed+i+1, depth+1)
		}
	case reflect.Map:
		entries := reflect.MakeMapWithSize(value.Type(), 2)
		for i := 0; i < 2; i++ {
			key := reflect.New(value.Type().Key()).Elem()
			item := reflect.New(value.Type().Elem()).Elem()
			setWireBoundaryValue(key, seed+i+1, depth+1)
			setWireBoundaryValue(item, seed+i+11, depth+1)
			entries.SetMapIndex(key, item)
		}
		value.Set(entries)
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			setWireBoundaryValue(value.Field(i), seed+i+1, depth+1)
		}
	default:
		// Wire structs never carry chan/func/complex/unsafe fields; leaving the
		// zero value is correct if one ever appears.
	}
}

func assertWireFieldBoundaryRoundTrip(t *testing.T, target any, goName, jsonName string) {
	t.Helper()

	field := wireBoundaryField(t, target, goName)
	setWireBoundaryValue(field, 1, 0)
	encoded, err := json.Marshal(target)
	if err != nil {
		t.Fatalf("marshal %T.%s: %v", target, goName, err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatalf("decode object for %T.%s: %v", target, goName, err)
	}
	if _, ok := object[jsonName]; !ok {
		t.Fatalf("%T.%s did not encode exact JSON key %q: %s", target, goName, jsonName, encoded)
	}

	decoded := reflect.New(reflect.TypeOf(target).Elem())
	if err := json.Unmarshal(encoded, decoded.Interface()); err != nil {
		t.Fatalf("round-trip %T.%s: %v", target, goName, err)
	}
	if !reflect.DeepEqual(reflect.ValueOf(target).Elem().Interface(), decoded.Elem().Interface()) {
		t.Fatalf("round-trip changed %T.%s: before=%#v after=%#v", target, goName, reflect.ValueOf(target).Elem().Interface(), decoded.Elem().Interface())
	}
}

func incompatibleWireShape(fieldType reflect.Type) json.RawMessage {
	for fieldType.Kind() == reflect.Pointer {
		fieldType = fieldType.Elem()
	}
	if fieldType == wireBoundaryTimeType {
		return json.RawMessage(`"not-a-time"`)
	}
	switch fieldType.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return json.RawMessage(`{"wrong":true}`)
	case reflect.Slice, reflect.Array, reflect.Map:
		return json.RawMessage(`7`)
	case reflect.Struct:
		return json.RawMessage(`"not-an-object"`)
	default:
		return json.RawMessage(`null`)
	}
}

func assertWireFieldRejectsWrongShape(t *testing.T, target any, goName, jsonName string) {
	t.Helper()

	field := wireBoundaryField(t, target, goName)
	encoded, err := json.Marshal(map[string]json.RawMessage{
		jsonName: incompatibleWireShape(field.Type()),
	})
	if err != nil {
		t.Fatalf("build malformed payload for %T.%s: %v", target, goName, err)
	}
	if err := json.Unmarshal(encoded, target); err == nil {
		t.Fatalf("%T.%s accepted incompatible payload %s", target, goName, encoded)
	}
}

func assertWireFieldMissingPreservesValue(t *testing.T, target any, goName string) {
	t.Helper()

	field := wireBoundaryField(t, target, goName)
	setWireBoundaryValue(field, 2, 0)
	before := reflect.New(field.Type()).Elem()
	before.Set(field)
	if err := json.Unmarshal([]byte(`{}`), target); err != nil {
		t.Fatalf("decode missing field for %T.%s: %v", target, goName, err)
	}
	if !reflect.DeepEqual(before.Interface(), field.Interface()) {
		t.Fatalf("missing %T.%s replaced an existing patch value: before=%#v after=%#v", target, goName, before.Interface(), field.Interface())
	}
}

func assertWireFieldNullSemantics(t *testing.T, target any, goName, jsonName string) {
	t.Helper()

	field := wireBoundaryField(t, target, goName)
	setWireBoundaryValue(field, 3, 0)
	before := reflect.New(field.Type()).Elem()
	before.Set(field)
	encoded, err := json.Marshal(map[string]any{jsonName: nil})
	if err != nil {
		t.Fatalf("build null payload for %T.%s: %v", target, goName, err)
	}
	if err := json.Unmarshal(encoded, target); err != nil {
		t.Fatalf("decode null for %T.%s: %v", target, goName, err)
	}

	switch field.Kind() {
	case reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if !field.IsNil() {
			t.Fatalf("null did not clear nullable %T.%s: %#v", target, goName, field.Interface())
		}
	default:
		if !reflect.DeepEqual(before.Interface(), field.Interface()) {
			t.Fatalf("null changed non-nullable %T.%s: before=%#v after=%#v", target, goName, before.Interface(), field.Interface())
		}
	}
}

func TestCalendarEventOutIDBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldBoundaryRoundTrip(t, &value, "ID", "id")
}

func TestCalendarEventOutIDRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldRejectsWrongShape(t, &value, "ID", "id")
}

func TestCalendarEventOutIDMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldMissingPreservesValue(t, &value, "ID")
}

func TestCalendarEventOutIDNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldNullSemantics(t, &value, "ID", "id")
}

func TestCalendarEventOutSummaryBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Summary", "summary")
}

func TestCalendarEventOutSummaryRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldRejectsWrongShape(t, &value, "Summary", "summary")
}

func TestCalendarEventOutSummaryMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldMissingPreservesValue(t, &value, "Summary")
}

func TestCalendarEventOutSummaryNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldNullSemantics(t, &value, "Summary", "summary")
}

func TestCalendarEventOutDescriptionBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Description", "description")
}

func TestCalendarEventOutDescriptionRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldRejectsWrongShape(t, &value, "Description", "description")
}

func TestCalendarEventOutDescriptionMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldMissingPreservesValue(t, &value, "Description")
}

func TestCalendarEventOutDescriptionNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldNullSemantics(t, &value, "Description", "description")
}

func TestCalendarEventOutLocationBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Location", "location")
}

func TestCalendarEventOutLocationRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldRejectsWrongShape(t, &value, "Location", "location")
}

func TestCalendarEventOutLocationMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldMissingPreservesValue(t, &value, "Location")
}

func TestCalendarEventOutLocationNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldNullSemantics(t, &value, "Location", "location")
}

func TestCalendarEventOutStartBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Start", "start")
}

func TestCalendarEventOutStartRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldRejectsWrongShape(t, &value, "Start", "start")
}

func TestCalendarEventOutStartMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldMissingPreservesValue(t, &value, "Start")
}

func TestCalendarEventOutStartNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldNullSemantics(t, &value, "Start", "start")
}

func TestCalendarEventOutEndBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldBoundaryRoundTrip(t, &value, "End", "end")
}

func TestCalendarEventOutEndRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldRejectsWrongShape(t, &value, "End", "end")
}

func TestCalendarEventOutEndMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldMissingPreservesValue(t, &value, "End")
}

func TestCalendarEventOutEndNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldNullSemantics(t, &value, "End", "end")
}

func TestCalendarEventOutAllDayBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldBoundaryRoundTrip(t, &value, "AllDay", "allDay")
}

func TestCalendarEventOutAllDayRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldRejectsWrongShape(t, &value, "AllDay", "allDay")
}

func TestCalendarEventOutAllDayMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldMissingPreservesValue(t, &value, "AllDay")
}

func TestCalendarEventOutAllDayNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldNullSemantics(t, &value, "AllDay", "allDay")
}

func TestCalendarEventOutStatusBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Status", "status")
}

func TestCalendarEventOutStatusRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldRejectsWrongShape(t, &value, "Status", "status")
}

func TestCalendarEventOutStatusMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldMissingPreservesValue(t, &value, "Status")
}

func TestCalendarEventOutStatusNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldNullSemantics(t, &value, "Status", "status")
}

func TestCalendarEventOutHTMLLinkBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldBoundaryRoundTrip(t, &value, "HTMLLink", "htmlLink")
}

func TestCalendarEventOutHTMLLinkRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldRejectsWrongShape(t, &value, "HTMLLink", "htmlLink")
}

func TestCalendarEventOutHTMLLinkMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldMissingPreservesValue(t, &value, "HTMLLink")
}

func TestCalendarEventOutHTMLLinkNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldNullSemantics(t, &value, "HTMLLink", "htmlLink")
}

func TestCalendarEventOutLocalBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Local", "local")
}

func TestCalendarEventOutLocalRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldRejectsWrongShape(t, &value, "Local", "local")
}

func TestCalendarEventOutLocalMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldMissingPreservesValue(t, &value, "Local")
}

func TestCalendarEventOutLocalNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldNullSemantics(t, &value, "Local", "local")
}

func TestCalendarEventOutCategoryBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Category", "category")
}

func TestCalendarEventOutCategoryRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldRejectsWrongShape(t, &value, "Category", "category")
}

func TestCalendarEventOutCategoryMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldMissingPreservesValue(t, &value, "Category")
}

func TestCalendarEventOutCategoryNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldNullSemantics(t, &value, "Category", "category")
}

func TestCalendarEventOutOrganizerBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Organizer", "organizer")
}

func TestCalendarEventOutOrganizerRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldRejectsWrongShape(t, &value, "Organizer", "organizer")
}

func TestCalendarEventOutOrganizerMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldMissingPreservesValue(t, &value, "Organizer")
}

func TestCalendarEventOutOrganizerNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldNullSemantics(t, &value, "Organizer", "organizer")
}

func TestCalendarEventOutAttendeesBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Attendees", "attendees")
}

func TestCalendarEventOutAttendeesRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldRejectsWrongShape(t, &value, "Attendees", "attendees")
}

func TestCalendarEventOutAttendeesMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldMissingPreservesValue(t, &value, "Attendees")
}

func TestCalendarEventOutAttendeesNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldNullSemantics(t, &value, "Attendees", "attendees")
}

func TestCalendarEventOutConferenceBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Conference", "conference")
}

func TestCalendarEventOutConferenceRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldRejectsWrongShape(t, &value, "Conference", "conference")
}

func TestCalendarEventOutConferenceMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldMissingPreservesValue(t, &value, "Conference")
}

func TestCalendarEventOutConferenceNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldNullSemantics(t, &value, "Conference", "conference")
}

func TestCalendarEventOutHasMeetBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldBoundaryRoundTrip(t, &value, "HasMeet", "hasMeet")
}

func TestCalendarEventOutHasMeetRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldRejectsWrongShape(t, &value, "HasMeet", "hasMeet")
}

func TestCalendarEventOutHasMeetMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldMissingPreservesValue(t, &value, "HasMeet")
}

func TestCalendarEventOutHasMeetNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldNullSemantics(t, &value, "HasMeet", "hasMeet")
}

func TestCalendarProposalOutIDBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldBoundaryRoundTrip(t, &value, "ID", "id")
}

func TestCalendarProposalOutIDRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldRejectsWrongShape(t, &value, "ID", "id")
}

func TestCalendarProposalOutIDMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldMissingPreservesValue(t, &value, "ID")
}

func TestCalendarProposalOutIDNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldNullSemantics(t, &value, "ID", "id")
}

func TestCalendarProposalOutTitleBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Title", "title")
}

func TestCalendarProposalOutTitleRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldRejectsWrongShape(t, &value, "Title", "title")
}

func TestCalendarProposalOutTitleMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldMissingPreservesValue(t, &value, "Title")
}

func TestCalendarProposalOutTitleNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldNullSemantics(t, &value, "Title", "title")
}

func TestCalendarProposalOutStartBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Start", "start")
}

func TestCalendarProposalOutStartRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldRejectsWrongShape(t, &value, "Start", "start")
}

func TestCalendarProposalOutStartMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldMissingPreservesValue(t, &value, "Start")
}

func TestCalendarProposalOutStartNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldNullSemantics(t, &value, "Start", "start")
}

func TestCalendarProposalOutAllDayBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldBoundaryRoundTrip(t, &value, "AllDay", "allDay")
}

func TestCalendarProposalOutAllDayRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldRejectsWrongShape(t, &value, "AllDay", "allDay")
}

func TestCalendarProposalOutAllDayMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldMissingPreservesValue(t, &value, "AllDay")
}

func TestCalendarProposalOutAllDayNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldNullSemantics(t, &value, "AllDay", "allDay")
}

func TestCalendarProposalOutKindBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Kind", "kind")
}

func TestCalendarProposalOutKindRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldRejectsWrongShape(t, &value, "Kind", "kind")
}

func TestCalendarProposalOutKindMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldMissingPreservesValue(t, &value, "Kind")
}

func TestCalendarProposalOutKindNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldNullSemantics(t, &value, "Kind", "kind")
}

func TestCalendarProposalOutSourceSubjectBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldBoundaryRoundTrip(t, &value, "SourceSubject", "sourceSubject")
}

func TestCalendarProposalOutSourceSubjectRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldRejectsWrongShape(t, &value, "SourceSubject", "sourceSubject")
}

func TestCalendarProposalOutSourceSubjectMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldMissingPreservesValue(t, &value, "SourceSubject")
}

func TestCalendarProposalOutSourceSubjectNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldNullSemantics(t, &value, "SourceSubject", "sourceSubject")
}

func TestCalendarProposalOutSourceFromBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldBoundaryRoundTrip(t, &value, "SourceFrom", "sourceFrom")
}

func TestCalendarProposalOutSourceFromRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldRejectsWrongShape(t, &value, "SourceFrom", "sourceFrom")
}

func TestCalendarProposalOutSourceFromMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldMissingPreservesValue(t, &value, "SourceFrom")
}

func TestCalendarProposalOutSourceFromNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldNullSemantics(t, &value, "SourceFrom", "sourceFrom")
}

func TestMiniappCronRowIDBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldBoundaryRoundTrip(t, &value, "ID", "id")
}

func TestMiniappCronRowIDRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldRejectsWrongShape(t, &value, "ID", "id")
}

func TestMiniappCronRowIDMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldMissingPreservesValue(t, &value, "ID")
}

func TestMiniappCronRowIDNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldNullSemantics(t, &value, "ID", "id")
}

func TestMiniappCronRowNameBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Name", "name")
}

func TestMiniappCronRowNameRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldRejectsWrongShape(t, &value, "Name", "name")
}

func TestMiniappCronRowNameMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldMissingPreservesValue(t, &value, "Name")
}

func TestMiniappCronRowNameNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldNullSemantics(t, &value, "Name", "name")
}

func TestMiniappCronRowEnabledBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Enabled", "enabled")
}

func TestMiniappCronRowEnabledRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldRejectsWrongShape(t, &value, "Enabled", "enabled")
}

func TestMiniappCronRowEnabledMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldMissingPreservesValue(t, &value, "Enabled")
}

func TestMiniappCronRowEnabledNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldNullSemantics(t, &value, "Enabled", "enabled")
}

func TestMiniappCronRowScheduleBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Schedule", "schedule")
}

func TestMiniappCronRowScheduleRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldRejectsWrongShape(t, &value, "Schedule", "schedule")
}

func TestMiniappCronRowScheduleMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldMissingPreservesValue(t, &value, "Schedule")
}

func TestMiniappCronRowScheduleNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldNullSemantics(t, &value, "Schedule", "schedule")
}

func TestMiniappCronRowPayloadKindBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldBoundaryRoundTrip(t, &value, "PayloadKind", "payloadKind")
}

func TestMiniappCronRowPayloadKindRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldRejectsWrongShape(t, &value, "PayloadKind", "payloadKind")
}

func TestMiniappCronRowPayloadKindMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldMissingPreservesValue(t, &value, "PayloadKind")
}

func TestMiniappCronRowPayloadKindNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldNullSemantics(t, &value, "PayloadKind", "payloadKind")
}

func TestMiniappCronRowPayloadPreviewBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldBoundaryRoundTrip(t, &value, "PayloadPreview", "payloadPreview")
}

func TestMiniappCronRowPayloadPreviewRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldRejectsWrongShape(t, &value, "PayloadPreview", "payloadPreview")
}

func TestMiniappCronRowPayloadPreviewMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldMissingPreservesValue(t, &value, "PayloadPreview")
}

func TestMiniappCronRowPayloadPreviewNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldNullSemantics(t, &value, "PayloadPreview", "payloadPreview")
}

func TestMiniappCronRowNextRunAtMsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldBoundaryRoundTrip(t, &value, "NextRunAtMs", "nextRunAtMs")
}

func TestMiniappCronRowNextRunAtMsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldRejectsWrongShape(t, &value, "NextRunAtMs", "nextRunAtMs")
}

func TestMiniappCronRowNextRunAtMsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldMissingPreservesValue(t, &value, "NextRunAtMs")
}

func TestMiniappCronRowNextRunAtMsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldNullSemantics(t, &value, "NextRunAtMs", "nextRunAtMs")
}

func TestMiniappCronRowConsecutiveErrorsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldBoundaryRoundTrip(t, &value, "ConsecutiveErrors", "consecutiveErrors")
}

func TestMiniappCronRowConsecutiveErrorsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldRejectsWrongShape(t, &value, "ConsecutiveErrors", "consecutiveErrors")
}

func TestMiniappCronRowConsecutiveErrorsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldMissingPreservesValue(t, &value, "ConsecutiveErrors")
}

func TestMiniappCronRowConsecutiveErrorsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldNullSemantics(t, &value, "ConsecutiveErrors", "consecutiveErrors")
}

func TestMiniappCronRowAutoDisabledAtMsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldBoundaryRoundTrip(t, &value, "AutoDisabledAtMs", "autoDisabledAtMs")
}

func TestMiniappCronRowAutoDisabledAtMsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldRejectsWrongShape(t, &value, "AutoDisabledAtMs", "autoDisabledAtMs")
}

func TestMiniappCronRowAutoDisabledAtMsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldMissingPreservesValue(t, &value, "AutoDisabledAtMs")
}

func TestMiniappCronRowAutoDisabledAtMsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldNullSemantics(t, &value, "AutoDisabledAtMs", "autoDisabledAtMs")
}

func TestMiniappCronRowLastErrorBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldBoundaryRoundTrip(t, &value, "LastError", "lastError")
}

func TestMiniappCronRowLastErrorRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldRejectsWrongShape(t, &value, "LastError", "lastError")
}

func TestMiniappCronRowLastErrorMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldMissingPreservesValue(t, &value, "LastError")
}

func TestMiniappCronRowLastErrorNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldNullSemantics(t, &value, "LastError", "lastError")
}

func TestMiniappCronDetailIDBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "ID", "id")
}

func TestMiniappCronDetailIDRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "ID", "id")
}

func TestMiniappCronDetailIDMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "ID")
}

func TestMiniappCronDetailIDNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "ID", "id")
}

func TestMiniappCronDetailNameBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "Name", "name")
}

func TestMiniappCronDetailNameRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "Name", "name")
}

func TestMiniappCronDetailNameMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "Name")
}

func TestMiniappCronDetailNameNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "Name", "name")
}

func TestMiniappCronDetailEnabledBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "Enabled", "enabled")
}

func TestMiniappCronDetailEnabledRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "Enabled", "enabled")
}

func TestMiniappCronDetailEnabledMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "Enabled")
}

func TestMiniappCronDetailEnabledNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "Enabled", "enabled")
}

func TestMiniappCronDetailAgentIDBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "AgentID", "agentId")
}

func TestMiniappCronDetailAgentIDRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "AgentID", "agentId")
}

func TestMiniappCronDetailAgentIDMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "AgentID")
}

func TestMiniappCronDetailAgentIDNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "AgentID", "agentId")
}

func TestMiniappCronDetailSessionTargetBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "SessionTarget", "sessionTarget")
}

func TestMiniappCronDetailSessionTargetRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "SessionTarget", "sessionTarget")
}

func TestMiniappCronDetailSessionTargetMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "SessionTarget")
}

func TestMiniappCronDetailSessionTargetNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "SessionTarget", "sessionTarget")
}

func TestMiniappCronDetailScheduleBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "Schedule", "schedule")
}

func TestMiniappCronDetailScheduleRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "Schedule", "schedule")
}

func TestMiniappCronDetailScheduleMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "Schedule")
}

func TestMiniappCronDetailScheduleNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "Schedule", "schedule")
}

func TestMiniappCronDetailScheduleSpecBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "ScheduleSpec", "scheduleSpec")
}

func TestMiniappCronDetailScheduleSpecRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "ScheduleSpec", "scheduleSpec")
}

func TestMiniappCronDetailScheduleSpecMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "ScheduleSpec")
}

func TestMiniappCronDetailScheduleSpecNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "ScheduleSpec", "scheduleSpec")
}

func TestMiniappCronDetailScheduleKindBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "ScheduleKind", "scheduleKind")
}

func TestMiniappCronDetailScheduleKindRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "ScheduleKind", "scheduleKind")
}

func TestMiniappCronDetailScheduleKindMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "ScheduleKind")
}

func TestMiniappCronDetailScheduleKindNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "ScheduleKind", "scheduleKind")
}

func TestMiniappCronDetailTimezoneBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "Timezone", "timezone")
}

func TestMiniappCronDetailTimezoneRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "Timezone", "timezone")
}

func TestMiniappCronDetailTimezoneMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "Timezone")
}

func TestMiniappCronDetailTimezoneNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "Timezone", "timezone")
}

func TestMiniappCronDetailCronExprBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "CronExpr", "cronExpr")
}

func TestMiniappCronDetailCronExprRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "CronExpr", "cronExpr")
}

func TestMiniappCronDetailCronExprMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "CronExpr")
}

func TestMiniappCronDetailCronExprNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "CronExpr", "cronExpr")
}

func TestMiniappCronDetailStaggerMsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "StaggerMs", "staggerMs")
}

func TestMiniappCronDetailStaggerMsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "StaggerMs", "staggerMs")
}

func TestMiniappCronDetailStaggerMsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "StaggerMs")
}

func TestMiniappCronDetailStaggerMsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "StaggerMs", "staggerMs")
}

func TestMiniappCronDetailPayloadKindBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "PayloadKind", "payloadKind")
}

func TestMiniappCronDetailPayloadKindRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "PayloadKind", "payloadKind")
}

func TestMiniappCronDetailPayloadKindMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "PayloadKind")
}

func TestMiniappCronDetailPayloadKindNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "PayloadKind", "payloadKind")
}

func TestMiniappCronDetailPromptBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "Prompt", "prompt")
}

func TestMiniappCronDetailPromptRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "Prompt", "prompt")
}

func TestMiniappCronDetailPromptMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "Prompt")
}

func TestMiniappCronDetailPromptNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "Prompt", "prompt")
}

func TestMiniappCronDetailModelBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "Model", "model")
}

func TestMiniappCronDetailModelRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "Model", "model")
}

func TestMiniappCronDetailModelMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "Model")
}

func TestMiniappCronDetailModelNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "Model", "model")
}

func TestMiniappCronDetailThinkingBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "Thinking", "thinking")
}

func TestMiniappCronDetailThinkingRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "Thinking", "thinking")
}

func TestMiniappCronDetailThinkingMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "Thinking")
}

func TestMiniappCronDetailThinkingNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "Thinking", "thinking")
}

func TestMiniappCronDetailTimeoutSecondsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "TimeoutSeconds", "timeoutSeconds")
}

func TestMiniappCronDetailTimeoutSecondsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "TimeoutSeconds", "timeoutSeconds")
}

func TestMiniappCronDetailTimeoutSecondsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "TimeoutSeconds")
}

func TestMiniappCronDetailTimeoutSecondsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "TimeoutSeconds", "timeoutSeconds")
}

func TestMiniappCronDetailLightContextBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "LightContext", "lightContext")
}

func TestMiniappCronDetailLightContextRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "LightContext", "lightContext")
}

func TestMiniappCronDetailLightContextMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "LightContext")
}

func TestMiniappCronDetailLightContextNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "LightContext", "lightContext")
}

func TestMiniappCronDetailRetryCountBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "RetryCount", "retryCount")
}

func TestMiniappCronDetailRetryCountRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "RetryCount", "retryCount")
}

func TestMiniappCronDetailRetryCountMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "RetryCount")
}

func TestMiniappCronDetailRetryCountNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "RetryCount", "retryCount")
}

func TestMiniappCronDetailDeliveryChannelBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "DeliveryChannel", "deliveryChannel")
}

func TestMiniappCronDetailDeliveryChannelRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "DeliveryChannel", "deliveryChannel")
}

func TestMiniappCronDetailDeliveryChannelMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "DeliveryChannel")
}

func TestMiniappCronDetailDeliveryChannelNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "DeliveryChannel", "deliveryChannel")
}

func TestMiniappCronDetailDeliveryToBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "DeliveryTo", "deliveryTo")
}

func TestMiniappCronDetailDeliveryToRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "DeliveryTo", "deliveryTo")
}

func TestMiniappCronDetailDeliveryToMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "DeliveryTo")
}

func TestMiniappCronDetailDeliveryToNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "DeliveryTo", "deliveryTo")
}

func TestMiniappCronDetailDeliveryThreadIDBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "DeliveryThreadID", "deliveryThreadId")
}

func TestMiniappCronDetailDeliveryThreadIDRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "DeliveryThreadID", "deliveryThreadId")
}

func TestMiniappCronDetailDeliveryThreadIDMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "DeliveryThreadID")
}

func TestMiniappCronDetailDeliveryThreadIDNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "DeliveryThreadID", "deliveryThreadId")
}

func TestMiniappCronDetailFailureAlertAfterBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "FailureAlertAfter", "failureAlertAfter")
}

func TestMiniappCronDetailFailureAlertAfterRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "FailureAlertAfter", "failureAlertAfter")
}

func TestMiniappCronDetailFailureAlertAfterMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "FailureAlertAfter")
}

func TestMiniappCronDetailFailureAlertAfterNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "FailureAlertAfter", "failureAlertAfter")
}

func TestMiniappCronDetailNextRunAtMsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "NextRunAtMs", "nextRunAtMs")
}

func TestMiniappCronDetailNextRunAtMsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "NextRunAtMs", "nextRunAtMs")
}

func TestMiniappCronDetailNextRunAtMsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "NextRunAtMs")
}

func TestMiniappCronDetailNextRunAtMsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "NextRunAtMs", "nextRunAtMs")
}

func TestMiniappCronDetailLastSessionKeyBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "LastSessionKey", "lastSessionKey")
}

func TestMiniappCronDetailLastSessionKeyRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "LastSessionKey", "lastSessionKey")
}

func TestMiniappCronDetailLastSessionKeyMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "LastSessionKey")
}

func TestMiniappCronDetailLastSessionKeyNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "LastSessionKey", "lastSessionKey")
}

func TestMiniappCronDetailLastDeliveryStatusBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "LastDeliveryStatus", "lastDeliveryStatus")
}

func TestMiniappCronDetailLastDeliveryStatusRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "LastDeliveryStatus", "lastDeliveryStatus")
}

func TestMiniappCronDetailLastDeliveryStatusMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "LastDeliveryStatus")
}

func TestMiniappCronDetailLastDeliveryStatusNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "LastDeliveryStatus", "lastDeliveryStatus")
}

func TestMiniappCronDetailLastErrorBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "LastError", "lastError")
}

func TestMiniappCronDetailLastErrorRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "LastError", "lastError")
}

func TestMiniappCronDetailLastErrorMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "LastError")
}

func TestMiniappCronDetailLastErrorNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "LastError", "lastError")
}

func TestMiniappCronDetailConsecutiveErrorsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "ConsecutiveErrors", "consecutiveErrors")
}

func TestMiniappCronDetailConsecutiveErrorsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "ConsecutiveErrors", "consecutiveErrors")
}

func TestMiniappCronDetailConsecutiveErrorsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "ConsecutiveErrors")
}

func TestMiniappCronDetailConsecutiveErrorsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "ConsecutiveErrors", "consecutiveErrors")
}

func TestMiniappCronDetailAutoDisabledAtMsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "AutoDisabledAtMs", "autoDisabledAtMs")
}

func TestMiniappCronDetailAutoDisabledAtMsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "AutoDisabledAtMs", "autoDisabledAtMs")
}

func TestMiniappCronDetailAutoDisabledAtMsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "AutoDisabledAtMs")
}

func TestMiniappCronDetailAutoDisabledAtMsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "AutoDisabledAtMs", "autoDisabledAtMs")
}

func TestMiniappCronDetailCreatedAtMsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "CreatedAtMs", "createdAtMs")
}

func TestMiniappCronDetailCreatedAtMsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "CreatedAtMs", "createdAtMs")
}

func TestMiniappCronDetailCreatedAtMsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "CreatedAtMs")
}

func TestMiniappCronDetailCreatedAtMsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "CreatedAtMs", "createdAtMs")
}

func TestMiniappCronDetailUpdatedAtMsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldBoundaryRoundTrip(t, &value, "UpdatedAtMs", "updatedAtMs")
}

func TestMiniappCronDetailUpdatedAtMsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldRejectsWrongShape(t, &value, "UpdatedAtMs", "updatedAtMs")
}

func TestMiniappCronDetailUpdatedAtMsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldMissingPreservesValue(t, &value, "UpdatedAtMs")
}

func TestMiniappCronDetailUpdatedAtMsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldNullSemantics(t, &value, "UpdatedAtMs", "updatedAtMs")
}

func TestTodoOutIDBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldBoundaryRoundTrip(t, &value, "ID", "id")
}

func TestTodoOutIDRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldRejectsWrongShape(t, &value, "ID", "id")
}

func TestTodoOutIDMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldMissingPreservesValue(t, &value, "ID")
}

func TestTodoOutIDNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldNullSemantics(t, &value, "ID", "id")
}

func TestTodoOutTitleBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Title", "title")
}

func TestTodoOutTitleRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldRejectsWrongShape(t, &value, "Title", "title")
}

func TestTodoOutTitleMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldMissingPreservesValue(t, &value, "Title")
}

func TestTodoOutTitleNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldNullSemantics(t, &value, "Title", "title")
}

func TestTodoOutNoteBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Note", "note")
}

func TestTodoOutNoteRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldRejectsWrongShape(t, &value, "Note", "note")
}

func TestTodoOutNoteMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldMissingPreservesValue(t, &value, "Note")
}

func TestTodoOutNoteNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldNullSemantics(t, &value, "Note", "note")
}

func TestTodoOutDueBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Due", "due")
}

func TestTodoOutDueRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldRejectsWrongShape(t, &value, "Due", "due")
}

func TestTodoOutDueMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldMissingPreservesValue(t, &value, "Due")
}

func TestTodoOutDueNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldNullSemantics(t, &value, "Due", "due")
}

func TestTodoOutDueAllDayBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldBoundaryRoundTrip(t, &value, "DueAllDay", "dueAllDay")
}

func TestTodoOutDueAllDayRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldRejectsWrongShape(t, &value, "DueAllDay", "dueAllDay")
}

func TestTodoOutDueAllDayMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldMissingPreservesValue(t, &value, "DueAllDay")
}

func TestTodoOutDueAllDayNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldNullSemantics(t, &value, "DueAllDay", "dueAllDay")
}

func TestTodoOutDoneBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Done", "done")
}

func TestTodoOutDoneRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldRejectsWrongShape(t, &value, "Done", "done")
}

func TestTodoOutDoneMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldMissingPreservesValue(t, &value, "Done")
}

func TestTodoOutDoneNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldNullSemantics(t, &value, "Done", "done")
}

func TestTodoOutDoneAtBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldBoundaryRoundTrip(t, &value, "DoneAt", "doneAt")
}

func TestTodoOutDoneAtRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldRejectsWrongShape(t, &value, "DoneAt", "doneAt")
}

func TestTodoOutDoneAtMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldMissingPreservesValue(t, &value, "DoneAt")
}

func TestTodoOutDoneAtNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldNullSemantics(t, &value, "DoneAt", "doneAt")
}

func assertWireFieldOmitsZeroValue(t *testing.T, target any, jsonName string) {
	t.Helper()

	encoded, err := json.Marshal(target)
	if err != nil {
		t.Fatalf("marshal zero %T: %v", target, err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatalf("decode zero object for %T: %v", target, err)
	}
	if value, ok := object[jsonName]; ok {
		t.Fatalf("zero %T unexpectedly emitted omitempty key %q as %s", target, jsonName, value)
	}
}

func TestCalendarEventOutDescriptionOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldOmitsZeroValue(t, &value, "description")
}

func TestCalendarEventOutLocationOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldOmitsZeroValue(t, &value, "location")
}

func TestCalendarEventOutAllDayOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldOmitsZeroValue(t, &value, "allDay")
}

func TestCalendarEventOutStatusOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldOmitsZeroValue(t, &value, "status")
}

func TestCalendarEventOutHTMLLinkOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldOmitsZeroValue(t, &value, "htmlLink")
}

func TestCalendarEventOutLocalOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldOmitsZeroValue(t, &value, "local")
}

func TestCalendarEventOutCategoryOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldOmitsZeroValue(t, &value, "category")
}

func TestCalendarEventOutOrganizerOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldOmitsZeroValue(t, &value, "organizer")
}

func TestCalendarEventOutAttendeesOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldOmitsZeroValue(t, &value, "attendees")
}

func TestCalendarEventOutConferenceOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldOmitsZeroValue(t, &value, "conference")
}

func TestCalendarEventOutHasMeetOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value calendarEventOut
	assertWireFieldOmitsZeroValue(t, &value, "hasMeet")
}

func TestCalendarProposalOutSourceSubjectOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldOmitsZeroValue(t, &value, "sourceSubject")
}

func TestCalendarProposalOutSourceFromOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value calendarProposalOut
	assertWireFieldOmitsZeroValue(t, &value, "sourceFrom")
}

func TestMiniappCronRowNameOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldOmitsZeroValue(t, &value, "name")
}

func TestMiniappCronRowPayloadPreviewOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldOmitsZeroValue(t, &value, "payloadPreview")
}

func TestMiniappCronRowNextRunAtMsOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldOmitsZeroValue(t, &value, "nextRunAtMs")
}

func TestMiniappCronRowConsecutiveErrorsOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldOmitsZeroValue(t, &value, "consecutiveErrors")
}

func TestMiniappCronRowAutoDisabledAtMsOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldOmitsZeroValue(t, &value, "autoDisabledAtMs")
}

func TestMiniappCronRowLastErrorOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronRow
	assertWireFieldOmitsZeroValue(t, &value, "lastError")
}

func TestMiniappCronDetailNameOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldOmitsZeroValue(t, &value, "name")
}

func TestMiniappCronDetailAgentIDOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldOmitsZeroValue(t, &value, "agentId")
}

func TestMiniappCronDetailSessionTargetOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldOmitsZeroValue(t, &value, "sessionTarget")
}

func TestMiniappCronDetailTimezoneOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldOmitsZeroValue(t, &value, "timezone")
}

func TestMiniappCronDetailCronExprOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldOmitsZeroValue(t, &value, "cronExpr")
}

func TestMiniappCronDetailStaggerMsOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldOmitsZeroValue(t, &value, "staggerMs")
}

func TestMiniappCronDetailPromptOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldOmitsZeroValue(t, &value, "prompt")
}

func TestMiniappCronDetailModelOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldOmitsZeroValue(t, &value, "model")
}

func TestMiniappCronDetailThinkingOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldOmitsZeroValue(t, &value, "thinking")
}

func TestMiniappCronDetailTimeoutSecondsOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldOmitsZeroValue(t, &value, "timeoutSeconds")
}

func TestMiniappCronDetailLightContextOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldOmitsZeroValue(t, &value, "lightContext")
}

func TestMiniappCronDetailRetryCountOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldOmitsZeroValue(t, &value, "retryCount")
}

func TestMiniappCronDetailDeliveryChannelOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldOmitsZeroValue(t, &value, "deliveryChannel")
}

func TestMiniappCronDetailDeliveryToOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldOmitsZeroValue(t, &value, "deliveryTo")
}

func TestMiniappCronDetailDeliveryThreadIDOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldOmitsZeroValue(t, &value, "deliveryThreadId")
}

func TestMiniappCronDetailFailureAlertAfterOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldOmitsZeroValue(t, &value, "failureAlertAfter")
}

func TestMiniappCronDetailNextRunAtMsOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldOmitsZeroValue(t, &value, "nextRunAtMs")
}

func TestMiniappCronDetailLastSessionKeyOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldOmitsZeroValue(t, &value, "lastSessionKey")
}

func TestMiniappCronDetailLastDeliveryStatusOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldOmitsZeroValue(t, &value, "lastDeliveryStatus")
}

func TestMiniappCronDetailLastErrorOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldOmitsZeroValue(t, &value, "lastError")
}

func TestMiniappCronDetailConsecutiveErrorsOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldOmitsZeroValue(t, &value, "consecutiveErrors")
}

func TestMiniappCronDetailAutoDisabledAtMsOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldOmitsZeroValue(t, &value, "autoDisabledAtMs")
}

func TestMiniappCronDetailCreatedAtMsOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldOmitsZeroValue(t, &value, "createdAtMs")
}

func TestMiniappCronDetailUpdatedAtMsOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value MiniappCronDetail
	assertWireFieldOmitsZeroValue(t, &value, "updatedAtMs")
}

func TestTodoOutNoteOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldOmitsZeroValue(t, &value, "note")
}

func TestTodoOutDueOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldOmitsZeroValue(t, &value, "due")
}

func TestTodoOutDueAllDayOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldOmitsZeroValue(t, &value, "dueAllDay")
}

func TestTodoOutDoneOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldOmitsZeroValue(t, &value, "done")
}

func TestTodoOutDoneAtOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value todoOut
	assertWireFieldOmitsZeroValue(t, &value, "doneAt")
}
