package handlerminiapp

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

func TestContactRowNameBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ContactRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Name", "name")
}

func TestContactRowNameRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ContactRow
	assertWireFieldRejectsWrongShape(t, &value, "Name", "name")
}

func TestContactRowNameMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ContactRow
	assertWireFieldMissingPreservesValue(t, &value, "Name")
}

func TestContactRowNameNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ContactRow
	assertWireFieldNullSemantics(t, &value, "Name", "name")
}

func TestContactRowPhonesBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ContactRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Phones", "phones")
}

func TestContactRowPhonesRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ContactRow
	assertWireFieldRejectsWrongShape(t, &value, "Phones", "phones")
}

func TestContactRowPhonesMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ContactRow
	assertWireFieldMissingPreservesValue(t, &value, "Phones")
}

func TestContactRowPhonesNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ContactRow
	assertWireFieldNullSemantics(t, &value, "Phones", "phones")
}

func TestContactRowEmailsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ContactRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Emails", "emails")
}

func TestContactRowEmailsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ContactRow
	assertWireFieldRejectsWrongShape(t, &value, "Emails", "emails")
}

func TestContactRowEmailsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ContactRow
	assertWireFieldMissingPreservesValue(t, &value, "Emails")
}

func TestContactRowEmailsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ContactRow
	assertWireFieldNullSemantics(t, &value, "Emails", "emails")
}

func TestContactRowOrgBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ContactRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Org", "org")
}

func TestContactRowOrgRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ContactRow
	assertWireFieldRejectsWrongShape(t, &value, "Org", "org")
}

func TestContactRowOrgMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ContactRow
	assertWireFieldMissingPreservesValue(t, &value, "Org")
}

func TestContactRowOrgNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ContactRow
	assertWireFieldNullSemantics(t, &value, "Org", "org")
}

func TestDashboardItemTitleBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value DashboardItem
	assertWireFieldBoundaryRoundTrip(t, &value, "Title", "title")
}

func TestDashboardItemTitleRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value DashboardItem
	assertWireFieldRejectsWrongShape(t, &value, "Title", "title")
}

func TestDashboardItemTitleMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value DashboardItem
	assertWireFieldMissingPreservesValue(t, &value, "Title")
}

func TestDashboardItemTitleNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value DashboardItem
	assertWireFieldNullSemantics(t, &value, "Title", "title")
}

func TestDashboardItemSubtitleBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value DashboardItem
	assertWireFieldBoundaryRoundTrip(t, &value, "Subtitle", "subtitle")
}

func TestDashboardItemSubtitleRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value DashboardItem
	assertWireFieldRejectsWrongShape(t, &value, "Subtitle", "subtitle")
}

func TestDashboardItemSubtitleMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value DashboardItem
	assertWireFieldMissingPreservesValue(t, &value, "Subtitle")
}

func TestDashboardItemSubtitleNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value DashboardItem
	assertWireFieldNullSemantics(t, &value, "Subtitle", "subtitle")
}

func TestDashboardItemSourceBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value DashboardItem
	assertWireFieldBoundaryRoundTrip(t, &value, "Source", "source")
}

func TestDashboardItemSourceRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value DashboardItem
	assertWireFieldRejectsWrongShape(t, &value, "Source", "source")
}

func TestDashboardItemSourceMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value DashboardItem
	assertWireFieldMissingPreservesValue(t, &value, "Source")
}

func TestDashboardItemSourceNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value DashboardItem
	assertWireFieldNullSemantics(t, &value, "Source", "source")
}

func TestDashboardItemRefTypeBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value DashboardItem
	assertWireFieldBoundaryRoundTrip(t, &value, "RefType", "refType")
}

func TestDashboardItemRefTypeRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value DashboardItem
	assertWireFieldRejectsWrongShape(t, &value, "RefType", "refType")
}

func TestDashboardItemRefTypeMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value DashboardItem
	assertWireFieldMissingPreservesValue(t, &value, "RefType")
}

func TestDashboardItemRefTypeNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value DashboardItem
	assertWireFieldNullSemantics(t, &value, "RefType", "refType")
}

func TestDashboardItemRefIDBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value DashboardItem
	assertWireFieldBoundaryRoundTrip(t, &value, "RefID", "refId")
}

func TestDashboardItemRefIDRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value DashboardItem
	assertWireFieldRejectsWrongShape(t, &value, "RefID", "refId")
}

func TestDashboardItemRefIDMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value DashboardItem
	assertWireFieldMissingPreservesValue(t, &value, "RefID")
}

func TestDashboardItemRefIDNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value DashboardItem
	assertWireFieldNullSemantics(t, &value, "RefID", "refId")
}

func TestDashboardItemWhenMsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value DashboardItem
	assertWireFieldBoundaryRoundTrip(t, &value, "WhenMs", "whenMs")
}

func TestDashboardItemWhenMsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value DashboardItem
	assertWireFieldRejectsWrongShape(t, &value, "WhenMs", "whenMs")
}

func TestDashboardItemWhenMsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value DashboardItem
	assertWireFieldMissingPreservesValue(t, &value, "WhenMs")
}

func TestDashboardItemWhenMsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value DashboardItem
	assertWireFieldNullSemantics(t, &value, "WhenMs", "whenMs")
}

func TestLaneOutKeyBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value LaneOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Key", "key")
}

func TestLaneOutKeyRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value LaneOut
	assertWireFieldRejectsWrongShape(t, &value, "Key", "key")
}

func TestLaneOutKeyMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value LaneOut
	assertWireFieldMissingPreservesValue(t, &value, "Key")
}

func TestLaneOutKeyNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value LaneOut
	assertWireFieldNullSemantics(t, &value, "Key", "key")
}

func TestLaneOutNameBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value LaneOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Name", "name")
}

func TestLaneOutNameRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value LaneOut
	assertWireFieldRejectsWrongShape(t, &value, "Name", "name")
}

func TestLaneOutNameMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value LaneOut
	assertWireFieldMissingPreservesValue(t, &value, "Name")
}

func TestLaneOutNameNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value LaneOut
	assertWireFieldNullSemantics(t, &value, "Name", "name")
}

func TestLaneOutItemsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value LaneOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Items", "items")
}

func TestLaneOutItemsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value LaneOut
	assertWireFieldRejectsWrongShape(t, &value, "Items", "items")
}

func TestLaneOutItemsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value LaneOut
	assertWireFieldMissingPreservesValue(t, &value, "Items")
}

func TestLaneOutItemsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value LaneOut
	assertWireFieldNullSemantics(t, &value, "Items", "items")
}

func TestDashboardOutLanesBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value DashboardOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Lanes", "lanes")
}

func TestDashboardOutLanesRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value DashboardOut
	assertWireFieldRejectsWrongShape(t, &value, "Lanes", "lanes")
}

func TestDashboardOutLanesMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value DashboardOut
	assertWireFieldMissingPreservesValue(t, &value, "Lanes")
}

func TestDashboardOutLanesNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value DashboardOut
	assertWireFieldNullSemantics(t, &value, "Lanes", "lanes")
}

func TestMailRowOutIDBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "ID", "id")
}

func TestMailRowOutIDRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldRejectsWrongShape(t, &value, "ID", "id")
}

func TestMailRowOutIDMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldMissingPreservesValue(t, &value, "ID")
}

func TestMailRowOutIDNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldNullSemantics(t, &value, "ID", "id")
}

func TestMailRowOutThreadIDBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "ThreadID", "threadId")
}

func TestMailRowOutThreadIDRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldRejectsWrongShape(t, &value, "ThreadID", "threadId")
}

func TestMailRowOutThreadIDMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldMissingPreservesValue(t, &value, "ThreadID")
}

func TestMailRowOutThreadIDNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldNullSemantics(t, &value, "ThreadID", "threadId")
}

func TestMailRowOutFromBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "From", "from")
}

func TestMailRowOutFromRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldRejectsWrongShape(t, &value, "From", "from")
}

func TestMailRowOutFromMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldMissingPreservesValue(t, &value, "From")
}

func TestMailRowOutFromNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldNullSemantics(t, &value, "From", "from")
}

func TestMailRowOutSubjectBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Subject", "subject")
}

func TestMailRowOutSubjectRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldRejectsWrongShape(t, &value, "Subject", "subject")
}

func TestMailRowOutSubjectMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldMissingPreservesValue(t, &value, "Subject")
}

func TestMailRowOutSubjectNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldNullSemantics(t, &value, "Subject", "subject")
}

func TestMailRowOutSnippetBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Snippet", "snippet")
}

func TestMailRowOutSnippetRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldRejectsWrongShape(t, &value, "Snippet", "snippet")
}

func TestMailRowOutSnippetMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldMissingPreservesValue(t, &value, "Snippet")
}

func TestMailRowOutSnippetNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldNullSemantics(t, &value, "Snippet", "snippet")
}

func TestMailRowOutDateBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Date", "date")
}

func TestMailRowOutDateRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldRejectsWrongShape(t, &value, "Date", "date")
}

func TestMailRowOutDateMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldMissingPreservesValue(t, &value, "Date")
}

func TestMailRowOutDateNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldNullSemantics(t, &value, "Date", "date")
}

func TestMailRowOutIsUnreadBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "IsUnread", "isUnread")
}

func TestMailRowOutIsUnreadRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldRejectsWrongShape(t, &value, "IsUnread", "isUnread")
}

func TestMailRowOutIsUnreadMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldMissingPreservesValue(t, &value, "IsUnread")
}

func TestMailRowOutIsUnreadNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldNullSemantics(t, &value, "IsUnread", "isUnread")
}

func TestMailRowOutLabelsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Labels", "labels")
}

func TestMailRowOutLabelsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldRejectsWrongShape(t, &value, "Labels", "labels")
}

func TestMailRowOutLabelsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldMissingPreservesValue(t, &value, "Labels")
}

func TestMailRowOutLabelsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldNullSemantics(t, &value, "Labels", "labels")
}

func TestMailRowOutMailboxBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Mailbox", "mailbox")
}

func TestMailRowOutMailboxRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldRejectsWrongShape(t, &value, "Mailbox", "mailbox")
}

func TestMailRowOutMailboxMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldMissingPreservesValue(t, &value, "Mailbox")
}

func TestMailRowOutMailboxNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldNullSemantics(t, &value, "Mailbox", "mailbox")
}

func TestMailRowOutHasAttachmentBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "HasAttachment", "hasAttachment")
}

func TestMailRowOutHasAttachmentRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldRejectsWrongShape(t, &value, "HasAttachment", "hasAttachment")
}

func TestMailRowOutHasAttachmentMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldMissingPreservesValue(t, &value, "HasAttachment")
}

func TestMailRowOutHasAttachmentNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldNullSemantics(t, &value, "HasAttachment", "hasAttachment")
}

func TestMailRowOutAttachmentCountBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "AttachmentCount", "attachmentCount")
}

func TestMailRowOutAttachmentCountRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldRejectsWrongShape(t, &value, "AttachmentCount", "attachmentCount")
}

func TestMailRowOutAttachmentCountMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldMissingPreservesValue(t, &value, "AttachmentCount")
}

func TestMailRowOutAttachmentCountNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldNullSemantics(t, &value, "AttachmentCount", "attachmentCount")
}

func TestMailRowOutPriorityBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Priority", "priority")
}

func TestMailRowOutPriorityRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldRejectsWrongShape(t, &value, "Priority", "priority")
}

func TestMailRowOutPriorityMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldMissingPreservesValue(t, &value, "Priority")
}

func TestMailRowOutPriorityNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldNullSemantics(t, &value, "Priority", "priority")
}

func TestMailRowOutPriorityHintBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "PriorityHint", "priorityHint")
}

func TestMailRowOutPriorityHintRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldRejectsWrongShape(t, &value, "PriorityHint", "priorityHint")
}

func TestMailRowOutPriorityHintMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldMissingPreservesValue(t, &value, "PriorityHint")
}

func TestMailRowOutPriorityHintNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldNullSemantics(t, &value, "PriorityHint", "priorityHint")
}

func TestMailRowOutAnalysisStatusBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "AnalysisStatus", "analysisStatus")
}

func TestMailRowOutAnalysisStatusRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldRejectsWrongShape(t, &value, "AnalysisStatus", "analysisStatus")
}

func TestMailRowOutAnalysisStatusMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldMissingPreservesValue(t, &value, "AnalysisStatus")
}

func TestMailRowOutAnalysisStatusNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldNullSemantics(t, &value, "AnalysisStatus", "analysisStatus")
}

func TestMailRowOutAnalysisQualityBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "AnalysisQuality", "analysisQuality")
}

func TestMailRowOutAnalysisQualityRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldRejectsWrongShape(t, &value, "AnalysisQuality", "analysisQuality")
}

func TestMailRowOutAnalysisQualityMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldMissingPreservesValue(t, &value, "AnalysisQuality")
}

func TestMailRowOutAnalysisQualityNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldNullSemantics(t, &value, "AnalysisQuality", "analysisQuality")
}

func TestMailRowOutFeedStatusBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "FeedStatus", "feedStatus")
}

func TestMailRowOutFeedStatusRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldRejectsWrongShape(t, &value, "FeedStatus", "feedStatus")
}

func TestMailRowOutFeedStatusMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldMissingPreservesValue(t, &value, "FeedStatus")
}

func TestMailRowOutFeedStatusNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldNullSemantics(t, &value, "FeedStatus", "feedStatus")
}

func TestMailRowOutCalendarProposalCountBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "CalendarProposalCount", "calendarProposalCount")
}

func TestMailRowOutCalendarProposalCountRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldRejectsWrongShape(t, &value, "CalendarProposalCount", "calendarProposalCount")
}

func TestMailRowOutCalendarProposalCountMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldMissingPreservesValue(t, &value, "CalendarProposalCount")
}

func TestMailRowOutCalendarProposalCountNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldNullSemantics(t, &value, "CalendarProposalCount", "calendarProposalCount")
}

func TestMailRowOutTodoCountBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "TodoCount", "todoCount")
}

func TestMailRowOutTodoCountRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldRejectsWrongShape(t, &value, "TodoCount", "todoCount")
}

func TestMailRowOutTodoCountMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldMissingPreservesValue(t, &value, "TodoCount")
}

func TestMailRowOutTodoCountNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldNullSemantics(t, &value, "TodoCount", "todoCount")
}

func TestMailRowOutWorkStateHintBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "WorkStateHint", "workStateHint")
}

func TestMailRowOutWorkStateHintRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldRejectsWrongShape(t, &value, "WorkStateHint", "workStateHint")
}

func TestMailRowOutWorkStateHintMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldMissingPreservesValue(t, &value, "WorkStateHint")
}

func TestMailRowOutWorkStateHintNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldNullSemantics(t, &value, "WorkStateHint", "workStateHint")
}

func TestMailRowOutRelatedProjectsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "RelatedProjects", "relatedProjects")
}

func TestMailRowOutRelatedProjectsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldRejectsWrongShape(t, &value, "RelatedProjects", "relatedProjects")
}

func TestMailRowOutRelatedProjectsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldMissingPreservesValue(t, &value, "RelatedProjects")
}

func TestMailRowOutRelatedProjectsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldNullSemantics(t, &value, "RelatedProjects", "relatedProjects")
}

func TestMailMessageOutIDBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldBoundaryRoundTrip(t, &value, "ID", "id")
}

func TestMailMessageOutIDRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldRejectsWrongShape(t, &value, "ID", "id")
}

func TestMailMessageOutIDMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldMissingPreservesValue(t, &value, "ID")
}

func TestMailMessageOutIDNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldNullSemantics(t, &value, "ID", "id")
}

func TestMailMessageOutThreadIDBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldBoundaryRoundTrip(t, &value, "ThreadID", "threadId")
}

func TestMailMessageOutThreadIDRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldRejectsWrongShape(t, &value, "ThreadID", "threadId")
}

func TestMailMessageOutThreadIDMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldMissingPreservesValue(t, &value, "ThreadID")
}

func TestMailMessageOutThreadIDNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldNullSemantics(t, &value, "ThreadID", "threadId")
}

func TestMailMessageOutFromBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldBoundaryRoundTrip(t, &value, "From", "from")
}

func TestMailMessageOutFromRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldRejectsWrongShape(t, &value, "From", "from")
}

func TestMailMessageOutFromMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldMissingPreservesValue(t, &value, "From")
}

func TestMailMessageOutFromNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldNullSemantics(t, &value, "From", "from")
}

func TestMailMessageOutToBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldBoundaryRoundTrip(t, &value, "To", "to")
}

func TestMailMessageOutToRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldRejectsWrongShape(t, &value, "To", "to")
}

func TestMailMessageOutToMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldMissingPreservesValue(t, &value, "To")
}

func TestMailMessageOutToNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldNullSemantics(t, &value, "To", "to")
}

func TestMailMessageOutCCBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldBoundaryRoundTrip(t, &value, "CC", "cc")
}

func TestMailMessageOutCCRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldRejectsWrongShape(t, &value, "CC", "cc")
}

func TestMailMessageOutCCMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldMissingPreservesValue(t, &value, "CC")
}

func TestMailMessageOutCCNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldNullSemantics(t, &value, "CC", "cc")
}

func TestMailMessageOutSubjectBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Subject", "subject")
}

func TestMailMessageOutSubjectRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldRejectsWrongShape(t, &value, "Subject", "subject")
}

func TestMailMessageOutSubjectMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldMissingPreservesValue(t, &value, "Subject")
}

func TestMailMessageOutSubjectNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldNullSemantics(t, &value, "Subject", "subject")
}

func TestMailMessageOutDateBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Date", "date")
}

func TestMailMessageOutDateRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldRejectsWrongShape(t, &value, "Date", "date")
}

func TestMailMessageOutDateMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldMissingPreservesValue(t, &value, "Date")
}

func TestMailMessageOutDateNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldNullSemantics(t, &value, "Date", "date")
}

func TestMailMessageOutIsUnreadBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldBoundaryRoundTrip(t, &value, "IsUnread", "isUnread")
}

func TestMailMessageOutIsUnreadRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldRejectsWrongShape(t, &value, "IsUnread", "isUnread")
}

func TestMailMessageOutIsUnreadMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldMissingPreservesValue(t, &value, "IsUnread")
}

func TestMailMessageOutIsUnreadNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldNullSemantics(t, &value, "IsUnread", "isUnread")
}

func TestMailMessageOutBodyBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Body", "body")
}

func TestMailMessageOutBodyRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldRejectsWrongShape(t, &value, "Body", "body")
}

func TestMailMessageOutBodyMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldMissingPreservesValue(t, &value, "Body")
}

func TestMailMessageOutBodyNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldNullSemantics(t, &value, "Body", "body")
}

func TestMailMessageOutBodyTotalBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldBoundaryRoundTrip(t, &value, "BodyTotal", "bodyTotal")
}

func TestMailMessageOutBodyTotalRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldRejectsWrongShape(t, &value, "BodyTotal", "bodyTotal")
}

func TestMailMessageOutBodyTotalMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldMissingPreservesValue(t, &value, "BodyTotal")
}

func TestMailMessageOutBodyTotalNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldNullSemantics(t, &value, "BodyTotal", "bodyTotal")
}

func TestMailMessageOutRawBodyBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldBoundaryRoundTrip(t, &value, "RawBody", "rawBody")
}

func TestMailMessageOutRawBodyRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldRejectsWrongShape(t, &value, "RawBody", "rawBody")
}

func TestMailMessageOutRawBodyMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldMissingPreservesValue(t, &value, "RawBody")
}

func TestMailMessageOutRawBodyNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldNullSemantics(t, &value, "RawBody", "rawBody")
}

func TestMailMessageOutRawBodyTotalBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldBoundaryRoundTrip(t, &value, "RawBodyTotal", "rawBodyTotal")
}

func TestMailMessageOutRawBodyTotalRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldRejectsWrongShape(t, &value, "RawBodyTotal", "rawBodyTotal")
}

func TestMailMessageOutRawBodyTotalMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldMissingPreservesValue(t, &value, "RawBodyTotal")
}

func TestMailMessageOutRawBodyTotalNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldNullSemantics(t, &value, "RawBodyTotal", "rawBodyTotal")
}

func TestMailMessageOutBodyCleanedBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldBoundaryRoundTrip(t, &value, "BodyCleaned", "bodyCleaned")
}

func TestMailMessageOutBodyCleanedRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldRejectsWrongShape(t, &value, "BodyCleaned", "bodyCleaned")
}

func TestMailMessageOutBodyCleanedMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldMissingPreservesValue(t, &value, "BodyCleaned")
}

func TestMailMessageOutBodyCleanedNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldNullSemantics(t, &value, "BodyCleaned", "bodyCleaned")
}

func TestMailMessageOutBodyHiddenBlockCountBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldBoundaryRoundTrip(t, &value, "BodyHiddenBlockCount", "bodyHiddenBlockCount")
}

func TestMailMessageOutBodyHiddenBlockCountRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldRejectsWrongShape(t, &value, "BodyHiddenBlockCount", "bodyHiddenBlockCount")
}

func TestMailMessageOutBodyHiddenBlockCountMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldMissingPreservesValue(t, &value, "BodyHiddenBlockCount")
}

func TestMailMessageOutBodyHiddenBlockCountNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldNullSemantics(t, &value, "BodyHiddenBlockCount", "bodyHiddenBlockCount")
}

func TestMailMessageOutBodyHiddenLineCountBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldBoundaryRoundTrip(t, &value, "BodyHiddenLineCount", "bodyHiddenLineCount")
}

func TestMailMessageOutBodyHiddenLineCountRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldRejectsWrongShape(t, &value, "BodyHiddenLineCount", "bodyHiddenLineCount")
}

func TestMailMessageOutBodyHiddenLineCountMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldMissingPreservesValue(t, &value, "BodyHiddenLineCount")
}

func TestMailMessageOutBodyHiddenLineCountNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldNullSemantics(t, &value, "BodyHiddenLineCount", "bodyHiddenLineCount")
}

func TestMailMessageOutLabelsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Labels", "labels")
}

func TestMailMessageOutLabelsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldRejectsWrongShape(t, &value, "Labels", "labels")
}

func TestMailMessageOutLabelsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldMissingPreservesValue(t, &value, "Labels")
}

func TestMailMessageOutLabelsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldNullSemantics(t, &value, "Labels", "labels")
}

func TestMailMessageOutAttachmentsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Attachments", "attachments")
}

func TestMailMessageOutAttachmentsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldRejectsWrongShape(t, &value, "Attachments", "attachments")
}

func TestMailMessageOutAttachmentsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldMissingPreservesValue(t, &value, "Attachments")
}

func TestMailMessageOutAttachmentsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldNullSemantics(t, &value, "Attachments", "attachments")
}

func TestMailMessageOutAnalysisStatusBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldBoundaryRoundTrip(t, &value, "AnalysisStatus", "analysisStatus")
}

func TestMailMessageOutAnalysisStatusRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldRejectsWrongShape(t, &value, "AnalysisStatus", "analysisStatus")
}

func TestMailMessageOutAnalysisStatusMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldMissingPreservesValue(t, &value, "AnalysisStatus")
}

func TestMailMessageOutAnalysisStatusNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldNullSemantics(t, &value, "AnalysisStatus", "analysisStatus")
}

func TestMailMessageOutAnalysisQualityBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldBoundaryRoundTrip(t, &value, "AnalysisQuality", "analysisQuality")
}

func TestMailMessageOutAnalysisQualityRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldRejectsWrongShape(t, &value, "AnalysisQuality", "analysisQuality")
}

func TestMailMessageOutAnalysisQualityMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldMissingPreservesValue(t, &value, "AnalysisQuality")
}

func TestMailMessageOutAnalysisQualityNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldNullSemantics(t, &value, "AnalysisQuality", "analysisQuality")
}

func TestMailMessageOutFeedStatusBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldBoundaryRoundTrip(t, &value, "FeedStatus", "feedStatus")
}

func TestMailMessageOutFeedStatusRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldRejectsWrongShape(t, &value, "FeedStatus", "feedStatus")
}

func TestMailMessageOutFeedStatusMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldMissingPreservesValue(t, &value, "FeedStatus")
}

func TestMailMessageOutFeedStatusNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldNullSemantics(t, &value, "FeedStatus", "feedStatus")
}

func TestMailMessageOutCalendarProposalCountBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldBoundaryRoundTrip(t, &value, "CalendarProposalCount", "calendarProposalCount")
}

func TestMailMessageOutCalendarProposalCountRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldRejectsWrongShape(t, &value, "CalendarProposalCount", "calendarProposalCount")
}

func TestMailMessageOutCalendarProposalCountMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldMissingPreservesValue(t, &value, "CalendarProposalCount")
}

func TestMailMessageOutCalendarProposalCountNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldNullSemantics(t, &value, "CalendarProposalCount", "calendarProposalCount")
}

func TestMailMessageOutTodoCountBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldBoundaryRoundTrip(t, &value, "TodoCount", "todoCount")
}

func TestMailMessageOutTodoCountRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldRejectsWrongShape(t, &value, "TodoCount", "todoCount")
}

func TestMailMessageOutTodoCountMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldMissingPreservesValue(t, &value, "TodoCount")
}

func TestMailMessageOutTodoCountNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldNullSemantics(t, &value, "TodoCount", "todoCount")
}

func TestMailMessageOutWorkStateHintBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldBoundaryRoundTrip(t, &value, "WorkStateHint", "workStateHint")
}

func TestMailMessageOutWorkStateHintRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldRejectsWrongShape(t, &value, "WorkStateHint", "workStateHint")
}

func TestMailMessageOutWorkStateHintMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldMissingPreservesValue(t, &value, "WorkStateHint")
}

func TestMailMessageOutWorkStateHintNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldNullSemantics(t, &value, "WorkStateHint", "workStateHint")
}

func TestMailMessageOutRelatedProjectsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldBoundaryRoundTrip(t, &value, "RelatedProjects", "relatedProjects")
}

func TestMailMessageOutRelatedProjectsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldRejectsWrongShape(t, &value, "RelatedProjects", "relatedProjects")
}

func TestMailMessageOutRelatedProjectsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldMissingPreservesValue(t, &value, "RelatedProjects")
}

func TestMailMessageOutRelatedProjectsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldNullSemantics(t, &value, "RelatedProjects", "relatedProjects")
}

func TestMailNativeStatusOutSourceBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Source", "source")
}

func TestMailNativeStatusOutSourceRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldRejectsWrongShape(t, &value, "Source", "source")
}

func TestMailNativeStatusOutSourceMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldMissingPreservesValue(t, &value, "Source")
}

func TestMailNativeStatusOutSourceNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldNullSemantics(t, &value, "Source", "source")
}

func TestMailNativeStatusOutAvailableBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Available", "available")
}

func TestMailNativeStatusOutAvailableRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldRejectsWrongShape(t, &value, "Available", "available")
}

func TestMailNativeStatusOutAvailableMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldMissingPreservesValue(t, &value, "Available")
}

func TestMailNativeStatusOutAvailableNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldNullSemantics(t, &value, "Available", "available")
}

func TestMailNativeStatusOutOfflineCapableBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldBoundaryRoundTrip(t, &value, "OfflineCapable", "offlineCapable")
}

func TestMailNativeStatusOutOfflineCapableRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldRejectsWrongShape(t, &value, "OfflineCapable", "offlineCapable")
}

func TestMailNativeStatusOutOfflineCapableMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldMissingPreservesValue(t, &value, "OfflineCapable")
}

func TestMailNativeStatusOutOfflineCapableNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldNullSemantics(t, &value, "OfflineCapable", "offlineCapable")
}

func TestMailNativeStatusOutMailboxesBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Mailboxes", "mailboxes")
}

func TestMailNativeStatusOutMailboxesRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldRejectsWrongShape(t, &value, "Mailboxes", "mailboxes")
}

func TestMailNativeStatusOutMailboxesMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldMissingPreservesValue(t, &value, "Mailboxes")
}

func TestMailNativeStatusOutMailboxesNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldNullSemantics(t, &value, "Mailboxes", "mailboxes")
}

func TestMailNativeStatusOutOverlayBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Overlay", "overlay")
}

func TestMailNativeStatusOutOverlayRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldRejectsWrongShape(t, &value, "Overlay", "overlay")
}

func TestMailNativeStatusOutOverlayMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldMissingPreservesValue(t, &value, "Overlay")
}

func TestMailNativeStatusOutOverlayNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldNullSemantics(t, &value, "Overlay", "overlay")
}

func TestMailNativeStatusOutPipelineBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Pipeline", "pipeline")
}

func TestMailNativeStatusOutPipelineRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldRejectsWrongShape(t, &value, "Pipeline", "pipeline")
}

func TestMailNativeStatusOutPipelineMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldMissingPreservesValue(t, &value, "Pipeline")
}

func TestMailNativeStatusOutPipelineNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldNullSemantics(t, &value, "Pipeline", "pipeline")
}

func TestMailNativeStatusOutGeneratedAtBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldBoundaryRoundTrip(t, &value, "GeneratedAt", "generatedAt")
}

func TestMailNativeStatusOutGeneratedAtRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldRejectsWrongShape(t, &value, "GeneratedAt", "generatedAt")
}

func TestMailNativeStatusOutGeneratedAtMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldMissingPreservesValue(t, &value, "GeneratedAt")
}

func TestMailNativeStatusOutGeneratedAtNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldNullSemantics(t, &value, "GeneratedAt", "generatedAt")
}

func TestMailNativeStatusOutErrorBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Error", "error")
}

func TestMailNativeStatusOutErrorRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldRejectsWrongShape(t, &value, "Error", "error")
}

func TestMailNativeStatusOutErrorMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldMissingPreservesValue(t, &value, "Error")
}

func TestMailNativeStatusOutErrorNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldNullSemantics(t, &value, "Error", "error")
}

func TestProjectRefPathBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ProjectRef
	assertWireFieldBoundaryRoundTrip(t, &value, "Path", "path")
}

func TestProjectRefPathRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ProjectRef
	assertWireFieldRejectsWrongShape(t, &value, "Path", "path")
}

func TestProjectRefPathMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ProjectRef
	assertWireFieldMissingPreservesValue(t, &value, "Path")
}

func TestProjectRefPathNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ProjectRef
	assertWireFieldNullSemantics(t, &value, "Path", "path")
}

func TestProjectRefTitleBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ProjectRef
	assertWireFieldBoundaryRoundTrip(t, &value, "Title", "title")
}

func TestProjectRefTitleRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ProjectRef
	assertWireFieldRejectsWrongShape(t, &value, "Title", "title")
}

func TestProjectRefTitleMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ProjectRef
	assertWireFieldMissingPreservesValue(t, &value, "Title")
}

func TestProjectRefTitleNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ProjectRef
	assertWireFieldNullSemantics(t, &value, "Title", "title")
}

func TestProjectRefSummaryBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ProjectRef
	assertWireFieldBoundaryRoundTrip(t, &value, "Summary", "summary")
}

func TestProjectRefSummaryRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ProjectRef
	assertWireFieldRejectsWrongShape(t, &value, "Summary", "summary")
}

func TestProjectRefSummaryMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ProjectRef
	assertWireFieldMissingPreservesValue(t, &value, "Summary")
}

func TestProjectRefSummaryNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ProjectRef
	assertWireFieldNullSemantics(t, &value, "Summary", "summary")
}

func TestMailAnalysisOutIDBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldBoundaryRoundTrip(t, &value, "ID", "id")
}

func TestMailAnalysisOutIDRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldRejectsWrongShape(t, &value, "ID", "id")
}

func TestMailAnalysisOutIDMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldMissingPreservesValue(t, &value, "ID")
}

func TestMailAnalysisOutIDNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldNullSemantics(t, &value, "ID", "id")
}

func TestMailAnalysisOutSubjectBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Subject", "subject")
}

func TestMailAnalysisOutSubjectRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldRejectsWrongShape(t, &value, "Subject", "subject")
}

func TestMailAnalysisOutSubjectMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldMissingPreservesValue(t, &value, "Subject")
}

func TestMailAnalysisOutSubjectNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldNullSemantics(t, &value, "Subject", "subject")
}

func TestMailAnalysisOutFromBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldBoundaryRoundTrip(t, &value, "From", "from")
}

func TestMailAnalysisOutFromRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldRejectsWrongShape(t, &value, "From", "from")
}

func TestMailAnalysisOutFromMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldMissingPreservesValue(t, &value, "From")
}

func TestMailAnalysisOutFromNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldNullSemantics(t, &value, "From", "from")
}

func TestMailAnalysisOutDateBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Date", "date")
}

func TestMailAnalysisOutDateRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldRejectsWrongShape(t, &value, "Date", "date")
}

func TestMailAnalysisOutDateMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldMissingPreservesValue(t, &value, "Date")
}

func TestMailAnalysisOutDateNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldNullSemantics(t, &value, "Date", "date")
}

func TestMailAnalysisOutAnalysisBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Analysis", "analysis")
}

func TestMailAnalysisOutAnalysisRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldRejectsWrongShape(t, &value, "Analysis", "analysis")
}

func TestMailAnalysisOutAnalysisMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldMissingPreservesValue(t, &value, "Analysis")
}

func TestMailAnalysisOutAnalysisNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldNullSemantics(t, &value, "Analysis", "analysis")
}

func TestMailAnalysisOutRelatedProjectsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldBoundaryRoundTrip(t, &value, "RelatedProjects", "relatedProjects")
}

func TestMailAnalysisOutRelatedProjectsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldRejectsWrongShape(t, &value, "RelatedProjects", "relatedProjects")
}

func TestMailAnalysisOutRelatedProjectsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldMissingPreservesValue(t, &value, "RelatedProjects")
}

func TestMailAnalysisOutRelatedProjectsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldNullSemantics(t, &value, "RelatedProjects", "relatedProjects")
}

func TestMailAnalysisOutDurationMsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldBoundaryRoundTrip(t, &value, "DurationMs", "durationMs")
}

func TestMailAnalysisOutDurationMsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldRejectsWrongShape(t, &value, "DurationMs", "durationMs")
}

func TestMailAnalysisOutDurationMsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldMissingPreservesValue(t, &value, "DurationMs")
}

func TestMailAnalysisOutDurationMsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldNullSemantics(t, &value, "DurationMs", "durationMs")
}

func TestMailAnalysisOutCachedBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Cached", "cached")
}

func TestMailAnalysisOutCachedRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldRejectsWrongShape(t, &value, "Cached", "cached")
}

func TestMailAnalysisOutCachedMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldMissingPreservesValue(t, &value, "Cached")
}

func TestMailAnalysisOutCachedNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldNullSemantics(t, &value, "Cached", "cached")
}

func TestMailAnalysisOutCreatedAtBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldBoundaryRoundTrip(t, &value, "CreatedAt", "createdAt")
}

func TestMailAnalysisOutCreatedAtRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldRejectsWrongShape(t, &value, "CreatedAt", "createdAt")
}

func TestMailAnalysisOutCreatedAtMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldMissingPreservesValue(t, &value, "CreatedAt")
}

func TestMailAnalysisOutCreatedAtNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldNullSemantics(t, &value, "CreatedAt", "createdAt")
}

func TestMailAnalysisOutAnalysisStatusBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldBoundaryRoundTrip(t, &value, "AnalysisStatus", "analysisStatus")
}

func TestMailAnalysisOutAnalysisStatusRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldRejectsWrongShape(t, &value, "AnalysisStatus", "analysisStatus")
}

func TestMailAnalysisOutAnalysisStatusMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldMissingPreservesValue(t, &value, "AnalysisStatus")
}

func TestMailAnalysisOutAnalysisStatusNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldNullSemantics(t, &value, "AnalysisStatus", "analysisStatus")
}

func TestMailAnalysisOutAnalysisQualityBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldBoundaryRoundTrip(t, &value, "AnalysisQuality", "analysisQuality")
}

func TestMailAnalysisOutAnalysisQualityRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldRejectsWrongShape(t, &value, "AnalysisQuality", "analysisQuality")
}

func TestMailAnalysisOutAnalysisQualityMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldMissingPreservesValue(t, &value, "AnalysisQuality")
}

func TestMailAnalysisOutAnalysisQualityNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldNullSemantics(t, &value, "AnalysisQuality", "analysisQuality")
}

func TestMailAnalysisOutFeedStatusBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldBoundaryRoundTrip(t, &value, "FeedStatus", "feedStatus")
}

func TestMailAnalysisOutFeedStatusRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldRejectsWrongShape(t, &value, "FeedStatus", "feedStatus")
}

func TestMailAnalysisOutFeedStatusMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldMissingPreservesValue(t, &value, "FeedStatus")
}

func TestMailAnalysisOutFeedStatusNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldNullSemantics(t, &value, "FeedStatus", "feedStatus")
}

func TestMailAnalysisOutCalendarProposalCountBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldBoundaryRoundTrip(t, &value, "CalendarProposalCount", "calendarProposalCount")
}

func TestMailAnalysisOutCalendarProposalCountRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldRejectsWrongShape(t, &value, "CalendarProposalCount", "calendarProposalCount")
}

func TestMailAnalysisOutCalendarProposalCountMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldMissingPreservesValue(t, &value, "CalendarProposalCount")
}

func TestMailAnalysisOutCalendarProposalCountNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldNullSemantics(t, &value, "CalendarProposalCount", "calendarProposalCount")
}

func TestMailAnalysisOutTodoCountBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldBoundaryRoundTrip(t, &value, "TodoCount", "todoCount")
}

func TestMailAnalysisOutTodoCountRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldRejectsWrongShape(t, &value, "TodoCount", "todoCount")
}

func TestMailAnalysisOutTodoCountMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldMissingPreservesValue(t, &value, "TodoCount")
}

func TestMailAnalysisOutTodoCountNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldNullSemantics(t, &value, "TodoCount", "todoCount")
}

func TestMailAnalysisOutWorkStateHintBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldBoundaryRoundTrip(t, &value, "WorkStateHint", "workStateHint")
}

func TestMailAnalysisOutWorkStateHintRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldRejectsWrongShape(t, &value, "WorkStateHint", "workStateHint")
}

func TestMailAnalysisOutWorkStateHintMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldMissingPreservesValue(t, &value, "WorkStateHint")
}

func TestMailAnalysisOutWorkStateHintNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldNullSemantics(t, &value, "WorkStateHint", "workStateHint")
}

func TestSenderWikiHitOutPathBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SenderWikiHitOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Path", "path")
}

func TestSenderWikiHitOutPathRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SenderWikiHitOut
	assertWireFieldRejectsWrongShape(t, &value, "Path", "path")
}

func TestSenderWikiHitOutPathMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SenderWikiHitOut
	assertWireFieldMissingPreservesValue(t, &value, "Path")
}

func TestSenderWikiHitOutPathNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SenderWikiHitOut
	assertWireFieldNullSemantics(t, &value, "Path", "path")
}

func TestSenderWikiHitOutTitleBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SenderWikiHitOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Title", "title")
}

func TestSenderWikiHitOutTitleRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SenderWikiHitOut
	assertWireFieldRejectsWrongShape(t, &value, "Title", "title")
}

func TestSenderWikiHitOutTitleMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SenderWikiHitOut
	assertWireFieldMissingPreservesValue(t, &value, "Title")
}

func TestSenderWikiHitOutTitleNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SenderWikiHitOut
	assertWireFieldNullSemantics(t, &value, "Title", "title")
}

func TestSenderWikiHitOutSummaryBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SenderWikiHitOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Summary", "summary")
}

func TestSenderWikiHitOutSummaryRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SenderWikiHitOut
	assertWireFieldRejectsWrongShape(t, &value, "Summary", "summary")
}

func TestSenderWikiHitOutSummaryMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SenderWikiHitOut
	assertWireFieldMissingPreservesValue(t, &value, "Summary")
}

func TestSenderWikiHitOutSummaryNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SenderWikiHitOut
	assertWireFieldNullSemantics(t, &value, "Summary", "summary")
}

func TestSenderWikiHitOutCategoryBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SenderWikiHitOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Category", "category")
}

func TestSenderWikiHitOutCategoryRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SenderWikiHitOut
	assertWireFieldRejectsWrongShape(t, &value, "Category", "category")
}

func TestSenderWikiHitOutCategoryMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SenderWikiHitOut
	assertWireFieldMissingPreservesValue(t, &value, "Category")
}

func TestSenderWikiHitOutCategoryNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SenderWikiHitOut
	assertWireFieldNullSemantics(t, &value, "Category", "category")
}

func TestSenderRecentOutCountBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SenderRecentOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Count", "count")
}

func TestSenderRecentOutCountRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SenderRecentOut
	assertWireFieldRejectsWrongShape(t, &value, "Count", "count")
}

func TestSenderRecentOutCountMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SenderRecentOut
	assertWireFieldMissingPreservesValue(t, &value, "Count")
}

func TestSenderRecentOutCountNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SenderRecentOut
	assertWireFieldNullSemantics(t, &value, "Count", "count")
}

func TestSenderRecentOutLastReceivedAtBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SenderRecentOut
	assertWireFieldBoundaryRoundTrip(t, &value, "LastReceivedAt", "lastReceivedAt")
}

func TestSenderRecentOutLastReceivedAtRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SenderRecentOut
	assertWireFieldRejectsWrongShape(t, &value, "LastReceivedAt", "lastReceivedAt")
}

func TestSenderRecentOutLastReceivedAtMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SenderRecentOut
	assertWireFieldMissingPreservesValue(t, &value, "LastReceivedAt")
}

func TestSenderRecentOutLastReceivedAtNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SenderRecentOut
	assertWireFieldNullSemantics(t, &value, "LastReceivedAt", "lastReceivedAt")
}

func TestSenderRecentOutWindowDaysBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SenderRecentOut
	assertWireFieldBoundaryRoundTrip(t, &value, "WindowDays", "windowDays")
}

func TestSenderRecentOutWindowDaysRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SenderRecentOut
	assertWireFieldRejectsWrongShape(t, &value, "WindowDays", "windowDays")
}

func TestSenderRecentOutWindowDaysMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SenderRecentOut
	assertWireFieldMissingPreservesValue(t, &value, "WindowDays")
}

func TestSenderRecentOutWindowDaysNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SenderRecentOut
	assertWireFieldNullSemantics(t, &value, "WindowDays", "windowDays")
}

func TestSenderRecentOutTruncatedBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SenderRecentOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Truncated", "truncated")
}

func TestSenderRecentOutTruncatedRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SenderRecentOut
	assertWireFieldRejectsWrongShape(t, &value, "Truncated", "truncated")
}

func TestSenderRecentOutTruncatedMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SenderRecentOut
	assertWireFieldMissingPreservesValue(t, &value, "Truncated")
}

func TestSenderRecentOutTruncatedNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SenderRecentOut
	assertWireFieldNullSemantics(t, &value, "Truncated", "truncated")
}

func TestQATurnQBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value QATurn
	assertWireFieldBoundaryRoundTrip(t, &value, "Q", "q")
}

func TestQATurnQRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value QATurn
	assertWireFieldRejectsWrongShape(t, &value, "Q", "q")
}

func TestQATurnQMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value QATurn
	assertWireFieldMissingPreservesValue(t, &value, "Q")
}

func TestQATurnQNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value QATurn
	assertWireFieldNullSemantics(t, &value, "Q", "q")
}

func TestQATurnABoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value QATurn
	assertWireFieldBoundaryRoundTrip(t, &value, "A", "a")
}

func TestQATurnARejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value QATurn
	assertWireFieldRejectsWrongShape(t, &value, "A", "a")
}

func TestQATurnAMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value QATurn
	assertWireFieldMissingPreservesValue(t, &value, "A")
}

func TestQATurnANullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value QATurn
	assertWireFieldNullSemantics(t, &value, "A", "a")
}

func TestMarketQuoteSymbolBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MarketQuote
	assertWireFieldBoundaryRoundTrip(t, &value, "Symbol", "symbol")
}

func TestMarketQuoteSymbolRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MarketQuote
	assertWireFieldRejectsWrongShape(t, &value, "Symbol", "symbol")
}

func TestMarketQuoteSymbolMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MarketQuote
	assertWireFieldMissingPreservesValue(t, &value, "Symbol")
}

func TestMarketQuoteSymbolNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MarketQuote
	assertWireFieldNullSemantics(t, &value, "Symbol", "symbol")
}

func TestMarketQuoteLabelBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MarketQuote
	assertWireFieldBoundaryRoundTrip(t, &value, "Label", "label")
}

func TestMarketQuoteLabelRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MarketQuote
	assertWireFieldRejectsWrongShape(t, &value, "Label", "label")
}

func TestMarketQuoteLabelMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MarketQuote
	assertWireFieldMissingPreservesValue(t, &value, "Label")
}

func TestMarketQuoteLabelNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MarketQuote
	assertWireFieldNullSemantics(t, &value, "Label", "label")
}

func TestMarketQuotePriceBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MarketQuote
	assertWireFieldBoundaryRoundTrip(t, &value, "Price", "price")
}

func TestMarketQuotePriceRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MarketQuote
	assertWireFieldRejectsWrongShape(t, &value, "Price", "price")
}

func TestMarketQuotePriceMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MarketQuote
	assertWireFieldMissingPreservesValue(t, &value, "Price")
}

func TestMarketQuotePriceNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MarketQuote
	assertWireFieldNullSemantics(t, &value, "Price", "price")
}

func TestMarketQuotePrevCloseBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MarketQuote
	assertWireFieldBoundaryRoundTrip(t, &value, "PrevClose", "prevClose")
}

func TestMarketQuotePrevCloseRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MarketQuote
	assertWireFieldRejectsWrongShape(t, &value, "PrevClose", "prevClose")
}

func TestMarketQuotePrevCloseMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MarketQuote
	assertWireFieldMissingPreservesValue(t, &value, "PrevClose")
}

func TestMarketQuotePrevCloseNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MarketQuote
	assertWireFieldNullSemantics(t, &value, "PrevClose", "prevClose")
}

func TestMarketQuoteChangePctBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MarketQuote
	assertWireFieldBoundaryRoundTrip(t, &value, "ChangePct", "changePct")
}

func TestMarketQuoteChangePctRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MarketQuote
	assertWireFieldRejectsWrongShape(t, &value, "ChangePct", "changePct")
}

func TestMarketQuoteChangePctMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MarketQuote
	assertWireFieldMissingPreservesValue(t, &value, "ChangePct")
}

func TestMarketQuoteChangePctNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MarketQuote
	assertWireFieldNullSemantics(t, &value, "ChangePct", "changePct")
}

func TestMarketQuoteCurrencyBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MarketQuote
	assertWireFieldBoundaryRoundTrip(t, &value, "Currency", "currency")
}

func TestMarketQuoteCurrencyRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MarketQuote
	assertWireFieldRejectsWrongShape(t, &value, "Currency", "currency")
}

func TestMarketQuoteCurrencyMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MarketQuote
	assertWireFieldMissingPreservesValue(t, &value, "Currency")
}

func TestMarketQuoteCurrencyNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MarketQuote
	assertWireFieldNullSemantics(t, &value, "Currency", "currency")
}

func TestMarketSummaryQuotesBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MarketSummary
	assertWireFieldBoundaryRoundTrip(t, &value, "Quotes", "quotes")
}

func TestMarketSummaryQuotesRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MarketSummary
	assertWireFieldRejectsWrongShape(t, &value, "Quotes", "quotes")
}

func TestMarketSummaryQuotesMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MarketSummary
	assertWireFieldMissingPreservesValue(t, &value, "Quotes")
}

func TestMarketSummaryQuotesNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MarketSummary
	assertWireFieldNullSemantics(t, &value, "Quotes", "quotes")
}

func TestMarketSummaryAsOfBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MarketSummary
	assertWireFieldBoundaryRoundTrip(t, &value, "AsOf", "asOf")
}

func TestMarketSummaryAsOfRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MarketSummary
	assertWireFieldRejectsWrongShape(t, &value, "AsOf", "asOf")
}

func TestMarketSummaryAsOfMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MarketSummary
	assertWireFieldMissingPreservesValue(t, &value, "AsOf")
}

func TestMarketSummaryAsOfNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MarketSummary
	assertWireFieldNullSemantics(t, &value, "AsOf", "asOf")
}

func TestMarketSummaryStaleBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MarketSummary
	assertWireFieldBoundaryRoundTrip(t, &value, "Stale", "stale")
}

func TestMarketSummaryStaleRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MarketSummary
	assertWireFieldRejectsWrongShape(t, &value, "Stale", "stale")
}

func TestMarketSummaryStaleMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MarketSummary
	assertWireFieldMissingPreservesValue(t, &value, "Stale")
}

func TestMarketSummaryStaleNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MarketSummary
	assertWireFieldNullSemantics(t, &value, "Stale", "stale")
}

func TestModelOptionIDBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldBoundaryRoundTrip(t, &value, "ID", "id")
}

func TestModelOptionIDRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldRejectsWrongShape(t, &value, "ID", "id")
}

func TestModelOptionIDMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldMissingPreservesValue(t, &value, "ID")
}

func TestModelOptionIDNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldNullSemantics(t, &value, "ID", "id")
}

func TestModelOptionLabelBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldBoundaryRoundTrip(t, &value, "Label", "label")
}

func TestModelOptionLabelRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldRejectsWrongShape(t, &value, "Label", "label")
}

func TestModelOptionLabelMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldMissingPreservesValue(t, &value, "Label")
}

func TestModelOptionLabelNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldNullSemantics(t, &value, "Label", "label")
}

func TestModelOptionProviderBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldBoundaryRoundTrip(t, &value, "Provider", "provider")
}

func TestModelOptionProviderRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldRejectsWrongShape(t, &value, "Provider", "provider")
}

func TestModelOptionProviderMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldMissingPreservesValue(t, &value, "Provider")
}

func TestModelOptionProviderNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldNullSemantics(t, &value, "Provider", "provider")
}

func TestModelOptionDisplayBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldBoundaryRoundTrip(t, &value, "Display", "display")
}

func TestModelOptionDisplayRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldRejectsWrongShape(t, &value, "Display", "display")
}

func TestModelOptionDisplayMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldMissingPreservesValue(t, &value, "Display")
}

func TestModelOptionDisplayNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldNullSemantics(t, &value, "Display", "display")
}

func TestModelOptionHealthBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldBoundaryRoundTrip(t, &value, "Health", "health")
}

func TestModelOptionHealthRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldRejectsWrongShape(t, &value, "Health", "health")
}

func TestModelOptionHealthMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldMissingPreservesValue(t, &value, "Health")
}

func TestModelOptionHealthNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldNullSemantics(t, &value, "Health", "health")
}

func TestModelOptionCurrentBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldBoundaryRoundTrip(t, &value, "Current", "current")
}

func TestModelOptionCurrentRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldRejectsWrongShape(t, &value, "Current", "current")
}

func TestModelOptionCurrentMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldMissingPreservesValue(t, &value, "Current")
}

func TestModelOptionCurrentNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldNullSemantics(t, &value, "Current", "current")
}

func TestModelOptionCustomBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldBoundaryRoundTrip(t, &value, "Custom", "custom")
}

func TestModelOptionCustomRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldRejectsWrongShape(t, &value, "Custom", "custom")
}

func TestModelOptionCustomMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldMissingPreservesValue(t, &value, "Custom")
}

func TestModelOptionCustomNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldNullSemantics(t, &value, "Custom", "custom")
}

func TestModelOptionDeletableBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldBoundaryRoundTrip(t, &value, "Deletable", "deletable")
}

func TestModelOptionDeletableRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldRejectsWrongShape(t, &value, "Deletable", "deletable")
}

func TestModelOptionDeletableMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldMissingPreservesValue(t, &value, "Deletable")
}

func TestModelOptionDeletableNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldNullSemantics(t, &value, "Deletable", "deletable")
}

func TestModelOptionUnhealthyBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldBoundaryRoundTrip(t, &value, "Unhealthy", "unhealthy")
}

func TestModelOptionUnhealthyRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldRejectsWrongShape(t, &value, "Unhealthy", "unhealthy")
}

func TestModelOptionUnhealthyMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldMissingPreservesValue(t, &value, "Unhealthy")
}

func TestModelOptionUnhealthyNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldNullSemantics(t, &value, "Unhealthy", "unhealthy")
}

func TestModelOptionNoteBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldBoundaryRoundTrip(t, &value, "Note", "note")
}

func TestModelOptionNoteRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldRejectsWrongShape(t, &value, "Note", "note")
}

func TestModelOptionNoteMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldMissingPreservesValue(t, &value, "Note")
}

func TestModelOptionNoteNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldNullSemantics(t, &value, "Note", "note")
}

func TestModelSectionTitleBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ModelSection
	assertWireFieldBoundaryRoundTrip(t, &value, "Title", "title")
}

func TestModelSectionTitleRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ModelSection
	assertWireFieldRejectsWrongShape(t, &value, "Title", "title")
}

func TestModelSectionTitleMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ModelSection
	assertWireFieldMissingPreservesValue(t, &value, "Title")
}

func TestModelSectionTitleNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ModelSection
	assertWireFieldNullSemantics(t, &value, "Title", "title")
}

func TestModelSectionModelsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ModelSection
	assertWireFieldBoundaryRoundTrip(t, &value, "Models", "models")
}

func TestModelSectionModelsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ModelSection
	assertWireFieldRejectsWrongShape(t, &value, "Models", "models")
}

func TestModelSectionModelsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ModelSection
	assertWireFieldMissingPreservesValue(t, &value, "Models")
}

func TestModelSectionModelsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ModelSection
	assertWireFieldNullSemantics(t, &value, "Models", "models")
}

func TestModelAddResultOKBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ModelAddResult
	assertWireFieldBoundaryRoundTrip(t, &value, "OK", "ok")
}

func TestModelAddResultOKRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ModelAddResult
	assertWireFieldRejectsWrongShape(t, &value, "OK", "ok")
}

func TestModelAddResultOKMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ModelAddResult
	assertWireFieldMissingPreservesValue(t, &value, "OK")
}

func TestModelAddResultOKNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ModelAddResult
	assertWireFieldNullSemantics(t, &value, "OK", "ok")
}

func TestModelAddResultIDBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ModelAddResult
	assertWireFieldBoundaryRoundTrip(t, &value, "ID", "id")
}

func TestModelAddResultIDRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ModelAddResult
	assertWireFieldRejectsWrongShape(t, &value, "ID", "id")
}

func TestModelAddResultIDMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ModelAddResult
	assertWireFieldMissingPreservesValue(t, &value, "ID")
}

func TestModelAddResultIDNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ModelAddResult
	assertWireFieldNullSemantics(t, &value, "ID", "id")
}

func TestModelAddResultProviderBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ModelAddResult
	assertWireFieldBoundaryRoundTrip(t, &value, "Provider", "provider")
}

func TestModelAddResultProviderRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ModelAddResult
	assertWireFieldRejectsWrongShape(t, &value, "Provider", "provider")
}

func TestModelAddResultProviderMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ModelAddResult
	assertWireFieldMissingPreservesValue(t, &value, "Provider")
}

func TestModelAddResultProviderNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ModelAddResult
	assertWireFieldNullSemantics(t, &value, "Provider", "provider")
}

func TestModelAddResultEndpointBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ModelAddResult
	assertWireFieldBoundaryRoundTrip(t, &value, "Endpoint", "endpoint")
}

func TestModelAddResultEndpointRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ModelAddResult
	assertWireFieldRejectsWrongShape(t, &value, "Endpoint", "endpoint")
}

func TestModelAddResultEndpointMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ModelAddResult
	assertWireFieldMissingPreservesValue(t, &value, "Endpoint")
}

func TestModelAddResultEndpointNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ModelAddResult
	assertWireFieldNullSemantics(t, &value, "Endpoint", "endpoint")
}

func TestModelAddResultModelBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ModelAddResult
	assertWireFieldBoundaryRoundTrip(t, &value, "Model", "model")
}

func TestModelAddResultModelRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ModelAddResult
	assertWireFieldRejectsWrongShape(t, &value, "Model", "model")
}

func TestModelAddResultModelMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ModelAddResult
	assertWireFieldMissingPreservesValue(t, &value, "Model")
}

func TestModelAddResultModelNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ModelAddResult
	assertWireFieldNullSemantics(t, &value, "Model", "model")
}

func TestModelAddResultAddedBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ModelAddResult
	assertWireFieldBoundaryRoundTrip(t, &value, "Added", "added")
}

func TestModelAddResultAddedRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ModelAddResult
	assertWireFieldRejectsWrongShape(t, &value, "Added", "added")
}

func TestModelAddResultAddedMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ModelAddResult
	assertWireFieldMissingPreservesValue(t, &value, "Added")
}

func TestModelAddResultAddedNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ModelAddResult
	assertWireFieldNullSemantics(t, &value, "Added", "added")
}

func TestModelDeleteResultOKBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ModelDeleteResult
	assertWireFieldBoundaryRoundTrip(t, &value, "OK", "ok")
}

func TestModelDeleteResultOKRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ModelDeleteResult
	assertWireFieldRejectsWrongShape(t, &value, "OK", "ok")
}

func TestModelDeleteResultOKMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ModelDeleteResult
	assertWireFieldMissingPreservesValue(t, &value, "OK")
}

func TestModelDeleteResultOKNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ModelDeleteResult
	assertWireFieldNullSemantics(t, &value, "OK", "ok")
}

func TestModelDeleteResultIDBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ModelDeleteResult
	assertWireFieldBoundaryRoundTrip(t, &value, "ID", "id")
}

func TestModelDeleteResultIDRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ModelDeleteResult
	assertWireFieldRejectsWrongShape(t, &value, "ID", "id")
}

func TestModelDeleteResultIDMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ModelDeleteResult
	assertWireFieldMissingPreservesValue(t, &value, "ID")
}

func TestModelDeleteResultIDNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ModelDeleteResult
	assertWireFieldNullSemantics(t, &value, "ID", "id")
}

func TestModelDeleteResultRemovedBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ModelDeleteResult
	assertWireFieldBoundaryRoundTrip(t, &value, "Removed", "removed")
}

func TestModelDeleteResultRemovedRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ModelDeleteResult
	assertWireFieldRejectsWrongShape(t, &value, "Removed", "removed")
}

func TestModelDeleteResultRemovedMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ModelDeleteResult
	assertWireFieldMissingPreservesValue(t, &value, "Removed")
}

func TestModelDeleteResultRemovedNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ModelDeleteResult
	assertWireFieldNullSemantics(t, &value, "Removed", "removed")
}

func TestModelDeleteResultClearedRolesBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ModelDeleteResult
	assertWireFieldBoundaryRoundTrip(t, &value, "ClearedRoles", "clearedRoles")
}

func TestModelDeleteResultClearedRolesRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ModelDeleteResult
	assertWireFieldRejectsWrongShape(t, &value, "ClearedRoles", "clearedRoles")
}

func TestModelDeleteResultClearedRolesMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ModelDeleteResult
	assertWireFieldMissingPreservesValue(t, &value, "ClearedRoles")
}

func TestModelDeleteResultClearedRolesNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ModelDeleteResult
	assertWireFieldNullSemantics(t, &value, "ClearedRoles", "clearedRoles")
}

func TestModelDeleteResultCurrentBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ModelDeleteResult
	assertWireFieldBoundaryRoundTrip(t, &value, "Current", "current")
}

func TestModelDeleteResultCurrentRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ModelDeleteResult
	assertWireFieldRejectsWrongShape(t, &value, "Current", "current")
}

func TestModelDeleteResultCurrentMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ModelDeleteResult
	assertWireFieldMissingPreservesValue(t, &value, "Current")
}

func TestModelDeleteResultCurrentNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ModelDeleteResult
	assertWireFieldNullSemantics(t, &value, "Current", "current")
}

func TestRoleModelRoleBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value RoleModel
	assertWireFieldBoundaryRoundTrip(t, &value, "Role", "role")
}

func TestRoleModelRoleRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value RoleModel
	assertWireFieldRejectsWrongShape(t, &value, "Role", "role")
}

func TestRoleModelRoleMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value RoleModel
	assertWireFieldMissingPreservesValue(t, &value, "Role")
}

func TestRoleModelRoleNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value RoleModel
	assertWireFieldNullSemantics(t, &value, "Role", "role")
}

func TestRoleModelModelBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value RoleModel
	assertWireFieldBoundaryRoundTrip(t, &value, "Model", "model")
}

func TestRoleModelModelRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value RoleModel
	assertWireFieldRejectsWrongShape(t, &value, "Model", "model")
}

func TestRoleModelModelMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value RoleModel
	assertWireFieldMissingPreservesValue(t, &value, "Model")
}

func TestRoleModelModelNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value RoleModel
	assertWireFieldNullSemantics(t, &value, "Model", "model")
}

func TestModelsListResultCurrentBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ModelsListResult
	assertWireFieldBoundaryRoundTrip(t, &value, "Current", "current")
}

func TestModelsListResultCurrentRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ModelsListResult
	assertWireFieldRejectsWrongShape(t, &value, "Current", "current")
}

func TestModelsListResultCurrentMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ModelsListResult
	assertWireFieldMissingPreservesValue(t, &value, "Current")
}

func TestModelsListResultCurrentNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ModelsListResult
	assertWireFieldNullSemantics(t, &value, "Current", "current")
}

func TestModelsListResultRolesBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ModelsListResult
	assertWireFieldBoundaryRoundTrip(t, &value, "Roles", "roles")
}

func TestModelsListResultRolesRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ModelsListResult
	assertWireFieldRejectsWrongShape(t, &value, "Roles", "roles")
}

func TestModelsListResultRolesMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ModelsListResult
	assertWireFieldMissingPreservesValue(t, &value, "Roles")
}

func TestModelsListResultRolesNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ModelsListResult
	assertWireFieldNullSemantics(t, &value, "Roles", "roles")
}

func TestModelsListResultSectionsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ModelsListResult
	assertWireFieldBoundaryRoundTrip(t, &value, "Sections", "sections")
}

func TestModelsListResultSectionsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ModelsListResult
	assertWireFieldRejectsWrongShape(t, &value, "Sections", "sections")
}

func TestModelsListResultSectionsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ModelsListResult
	assertWireFieldMissingPreservesValue(t, &value, "Sections")
}

func TestModelsListResultSectionsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ModelsListResult
	assertWireFieldNullSemantics(t, &value, "Sections", "sections")
}

func TestModelsListResultAdvisoriesBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ModelsListResult
	assertWireFieldBoundaryRoundTrip(t, &value, "Advisories", "advisories")
}

func TestModelsListResultAdvisoriesRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ModelsListResult
	assertWireFieldRejectsWrongShape(t, &value, "Advisories", "advisories")
}

func TestModelsListResultAdvisoriesMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ModelsListResult
	assertWireFieldMissingPreservesValue(t, &value, "Advisories")
}

func TestModelsListResultAdvisoriesNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ModelsListResult
	assertWireFieldNullSemantics(t, &value, "Advisories", "advisories")
}

func TestModelsListResultMainHasVisionBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ModelsListResult
	assertWireFieldBoundaryRoundTrip(t, &value, "MainHasVision", "mainHasVision")
}

func TestModelsListResultMainHasVisionRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ModelsListResult
	assertWireFieldRejectsWrongShape(t, &value, "MainHasVision", "mainHasVision")
}

func TestModelsListResultMainHasVisionMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ModelsListResult
	assertWireFieldMissingPreservesValue(t, &value, "MainHasVision")
}

func TestModelsListResultMainHasVisionNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ModelsListResult
	assertWireFieldNullSemantics(t, &value, "MainHasVision", "mainHasVision")
}

func TestMemberOutNameBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MemberOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Name", "name")
}

func TestMemberOutNameRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MemberOut
	assertWireFieldRejectsWrongShape(t, &value, "Name", "name")
}

func TestMemberOutNameMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MemberOut
	assertWireFieldMissingPreservesValue(t, &value, "Name")
}

func TestMemberOutNameNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MemberOut
	assertWireFieldNullSemantics(t, &value, "Name", "name")
}

func TestMemberOutRankBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MemberOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Rank", "rank")
}

func TestMemberOutRankRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MemberOut
	assertWireFieldRejectsWrongShape(t, &value, "Rank", "rank")
}

func TestMemberOutRankMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MemberOut
	assertWireFieldMissingPreservesValue(t, &value, "Rank")
}

func TestMemberOutRankNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MemberOut
	assertWireFieldNullSemantics(t, &value, "Rank", "rank")
}

func TestMemberOutPositionBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MemberOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Position", "position")
}

func TestMemberOutPositionRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MemberOut
	assertWireFieldRejectsWrongShape(t, &value, "Position", "position")
}

func TestMemberOutPositionMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MemberOut
	assertWireFieldMissingPreservesValue(t, &value, "Position")
}

func TestMemberOutPositionNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MemberOut
	assertWireFieldNullSemantics(t, &value, "Position", "position")
}

func TestMemberOutPhonesBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MemberOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Phones", "phones")
}

func TestMemberOutPhonesRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MemberOut
	assertWireFieldRejectsWrongShape(t, &value, "Phones", "phones")
}

func TestMemberOutPhonesMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MemberOut
	assertWireFieldMissingPreservesValue(t, &value, "Phones")
}

func TestMemberOutPhonesNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MemberOut
	assertWireFieldNullSemantics(t, &value, "Phones", "phones")
}

func TestMemberOutEmailsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MemberOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Emails", "emails")
}

func TestMemberOutEmailsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MemberOut
	assertWireFieldRejectsWrongShape(t, &value, "Emails", "emails")
}

func TestMemberOutEmailsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MemberOut
	assertWireFieldMissingPreservesValue(t, &value, "Emails")
}

func TestMemberOutEmailsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MemberOut
	assertWireFieldNullSemantics(t, &value, "Emails", "emails")
}

func TestMemberOutPersonPathBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MemberOut
	assertWireFieldBoundaryRoundTrip(t, &value, "PersonPath", "personPath")
}

func TestMemberOutPersonPathRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MemberOut
	assertWireFieldRejectsWrongShape(t, &value, "PersonPath", "personPath")
}

func TestMemberOutPersonPathMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MemberOut
	assertWireFieldMissingPreservesValue(t, &value, "PersonPath")
}

func TestMemberOutPersonPathNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MemberOut
	assertWireFieldNullSemantics(t, &value, "PersonPath", "personPath")
}

func TestOrgNodeOutIDBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldBoundaryRoundTrip(t, &value, "ID", "id")
}

func TestOrgNodeOutIDRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldRejectsWrongShape(t, &value, "ID", "id")
}

func TestOrgNodeOutIDMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldMissingPreservesValue(t, &value, "ID")
}

func TestOrgNodeOutIDNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldNullSemantics(t, &value, "ID", "id")
}

func TestOrgNodeOutNameBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Name", "name")
}

func TestOrgNodeOutNameRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldRejectsWrongShape(t, &value, "Name", "name")
}

func TestOrgNodeOutNameMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldMissingPreservesValue(t, &value, "Name")
}

func TestOrgNodeOutNameNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldNullSemantics(t, &value, "Name", "name")
}

func TestOrgNodeOutTypeBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Type", "type")
}

func TestOrgNodeOutTypeRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldRejectsWrongShape(t, &value, "Type", "type")
}

func TestOrgNodeOutTypeMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldMissingPreservesValue(t, &value, "Type")
}

func TestOrgNodeOutTypeNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldNullSemantics(t, &value, "Type", "type")
}

func TestOrgNodeOutParentIDBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldBoundaryRoundTrip(t, &value, "ParentID", "parentId")
}

func TestOrgNodeOutParentIDRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldRejectsWrongShape(t, &value, "ParentID", "parentId")
}

func TestOrgNodeOutParentIDMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldMissingPreservesValue(t, &value, "ParentID")
}

func TestOrgNodeOutParentIDNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldNullSemantics(t, &value, "ParentID", "parentId")
}

func TestOrgNodeOutLaneBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Lane", "lane")
}

func TestOrgNodeOutLaneRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldRejectsWrongShape(t, &value, "Lane", "lane")
}

func TestOrgNodeOutLaneMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldMissingPreservesValue(t, &value, "Lane")
}

func TestOrgNodeOutLaneNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldNullSemantics(t, &value, "Lane", "lane")
}

func TestOrgNodeOutMembersBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Members", "members")
}

func TestOrgNodeOutMembersRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldRejectsWrongShape(t, &value, "Members", "members")
}

func TestOrgNodeOutMembersMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldMissingPreservesValue(t, &value, "Members")
}

func TestOrgNodeOutMembersNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldNullSemantics(t, &value, "Members", "members")
}

func TestOrgNodeOutKeywordsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Keywords", "keywords")
}

func TestOrgNodeOutKeywordsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldRejectsWrongShape(t, &value, "Keywords", "keywords")
}

func TestOrgNodeOutKeywordsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldMissingPreservesValue(t, &value, "Keywords")
}

func TestOrgNodeOutKeywordsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldNullSemantics(t, &value, "Keywords", "keywords")
}

func TestOrgNodeOutCompaniesBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Companies", "companies")
}

func TestOrgNodeOutCompaniesRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldRejectsWrongShape(t, &value, "Companies", "companies")
}

func TestOrgNodeOutCompaniesMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldMissingPreservesValue(t, &value, "Companies")
}

func TestOrgNodeOutCompaniesNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldNullSemantics(t, &value, "Companies", "companies")
}

func TestOrgTreeOutNodesBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value OrgTreeOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Nodes", "nodes")
}

func TestOrgTreeOutNodesRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value OrgTreeOut
	assertWireFieldRejectsWrongShape(t, &value, "Nodes", "nodes")
}

func TestOrgTreeOutNodesMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value OrgTreeOut
	assertWireFieldMissingPreservesValue(t, &value, "Nodes")
}

func TestOrgTreeOutNodesNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value OrgTreeOut
	assertWireFieldNullSemantics(t, &value, "Nodes", "nodes")
}

func TestOrgSaveOutSavedBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value OrgSaveOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Saved", "saved")
}

func TestOrgSaveOutSavedRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value OrgSaveOut
	assertWireFieldRejectsWrongShape(t, &value, "Saved", "saved")
}

func TestOrgSaveOutSavedMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value OrgSaveOut
	assertWireFieldMissingPreservesValue(t, &value, "Saved")
}

func TestOrgSaveOutSavedNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value OrgSaveOut
	assertWireFieldNullSemantics(t, &value, "Saved", "saved")
}

func TestOrgSaveOutNodeCountBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value OrgSaveOut
	assertWireFieldBoundaryRoundTrip(t, &value, "NodeCount", "nodeCount")
}

func TestOrgSaveOutNodeCountRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value OrgSaveOut
	assertWireFieldRejectsWrongShape(t, &value, "NodeCount", "nodeCount")
}

func TestOrgSaveOutNodeCountMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value OrgSaveOut
	assertWireFieldMissingPreservesValue(t, &value, "NodeCount")
}

func TestOrgSaveOutNodeCountNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value OrgSaveOut
	assertWireFieldNullSemantics(t, &value, "NodeCount", "nodeCount")
}

func TestOrgSaveOutHasLanesBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value OrgSaveOut
	assertWireFieldBoundaryRoundTrip(t, &value, "HasLanes", "hasLanes")
}

func TestOrgSaveOutHasLanesRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value OrgSaveOut
	assertWireFieldRejectsWrongShape(t, &value, "HasLanes", "hasLanes")
}

func TestOrgSaveOutHasLanesMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value OrgSaveOut
	assertWireFieldMissingPreservesValue(t, &value, "HasLanes")
}

func TestOrgSaveOutHasLanesNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value OrgSaveOut
	assertWireFieldNullSemantics(t, &value, "HasLanes", "hasLanes")
}

func TestProjectDigestRowProjectBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Project", "project")
}

func TestProjectDigestRowProjectRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldRejectsWrongShape(t, &value, "Project", "project")
}

func TestProjectDigestRowProjectMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldMissingPreservesValue(t, &value, "Project")
}

func TestProjectDigestRowProjectNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldNullSemantics(t, &value, "Project", "project")
}

func TestProjectDigestRowHeadlineBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Headline", "headline")
}

func TestProjectDigestRowHeadlineRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldRejectsWrongShape(t, &value, "Headline", "headline")
}

func TestProjectDigestRowHeadlineMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldMissingPreservesValue(t, &value, "Headline")
}

func TestProjectDigestRowHeadlineNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldNullSemantics(t, &value, "Headline", "headline")
}

func TestProjectDigestRowBulletsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Bullets", "bullets")
}

func TestProjectDigestRowBulletsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldRejectsWrongShape(t, &value, "Bullets", "bullets")
}

func TestProjectDigestRowBulletsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldMissingPreservesValue(t, &value, "Bullets")
}

func TestProjectDigestRowBulletsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldNullSemantics(t, &value, "Bullets", "bullets")
}

func TestProjectDigestRowDueBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Due", "due")
}

func TestProjectDigestRowDueRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldRejectsWrongShape(t, &value, "Due", "due")
}

func TestProjectDigestRowDueMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldMissingPreservesValue(t, &value, "Due")
}

func TestProjectDigestRowDueNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldNullSemantics(t, &value, "Due", "due")
}

func TestProjectDigestRowUpdatedAtMsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldBoundaryRoundTrip(t, &value, "UpdatedAtMs", "updatedAtMs")
}

func TestProjectDigestRowUpdatedAtMsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldRejectsWrongShape(t, &value, "UpdatedAtMs", "updatedAtMs")
}

func TestProjectDigestRowUpdatedAtMsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldMissingPreservesValue(t, &value, "UpdatedAtMs")
}

func TestProjectDigestRowUpdatedAtMsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldNullSemantics(t, &value, "UpdatedAtMs", "updatedAtMs")
}

func TestProjectDigestRowPathBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Path", "path")
}

func TestProjectDigestRowPathRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldRejectsWrongShape(t, &value, "Path", "path")
}

func TestProjectDigestRowPathMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldMissingPreservesValue(t, &value, "Path")
}

func TestProjectDigestRowPathNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldNullSemantics(t, &value, "Path", "path")
}

func TestProjectDigestRowCodeBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Code", "code")
}

func TestProjectDigestRowCodeRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldRejectsWrongShape(t, &value, "Code", "code")
}

func TestProjectDigestRowCodeMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldMissingPreservesValue(t, &value, "Code")
}

func TestProjectDigestRowCodeNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldNullSemantics(t, &value, "Code", "code")
}

func TestProjectDigestRowClientBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Client", "client")
}

func TestProjectDigestRowClientRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldRejectsWrongShape(t, &value, "Client", "client")
}

func TestProjectDigestRowClientMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldMissingPreservesValue(t, &value, "Client")
}

func TestProjectDigestRowClientNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldNullSemantics(t, &value, "Client", "client")
}

func TestProjectDigestRowRefsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Refs", "refs")
}

func TestProjectDigestRowRefsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldRejectsWrongShape(t, &value, "Refs", "refs")
}

func TestProjectDigestRowRefsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldMissingPreservesValue(t, &value, "Refs")
}

func TestProjectDigestRowRefsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldNullSemantics(t, &value, "Refs", "refs")
}

func TestProjectDigestsOutDigestsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ProjectDigestsOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Digests", "digests")
}

func TestProjectDigestsOutDigestsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ProjectDigestsOut
	assertWireFieldRejectsWrongShape(t, &value, "Digests", "digests")
}

func TestProjectDigestsOutDigestsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ProjectDigestsOut
	assertWireFieldMissingPreservesValue(t, &value, "Digests")
}

func TestProjectDigestsOutDigestsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ProjectDigestsOut
	assertWireFieldNullSemantics(t, &value, "Digests", "digests")
}

func TestProjectLinkedOutMailBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ProjectLinkedOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Mail", "mail")
}

func TestProjectLinkedOutMailRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ProjectLinkedOut
	assertWireFieldRejectsWrongShape(t, &value, "Mail", "mail")
}

func TestProjectLinkedOutMailMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ProjectLinkedOut
	assertWireFieldMissingPreservesValue(t, &value, "Mail")
}

func TestProjectLinkedOutMailNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ProjectLinkedOut
	assertWireFieldNullSemantics(t, &value, "Mail", "mail")
}

func TestProjectLinkedOutCalendarBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ProjectLinkedOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Calendar", "calendar")
}

func TestProjectLinkedOutCalendarRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ProjectLinkedOut
	assertWireFieldRejectsWrongShape(t, &value, "Calendar", "calendar")
}

func TestProjectLinkedOutCalendarMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ProjectLinkedOut
	assertWireFieldMissingPreservesValue(t, &value, "Calendar")
}

func TestProjectLinkedOutCalendarNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ProjectLinkedOut
	assertWireFieldNullSemantics(t, &value, "Calendar", "calendar")
}

func TestProjectLinkedOutTodoBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ProjectLinkedOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Todo", "todo")
}

func TestProjectLinkedOutTodoRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ProjectLinkedOut
	assertWireFieldRejectsWrongShape(t, &value, "Todo", "todo")
}

func TestProjectLinkedOutTodoMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ProjectLinkedOut
	assertWireFieldMissingPreservesValue(t, &value, "Todo")
}

func TestProjectLinkedOutTodoNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ProjectLinkedOut
	assertWireFieldNullSemantics(t, &value, "Todo", "todo")
}

func TestProjectLinkedOutWorkfeedBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ProjectLinkedOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Workfeed", "workfeed")
}

func TestProjectLinkedOutWorkfeedRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ProjectLinkedOut
	assertWireFieldRejectsWrongShape(t, &value, "Workfeed", "workfeed")
}

func TestProjectLinkedOutWorkfeedMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ProjectLinkedOut
	assertWireFieldMissingPreservesValue(t, &value, "Workfeed")
}

func TestProjectLinkedOutWorkfeedNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ProjectLinkedOut
	assertWireFieldNullSemantics(t, &value, "Workfeed", "workfeed")
}

func TestProjectLinkedOutNotebookBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value ProjectLinkedOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Notebook", "notebook")
}

func TestProjectLinkedOutNotebookRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value ProjectLinkedOut
	assertWireFieldRejectsWrongShape(t, &value, "Notebook", "notebook")
}

func TestProjectLinkedOutNotebookMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value ProjectLinkedOut
	assertWireFieldMissingPreservesValue(t, &value, "Notebook")
}

func TestProjectLinkedOutNotebookNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value ProjectLinkedOut
	assertWireFieldNullSemantics(t, &value, "Notebook", "notebook")
}

func TestPromptTunerRunResponseTargetBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptTunerRunResponse
	assertWireFieldBoundaryRoundTrip(t, &value, "Target", "target")
}

func TestPromptTunerRunResponseTargetRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptTunerRunResponse
	assertWireFieldRejectsWrongShape(t, &value, "Target", "target")
}

func TestPromptTunerRunResponseTargetMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptTunerRunResponse
	assertWireFieldMissingPreservesValue(t, &value, "Target")
}

func TestPromptTunerRunResponseTargetNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptTunerRunResponse
	assertWireFieldNullSemantics(t, &value, "Target", "target")
}

func TestPromptTunerRunResponseReportBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptTunerRunResponse
	assertWireFieldBoundaryRoundTrip(t, &value, "Report", "report")
}

func TestPromptTunerRunResponseReportRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptTunerRunResponse
	assertWireFieldRejectsWrongShape(t, &value, "Report", "report")
}

func TestPromptTunerRunResponseReportMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptTunerRunResponse
	assertWireFieldMissingPreservesValue(t, &value, "Report")
}

func TestPromptTunerRunResponseReportNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptTunerRunResponse
	assertWireFieldNullSemantics(t, &value, "Report", "report")
}

func TestPromptTunerReportRanBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldBoundaryRoundTrip(t, &value, "Ran", "ran")
}

func TestPromptTunerReportRanRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldRejectsWrongShape(t, &value, "Ran", "ran")
}

func TestPromptTunerReportRanMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldMissingPreservesValue(t, &value, "Ran")
}

func TestPromptTunerReportRanNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldNullSemantics(t, &value, "Ran", "ran")
}

func TestPromptTunerReportChangedBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldBoundaryRoundTrip(t, &value, "Changed", "changed")
}

func TestPromptTunerReportChangedRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldRejectsWrongShape(t, &value, "Changed", "changed")
}

func TestPromptTunerReportChangedMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldMissingPreservesValue(t, &value, "Changed")
}

func TestPromptTunerReportChangedNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldNullSemantics(t, &value, "Changed", "changed")
}

func TestPromptTunerReportReasonBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldBoundaryRoundTrip(t, &value, "Reason", "reason")
}

func TestPromptTunerReportReasonRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldRejectsWrongShape(t, &value, "Reason", "reason")
}

func TestPromptTunerReportReasonMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldMissingPreservesValue(t, &value, "Reason")
}

func TestPromptTunerReportReasonNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldNullSemantics(t, &value, "Reason", "reason")
}

func TestPromptTunerReportErrorBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldBoundaryRoundTrip(t, &value, "Error", "error")
}

func TestPromptTunerReportErrorRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldRejectsWrongShape(t, &value, "Error", "error")
}

func TestPromptTunerReportErrorMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldMissingPreservesValue(t, &value, "Error")
}

func TestPromptTunerReportErrorNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldNullSemantics(t, &value, "Error", "error")
}

func TestPromptTunerReportLeafSummariesBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldBoundaryRoundTrip(t, &value, "LeafSummaries", "leafSummaries")
}

func TestPromptTunerReportLeafSummariesRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldRejectsWrongShape(t, &value, "LeafSummaries", "leafSummaries")
}

func TestPromptTunerReportLeafSummariesMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldMissingPreservesValue(t, &value, "LeafSummaries")
}

func TestPromptTunerReportLeafSummariesNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldNullSemantics(t, &value, "LeafSummaries", "leafSummaries")
}

func TestPromptTunerReportMinSummariesBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldBoundaryRoundTrip(t, &value, "MinSummaries", "minSummaries")
}

func TestPromptTunerReportMinSummariesRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldRejectsWrongShape(t, &value, "MinSummaries", "minSummaries")
}

func TestPromptTunerReportMinSummariesMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldMissingPreservesValue(t, &value, "MinSummaries")
}

func TestPromptTunerReportMinSummariesNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldNullSemantics(t, &value, "MinSummaries", "minSummaries")
}

func TestPromptTunerReportProposedBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldBoundaryRoundTrip(t, &value, "Proposed", "proposed")
}

func TestPromptTunerReportProposedRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldRejectsWrongShape(t, &value, "Proposed", "proposed")
}

func TestPromptTunerReportProposedMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldMissingPreservesValue(t, &value, "Proposed")
}

func TestPromptTunerReportProposedNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldNullSemantics(t, &value, "Proposed", "proposed")
}

func TestPromptTunerReportAddedBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldBoundaryRoundTrip(t, &value, "Added", "added")
}

func TestPromptTunerReportAddedRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldRejectsWrongShape(t, &value, "Added", "added")
}

func TestPromptTunerReportAddedMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldMissingPreservesValue(t, &value, "Added")
}

func TestPromptTunerReportAddedNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldNullSemantics(t, &value, "Added", "added")
}

func TestPromptTunerReportBeforeCountBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldBoundaryRoundTrip(t, &value, "BeforeCount", "beforeCount")
}

func TestPromptTunerReportBeforeCountRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldRejectsWrongShape(t, &value, "BeforeCount", "beforeCount")
}

func TestPromptTunerReportBeforeCountMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldMissingPreservesValue(t, &value, "BeforeCount")
}

func TestPromptTunerReportBeforeCountNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldNullSemantics(t, &value, "BeforeCount", "beforeCount")
}

func TestPromptTunerReportAfterCountBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldBoundaryRoundTrip(t, &value, "AfterCount", "afterCount")
}

func TestPromptTunerReportAfterCountRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldRejectsWrongShape(t, &value, "AfterCount", "afterCount")
}

func TestPromptTunerReportAfterCountMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldMissingPreservesValue(t, &value, "AfterCount")
}

func TestPromptTunerReportAfterCountNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldNullSemantics(t, &value, "AfterCount", "afterCount")
}

func TestPromptRowIDBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldBoundaryRoundTrip(t, &value, "ID", "id")
}

func TestPromptRowIDRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldRejectsWrongShape(t, &value, "ID", "id")
}

func TestPromptRowIDMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldMissingPreservesValue(t, &value, "ID")
}

func TestPromptRowIDNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldNullSemantics(t, &value, "ID", "id")
}

func TestPromptRowTitleBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Title", "title")
}

func TestPromptRowTitleRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldRejectsWrongShape(t, &value, "Title", "title")
}

func TestPromptRowTitleMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldMissingPreservesValue(t, &value, "Title")
}

func TestPromptRowTitleNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldNullSemantics(t, &value, "Title", "title")
}

func TestPromptRowDescriptionBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Description", "description")
}

func TestPromptRowDescriptionRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldRejectsWrongShape(t, &value, "Description", "description")
}

func TestPromptRowDescriptionMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldMissingPreservesValue(t, &value, "Description")
}

func TestPromptRowDescriptionNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldNullSemantics(t, &value, "Description", "description")
}

func TestPromptRowCategoryBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Category", "category")
}

func TestPromptRowCategoryRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldRejectsWrongShape(t, &value, "Category", "category")
}

func TestPromptRowCategoryMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldMissingPreservesValue(t, &value, "Category")
}

func TestPromptRowCategoryNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldNullSemantics(t, &value, "Category", "category")
}

func TestPromptRowEditableBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Editable", "editable")
}

func TestPromptRowEditableRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldRejectsWrongShape(t, &value, "Editable", "editable")
}

func TestPromptRowEditableMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldMissingPreservesValue(t, &value, "Editable")
}

func TestPromptRowEditableNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldNullSemantics(t, &value, "Editable", "editable")
}

func TestPromptRowOverriddenBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Overridden", "overridden")
}

func TestPromptRowOverriddenRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldRejectsWrongShape(t, &value, "Overridden", "overridden")
}

func TestPromptRowOverriddenMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldMissingPreservesValue(t, &value, "Overridden")
}

func TestPromptRowOverriddenNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldNullSemantics(t, &value, "Overridden", "overridden")
}

func TestPromptRowUpdatedAtMsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldBoundaryRoundTrip(t, &value, "UpdatedAtMs", "updatedAtMs")
}

func TestPromptRowUpdatedAtMsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldRejectsWrongShape(t, &value, "UpdatedAtMs", "updatedAtMs")
}

func TestPromptRowUpdatedAtMsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldMissingPreservesValue(t, &value, "UpdatedAtMs")
}

func TestPromptRowUpdatedAtMsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldNullSemantics(t, &value, "UpdatedAtMs", "updatedAtMs")
}

func TestPromptDetailOutIDBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldBoundaryRoundTrip(t, &value, "ID", "id")
}

func TestPromptDetailOutIDRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldRejectsWrongShape(t, &value, "ID", "id")
}

func TestPromptDetailOutIDMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldMissingPreservesValue(t, &value, "ID")
}

func TestPromptDetailOutIDNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldNullSemantics(t, &value, "ID", "id")
}

func TestPromptDetailOutTitleBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Title", "title")
}

func TestPromptDetailOutTitleRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldRejectsWrongShape(t, &value, "Title", "title")
}

func TestPromptDetailOutTitleMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldMissingPreservesValue(t, &value, "Title")
}

func TestPromptDetailOutTitleNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldNullSemantics(t, &value, "Title", "title")
}

func TestPromptDetailOutDescriptionBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Description", "description")
}

func TestPromptDetailOutDescriptionRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldRejectsWrongShape(t, &value, "Description", "description")
}

func TestPromptDetailOutDescriptionMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldMissingPreservesValue(t, &value, "Description")
}

func TestPromptDetailOutDescriptionNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldNullSemantics(t, &value, "Description", "description")
}

func TestPromptDetailOutCategoryBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Category", "category")
}

func TestPromptDetailOutCategoryRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldRejectsWrongShape(t, &value, "Category", "category")
}

func TestPromptDetailOutCategoryMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldMissingPreservesValue(t, &value, "Category")
}

func TestPromptDetailOutCategoryNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldNullSemantics(t, &value, "Category", "category")
}

func TestPromptDetailOutTextBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Text", "text")
}

func TestPromptDetailOutTextRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldRejectsWrongShape(t, &value, "Text", "text")
}

func TestPromptDetailOutTextMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldMissingPreservesValue(t, &value, "Text")
}

func TestPromptDetailOutTextNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldNullSemantics(t, &value, "Text", "text")
}

func TestPromptDetailOutDefaultTextBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldBoundaryRoundTrip(t, &value, "DefaultText", "defaultText")
}

func TestPromptDetailOutDefaultTextRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldRejectsWrongShape(t, &value, "DefaultText", "defaultText")
}

func TestPromptDetailOutDefaultTextMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldMissingPreservesValue(t, &value, "DefaultText")
}

func TestPromptDetailOutDefaultTextNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldNullSemantics(t, &value, "DefaultText", "defaultText")
}

func TestPromptDetailOutEditableBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Editable", "editable")
}

func TestPromptDetailOutEditableRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldRejectsWrongShape(t, &value, "Editable", "editable")
}

func TestPromptDetailOutEditableMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldMissingPreservesValue(t, &value, "Editable")
}

func TestPromptDetailOutEditableNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldNullSemantics(t, &value, "Editable", "editable")
}

func TestPromptDetailOutOverriddenBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Overridden", "overridden")
}

func TestPromptDetailOutOverriddenRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldRejectsWrongShape(t, &value, "Overridden", "overridden")
}

func TestPromptDetailOutOverriddenMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldMissingPreservesValue(t, &value, "Overridden")
}

func TestPromptDetailOutOverriddenNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldNullSemantics(t, &value, "Overridden", "overridden")
}

func TestPromptDetailOutUpdatedAtMsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldBoundaryRoundTrip(t, &value, "UpdatedAtMs", "updatedAtMs")
}

func TestPromptDetailOutUpdatedAtMsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldRejectsWrongShape(t, &value, "UpdatedAtMs", "updatedAtMs")
}

func TestPromptDetailOutUpdatedAtMsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldMissingPreservesValue(t, &value, "UpdatedAtMs")
}

func TestPromptDetailOutUpdatedAtMsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldNullSemantics(t, &value, "UpdatedAtMs", "updatedAtMs")
}

func TestPromptListResponsePromptsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptListResponse
	assertWireFieldBoundaryRoundTrip(t, &value, "Prompts", "prompts")
}

func TestPromptListResponsePromptsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptListResponse
	assertWireFieldRejectsWrongShape(t, &value, "Prompts", "prompts")
}

func TestPromptListResponsePromptsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptListResponse
	assertWireFieldMissingPreservesValue(t, &value, "Prompts")
}

func TestPromptListResponsePromptsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptListResponse
	assertWireFieldNullSemantics(t, &value, "Prompts", "prompts")
}

func TestPromptListResponseCountBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PromptListResponse
	assertWireFieldBoundaryRoundTrip(t, &value, "Count", "count")
}

func TestPromptListResponseCountRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PromptListResponse
	assertWireFieldRejectsWrongShape(t, &value, "Count", "count")
}

func TestPromptListResponseCountMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PromptListResponse
	assertWireFieldMissingPreservesValue(t, &value, "Count")
}

func TestPromptListResponseCountNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PromptListResponse
	assertWireFieldNullSemantics(t, &value, "Count", "count")
}

func TestSelfCorrectionCandidateIDBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldBoundaryRoundTrip(t, &value, "ID", "id")
}

func TestSelfCorrectionCandidateIDRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldRejectsWrongShape(t, &value, "ID", "id")
}

func TestSelfCorrectionCandidateIDMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldMissingPreservesValue(t, &value, "ID")
}

func TestSelfCorrectionCandidateIDNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldNullSemantics(t, &value, "ID", "id")
}

func TestSelfCorrectionCandidateStatusBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldBoundaryRoundTrip(t, &value, "Status", "status")
}

func TestSelfCorrectionCandidateStatusRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldRejectsWrongShape(t, &value, "Status", "status")
}

func TestSelfCorrectionCandidateStatusMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldMissingPreservesValue(t, &value, "Status")
}

func TestSelfCorrectionCandidateStatusNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldNullSemantics(t, &value, "Status", "status")
}

func TestSelfCorrectionCandidateScopeBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldBoundaryRoundTrip(t, &value, "Scope", "scope")
}

func TestSelfCorrectionCandidateScopeRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldRejectsWrongShape(t, &value, "Scope", "scope")
}

func TestSelfCorrectionCandidateScopeMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldMissingPreservesValue(t, &value, "Scope")
}

func TestSelfCorrectionCandidateScopeNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldNullSemantics(t, &value, "Scope", "scope")
}

func TestSelfCorrectionCandidateSkillNameBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldBoundaryRoundTrip(t, &value, "SkillName", "skillName")
}

func TestSelfCorrectionCandidateSkillNameRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldRejectsWrongShape(t, &value, "SkillName", "skillName")
}

func TestSelfCorrectionCandidateSkillNameMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldMissingPreservesValue(t, &value, "SkillName")
}

func TestSelfCorrectionCandidateSkillNameNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldNullSemantics(t, &value, "SkillName", "skillName")
}

func TestSelfCorrectionCandidateSessionKeyBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldBoundaryRoundTrip(t, &value, "SessionKey", "sessionKey")
}

func TestSelfCorrectionCandidateSessionKeyRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldRejectsWrongShape(t, &value, "SessionKey", "sessionKey")
}

func TestSelfCorrectionCandidateSessionKeyMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldMissingPreservesValue(t, &value, "SessionKey")
}

func TestSelfCorrectionCandidateSessionKeyNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldNullSemantics(t, &value, "SessionKey", "sessionKey")
}

func TestSelfCorrectionCandidateTitleBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldBoundaryRoundTrip(t, &value, "Title", "title")
}

func TestSelfCorrectionCandidateTitleRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldRejectsWrongShape(t, &value, "Title", "title")
}

func TestSelfCorrectionCandidateTitleMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldMissingPreservesValue(t, &value, "Title")
}

func TestSelfCorrectionCandidateTitleNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldNullSemantics(t, &value, "Title", "title")
}

func TestSelfCorrectionCandidateCandidateBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldBoundaryRoundTrip(t, &value, "Candidate", "candidate")
}

func TestSelfCorrectionCandidateCandidateRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldRejectsWrongShape(t, &value, "Candidate", "candidate")
}

func TestSelfCorrectionCandidateCandidateMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldMissingPreservesValue(t, &value, "Candidate")
}

func TestSelfCorrectionCandidateCandidateNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldNullSemantics(t, &value, "Candidate", "candidate")
}

func TestSelfCorrectionCandidateEvidenceBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldBoundaryRoundTrip(t, &value, "Evidence", "evidence")
}

func TestSelfCorrectionCandidateEvidenceRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldRejectsWrongShape(t, &value, "Evidence", "evidence")
}

func TestSelfCorrectionCandidateEvidenceMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldMissingPreservesValue(t, &value, "Evidence")
}

func TestSelfCorrectionCandidateEvidenceNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldNullSemantics(t, &value, "Evidence", "evidence")
}

func TestSelfCorrectionCandidateReasonBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldBoundaryRoundTrip(t, &value, "Reason", "reason")
}

func TestSelfCorrectionCandidateReasonRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldRejectsWrongShape(t, &value, "Reason", "reason")
}

func TestSelfCorrectionCandidateReasonMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldMissingPreservesValue(t, &value, "Reason")
}

func TestSelfCorrectionCandidateReasonNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldNullSemantics(t, &value, "Reason", "reason")
}

func TestSelfCorrectionCandidateTargetFilesBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldBoundaryRoundTrip(t, &value, "TargetFiles", "targetFiles")
}

func TestSelfCorrectionCandidateTargetFilesRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldRejectsWrongShape(t, &value, "TargetFiles", "targetFiles")
}

func TestSelfCorrectionCandidateTargetFilesMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldMissingPreservesValue(t, &value, "TargetFiles")
}

func TestSelfCorrectionCandidateTargetFilesNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldNullSemantics(t, &value, "TargetFiles", "targetFiles")
}

func TestSelfCorrectionCandidateProposedChangeBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldBoundaryRoundTrip(t, &value, "ProposedChange", "proposedChange")
}

func TestSelfCorrectionCandidateProposedChangeRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldRejectsWrongShape(t, &value, "ProposedChange", "proposedChange")
}

func TestSelfCorrectionCandidateProposedChangeMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldMissingPreservesValue(t, &value, "ProposedChange")
}

func TestSelfCorrectionCandidateProposedChangeNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldNullSemantics(t, &value, "ProposedChange", "proposedChange")
}

func TestSelfCorrectionCandidateRiskBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldBoundaryRoundTrip(t, &value, "Risk", "risk")
}

func TestSelfCorrectionCandidateRiskRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldRejectsWrongShape(t, &value, "Risk", "risk")
}

func TestSelfCorrectionCandidateRiskMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldMissingPreservesValue(t, &value, "Risk")
}

func TestSelfCorrectionCandidateRiskNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldNullSemantics(t, &value, "Risk", "risk")
}

func TestSelfCorrectionCandidateSourceBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldBoundaryRoundTrip(t, &value, "Source", "source")
}

func TestSelfCorrectionCandidateSourceRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldRejectsWrongShape(t, &value, "Source", "source")
}

func TestSelfCorrectionCandidateSourceMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldMissingPreservesValue(t, &value, "Source")
}

func TestSelfCorrectionCandidateSourceNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldNullSemantics(t, &value, "Source", "source")
}

func TestSelfCorrectionCandidateReviewerBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldBoundaryRoundTrip(t, &value, "Reviewer", "reviewer")
}

func TestSelfCorrectionCandidateReviewerRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldRejectsWrongShape(t, &value, "Reviewer", "reviewer")
}

func TestSelfCorrectionCandidateReviewerMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldMissingPreservesValue(t, &value, "Reviewer")
}

func TestSelfCorrectionCandidateReviewerNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldNullSemantics(t, &value, "Reviewer", "reviewer")
}

func TestSelfCorrectionCandidateReviewNoteBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldBoundaryRoundTrip(t, &value, "ReviewNote", "reviewNote")
}

func TestSelfCorrectionCandidateReviewNoteRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldRejectsWrongShape(t, &value, "ReviewNote", "reviewNote")
}

func TestSelfCorrectionCandidateReviewNoteMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldMissingPreservesValue(t, &value, "ReviewNote")
}

func TestSelfCorrectionCandidateReviewNoteNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldNullSemantics(t, &value, "ReviewNote", "reviewNote")
}

func TestSelfCorrectionCandidateEvidenceKindsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldBoundaryRoundTrip(t, &value, "EvidenceKinds", "evidenceKinds")
}

func TestSelfCorrectionCandidateEvidenceKindsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldRejectsWrongShape(t, &value, "EvidenceKinds", "evidenceKinds")
}

func TestSelfCorrectionCandidateEvidenceKindsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldMissingPreservesValue(t, &value, "EvidenceKinds")
}

func TestSelfCorrectionCandidateEvidenceKindsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldNullSemantics(t, &value, "EvidenceKinds", "evidenceKinds")
}

func TestSelfCorrectionCandidateReviewActionsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldBoundaryRoundTrip(t, &value, "ReviewActions", "reviewActions")
}

func TestSelfCorrectionCandidateReviewActionsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldRejectsWrongShape(t, &value, "ReviewActions", "reviewActions")
}

func TestSelfCorrectionCandidateReviewActionsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldMissingPreservesValue(t, &value, "ReviewActions")
}

func TestSelfCorrectionCandidateReviewActionsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldNullSemantics(t, &value, "ReviewActions", "reviewActions")
}

func TestSelfCorrectionCandidateCreatedAtBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldBoundaryRoundTrip(t, &value, "CreatedAt", "createdAt")
}

func TestSelfCorrectionCandidateCreatedAtRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldRejectsWrongShape(t, &value, "CreatedAt", "createdAt")
}

func TestSelfCorrectionCandidateCreatedAtMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldMissingPreservesValue(t, &value, "CreatedAt")
}

func TestSelfCorrectionCandidateCreatedAtNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldNullSemantics(t, &value, "CreatedAt", "createdAt")
}

func TestSelfCorrectionCandidateUpdatedAtBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldBoundaryRoundTrip(t, &value, "UpdatedAt", "updatedAt")
}

func TestSelfCorrectionCandidateUpdatedAtRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldRejectsWrongShape(t, &value, "UpdatedAt", "updatedAt")
}

func TestSelfCorrectionCandidateUpdatedAtMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldMissingPreservesValue(t, &value, "UpdatedAt")
}

func TestSelfCorrectionCandidateUpdatedAtNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldNullSemantics(t, &value, "UpdatedAt", "updatedAt")
}

func TestSelfImprovementCodingStatusCountStatusBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingStatusCount
	assertWireFieldBoundaryRoundTrip(t, &value, "Status", "status")
}

func TestSelfImprovementCodingStatusCountStatusRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingStatusCount
	assertWireFieldRejectsWrongShape(t, &value, "Status", "status")
}

func TestSelfImprovementCodingStatusCountStatusMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingStatusCount
	assertWireFieldMissingPreservesValue(t, &value, "Status")
}

func TestSelfImprovementCodingStatusCountStatusNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingStatusCount
	assertWireFieldNullSemantics(t, &value, "Status", "status")
}

func TestSelfImprovementCodingStatusCountCountBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingStatusCount
	assertWireFieldBoundaryRoundTrip(t, &value, "Count", "count")
}

func TestSelfImprovementCodingStatusCountCountRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingStatusCount
	assertWireFieldRejectsWrongShape(t, &value, "Count", "count")
}

func TestSelfImprovementCodingStatusCountCountMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingStatusCount
	assertWireFieldMissingPreservesValue(t, &value, "Count")
}

func TestSelfImprovementCodingStatusCountCountNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingStatusCount
	assertWireFieldNullSemantics(t, &value, "Count", "count")
}

func TestSelfImprovementCodingFunnelLastCaptureAtBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldBoundaryRoundTrip(t, &value, "LastCaptureAt", "lastCaptureAt")
}

func TestSelfImprovementCodingFunnelLastCaptureAtRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldRejectsWrongShape(t, &value, "LastCaptureAt", "lastCaptureAt")
}

func TestSelfImprovementCodingFunnelLastCaptureAtMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldMissingPreservesValue(t, &value, "LastCaptureAt")
}

func TestSelfImprovementCodingFunnelLastCaptureAtNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldNullSemantics(t, &value, "LastCaptureAt", "lastCaptureAt")
}

func TestSelfImprovementCodingFunnelLastReviewAtBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldBoundaryRoundTrip(t, &value, "LastReviewAt", "lastReviewAt")
}

func TestSelfImprovementCodingFunnelLastReviewAtRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldRejectsWrongShape(t, &value, "LastReviewAt", "lastReviewAt")
}

func TestSelfImprovementCodingFunnelLastReviewAtMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldMissingPreservesValue(t, &value, "LastReviewAt")
}

func TestSelfImprovementCodingFunnelLastReviewAtNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldNullSemantics(t, &value, "LastReviewAt", "lastReviewAt")
}

func TestSelfImprovementCodingFunnelRejections7dBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldBoundaryRoundTrip(t, &value, "Rejections7d", "rejections7d")
}

func TestSelfImprovementCodingFunnelRejections7dRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldRejectsWrongShape(t, &value, "Rejections7d", "rejections7d")
}

func TestSelfImprovementCodingFunnelRejections7dMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldMissingPreservesValue(t, &value, "Rejections7d")
}

func TestSelfImprovementCodingFunnelRejections7dNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldNullSemantics(t, &value, "Rejections7d", "rejections7d")
}

func TestSelfImprovementCodingFunnelPromotableRejections7dBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldBoundaryRoundTrip(t, &value, "PromotableRejections7d", "promotableRejections7d")
}

func TestSelfImprovementCodingFunnelPromotableRejections7dRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldRejectsWrongShape(t, &value, "PromotableRejections7d", "promotableRejections7d")
}

func TestSelfImprovementCodingFunnelPromotableRejections7dMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldMissingPreservesValue(t, &value, "PromotableRejections7d")
}

func TestSelfImprovementCodingFunnelPromotableRejections7dNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldNullSemantics(t, &value, "PromotableRejections7d", "promotableRejections7d")
}

func TestSelfImprovementCodingFunnelLastRejectionAtBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldBoundaryRoundTrip(t, &value, "LastRejectionAt", "lastRejectionAt")
}

func TestSelfImprovementCodingFunnelLastRejectionAtRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldRejectsWrongShape(t, &value, "LastRejectionAt", "lastRejectionAt")
}

func TestSelfImprovementCodingFunnelLastRejectionAtMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldMissingPreservesValue(t, &value, "LastRejectionAt")
}

func TestSelfImprovementCodingFunnelLastRejectionAtNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldNullSemantics(t, &value, "LastRejectionAt", "lastRejectionAt")
}

func TestSelfImprovementCodingFunnelLastNudgeAtBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldBoundaryRoundTrip(t, &value, "LastNudgeAt", "lastNudgeAt")
}

func TestSelfImprovementCodingFunnelLastNudgeAtRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldRejectsWrongShape(t, &value, "LastNudgeAt", "lastNudgeAt")
}

func TestSelfImprovementCodingFunnelLastNudgeAtMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldMissingPreservesValue(t, &value, "LastNudgeAt")
}

func TestSelfImprovementCodingFunnelLastNudgeAtNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldNullSemantics(t, &value, "LastNudgeAt", "lastNudgeAt")
}

func TestSelfImprovementCodingListResponseCandidatesBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingListResponse
	assertWireFieldBoundaryRoundTrip(t, &value, "Candidates", "candidates")
}

func TestSelfImprovementCodingListResponseCandidatesRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingListResponse
	assertWireFieldRejectsWrongShape(t, &value, "Candidates", "candidates")
}

func TestSelfImprovementCodingListResponseCandidatesMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingListResponse
	assertWireFieldMissingPreservesValue(t, &value, "Candidates")
}

func TestSelfImprovementCodingListResponseCandidatesNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingListResponse
	assertWireFieldNullSemantics(t, &value, "Candidates", "candidates")
}

func TestSelfImprovementCodingListResponseCountBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingListResponse
	assertWireFieldBoundaryRoundTrip(t, &value, "Count", "count")
}

func TestSelfImprovementCodingListResponseCountRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingListResponse
	assertWireFieldRejectsWrongShape(t, &value, "Count", "count")
}

func TestSelfImprovementCodingListResponseCountMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingListResponse
	assertWireFieldMissingPreservesValue(t, &value, "Count")
}

func TestSelfImprovementCodingListResponseCountNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingListResponse
	assertWireFieldNullSemantics(t, &value, "Count", "count")
}

func TestSelfImprovementCodingListResponseStatusCountsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingListResponse
	assertWireFieldBoundaryRoundTrip(t, &value, "StatusCounts", "statusCounts")
}

func TestSelfImprovementCodingListResponseStatusCountsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingListResponse
	assertWireFieldRejectsWrongShape(t, &value, "StatusCounts", "statusCounts")
}

func TestSelfImprovementCodingListResponseStatusCountsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingListResponse
	assertWireFieldMissingPreservesValue(t, &value, "StatusCounts")
}

func TestSelfImprovementCodingListResponseStatusCountsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingListResponse
	assertWireFieldNullSemantics(t, &value, "StatusCounts", "statusCounts")
}

func TestSelfImprovementCodingListResponseFunnelBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingListResponse
	assertWireFieldBoundaryRoundTrip(t, &value, "Funnel", "funnel")
}

func TestSelfImprovementCodingListResponseFunnelRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingListResponse
	assertWireFieldRejectsWrongShape(t, &value, "Funnel", "funnel")
}

func TestSelfImprovementCodingListResponseFunnelMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingListResponse
	assertWireFieldMissingPreservesValue(t, &value, "Funnel")
}

func TestSelfImprovementCodingListResponseFunnelNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingListResponse
	assertWireFieldNullSemantics(t, &value, "Funnel", "funnel")
}

func TestSessionRowOutKeyBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Key", "key")
}

func TestSessionRowOutKeyRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldRejectsWrongShape(t, &value, "Key", "key")
}

func TestSessionRowOutKeyMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldMissingPreservesValue(t, &value, "Key")
}

func TestSessionRowOutKeyNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldNullSemantics(t, &value, "Key", "key")
}

func TestSessionRowOutKindBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Kind", "kind")
}

func TestSessionRowOutKindRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldRejectsWrongShape(t, &value, "Kind", "kind")
}

func TestSessionRowOutKindMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldMissingPreservesValue(t, &value, "Kind")
}

func TestSessionRowOutKindNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldNullSemantics(t, &value, "Kind", "kind")
}

func TestSessionRowOutStatusBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Status", "status")
}

func TestSessionRowOutStatusRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldRejectsWrongShape(t, &value, "Status", "status")
}

func TestSessionRowOutStatusMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldMissingPreservesValue(t, &value, "Status")
}

func TestSessionRowOutStatusNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldNullSemantics(t, &value, "Status", "status")
}

func TestSessionRowOutChannelBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Channel", "channel")
}

func TestSessionRowOutChannelRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldRejectsWrongShape(t, &value, "Channel", "channel")
}

func TestSessionRowOutChannelMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldMissingPreservesValue(t, &value, "Channel")
}

func TestSessionRowOutChannelNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldNullSemantics(t, &value, "Channel", "channel")
}

func TestSessionRowOutModelBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Model", "model")
}

func TestSessionRowOutModelRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldRejectsWrongShape(t, &value, "Model", "model")
}

func TestSessionRowOutModelMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldMissingPreservesValue(t, &value, "Model")
}

func TestSessionRowOutModelNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldNullSemantics(t, &value, "Model", "model")
}

func TestSessionRowOutLabelBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Label", "label")
}

func TestSessionRowOutLabelRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldRejectsWrongShape(t, &value, "Label", "label")
}

func TestSessionRowOutLabelMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldMissingPreservesValue(t, &value, "Label")
}

func TestSessionRowOutLabelNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldNullSemantics(t, &value, "Label", "label")
}

func TestSessionRowOutUpdatedAtMsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "UpdatedAtMs", "updatedAtMs")
}

func TestSessionRowOutUpdatedAtMsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldRejectsWrongShape(t, &value, "UpdatedAtMs", "updatedAtMs")
}

func TestSessionRowOutUpdatedAtMsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldMissingPreservesValue(t, &value, "UpdatedAtMs")
}

func TestSessionRowOutUpdatedAtMsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldNullSemantics(t, &value, "UpdatedAtMs", "updatedAtMs")
}

func TestSessionRowOutStartedAtMsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "StartedAtMs", "startedAtMs")
}

func TestSessionRowOutStartedAtMsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldRejectsWrongShape(t, &value, "StartedAtMs", "startedAtMs")
}

func TestSessionRowOutStartedAtMsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldMissingPreservesValue(t, &value, "StartedAtMs")
}

func TestSessionRowOutStartedAtMsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldNullSemantics(t, &value, "StartedAtMs", "startedAtMs")
}

func TestSessionRowOutRuntimeMsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "RuntimeMs", "runtimeMs")
}

func TestSessionRowOutRuntimeMsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldRejectsWrongShape(t, &value, "RuntimeMs", "runtimeMs")
}

func TestSessionRowOutRuntimeMsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldMissingPreservesValue(t, &value, "RuntimeMs")
}

func TestSessionRowOutRuntimeMsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldNullSemantics(t, &value, "RuntimeMs", "runtimeMs")
}

func TestSessionRowOutTotalTokensBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldBoundaryRoundTrip(t, &value, "TotalTokens", "totalTokens")
}

func TestSessionRowOutTotalTokensRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldRejectsWrongShape(t, &value, "TotalTokens", "totalTokens")
}

func TestSessionRowOutTotalTokensMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldMissingPreservesValue(t, &value, "TotalTokens")
}

func TestSessionRowOutTotalTokensNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldNullSemantics(t, &value, "TotalTokens", "totalTokens")
}

func TestTranscriptMsgOutIDBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value transcriptMsgOut
	assertWireFieldBoundaryRoundTrip(t, &value, "ID", "id")
}

func TestTranscriptMsgOutIDRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value transcriptMsgOut
	assertWireFieldRejectsWrongShape(t, &value, "ID", "id")
}

func TestTranscriptMsgOutIDMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value transcriptMsgOut
	assertWireFieldMissingPreservesValue(t, &value, "ID")
}

func TestTranscriptMsgOutIDNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value transcriptMsgOut
	assertWireFieldNullSemantics(t, &value, "ID", "id")
}

func TestTranscriptMsgOutRoleBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value transcriptMsgOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Role", "role")
}

func TestTranscriptMsgOutRoleRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value transcriptMsgOut
	assertWireFieldRejectsWrongShape(t, &value, "Role", "role")
}

func TestTranscriptMsgOutRoleMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value transcriptMsgOut
	assertWireFieldMissingPreservesValue(t, &value, "Role")
}

func TestTranscriptMsgOutRoleNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value transcriptMsgOut
	assertWireFieldNullSemantics(t, &value, "Role", "role")
}

func TestTranscriptMsgOutContentBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value transcriptMsgOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Content", "content")
}

func TestTranscriptMsgOutContentRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value transcriptMsgOut
	assertWireFieldRejectsWrongShape(t, &value, "Content", "content")
}

func TestTranscriptMsgOutContentMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value transcriptMsgOut
	assertWireFieldMissingPreservesValue(t, &value, "Content")
}

func TestTranscriptMsgOutContentNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value transcriptMsgOut
	assertWireFieldNullSemantics(t, &value, "Content", "content")
}

func TestTranscriptMsgOutAttachmentsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value transcriptMsgOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Attachments", "attachments")
}

func TestTranscriptMsgOutAttachmentsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value transcriptMsgOut
	assertWireFieldRejectsWrongShape(t, &value, "Attachments", "attachments")
}

func TestTranscriptMsgOutAttachmentsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value transcriptMsgOut
	assertWireFieldMissingPreservesValue(t, &value, "Attachments")
}

func TestTranscriptMsgOutAttachmentsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value transcriptMsgOut
	assertWireFieldNullSemantics(t, &value, "Attachments", "attachments")
}

func TestTranscriptMsgOutTimestampMsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value transcriptMsgOut
	assertWireFieldBoundaryRoundTrip(t, &value, "TimestampMs", "timestampMs")
}

func TestTranscriptMsgOutTimestampMsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value transcriptMsgOut
	assertWireFieldRejectsWrongShape(t, &value, "TimestampMs", "timestampMs")
}

func TestTranscriptMsgOutTimestampMsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value transcriptMsgOut
	assertWireFieldMissingPreservesValue(t, &value, "TimestampMs")
}

func TestTranscriptMsgOutTimestampMsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value transcriptMsgOut
	assertWireFieldNullSemantics(t, &value, "TimestampMs", "timestampMs")
}

func TestSkillRowNameBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Name", "name")
}

func TestSkillRowNameRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldRejectsWrongShape(t, &value, "Name", "name")
}

func TestSkillRowNameMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldMissingPreservesValue(t, &value, "Name")
}

func TestSkillRowNameNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldNullSemantics(t, &value, "Name", "name")
}

func TestSkillRowDescriptionBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Description", "description")
}

func TestSkillRowDescriptionRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldRejectsWrongShape(t, &value, "Description", "description")
}

func TestSkillRowDescriptionMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldMissingPreservesValue(t, &value, "Description")
}

func TestSkillRowDescriptionNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldNullSemantics(t, &value, "Description", "description")
}

func TestSkillRowCategoryBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Category", "category")
}

func TestSkillRowCategoryRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldRejectsWrongShape(t, &value, "Category", "category")
}

func TestSkillRowCategoryMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldMissingPreservesValue(t, &value, "Category")
}

func TestSkillRowCategoryNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldNullSemantics(t, &value, "Category", "category")
}

func TestSkillRowHomepageBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Homepage", "homepage")
}

func TestSkillRowHomepageRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldRejectsWrongShape(t, &value, "Homepage", "homepage")
}

func TestSkillRowHomepageMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldMissingPreservesValue(t, &value, "Homepage")
}

func TestSkillRowHomepageNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldNullSemantics(t, &value, "Homepage", "homepage")
}

func TestSkillRowTagsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Tags", "tags")
}

func TestSkillRowTagsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldRejectsWrongShape(t, &value, "Tags", "tags")
}

func TestSkillRowTagsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldMissingPreservesValue(t, &value, "Tags")
}

func TestSkillRowTagsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldNullSemantics(t, &value, "Tags", "tags")
}

func TestSkillRowRelatedSkillsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldBoundaryRoundTrip(t, &value, "RelatedSkills", "relatedSkills")
}

func TestSkillRowRelatedSkillsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldRejectsWrongShape(t, &value, "RelatedSkills", "relatedSkills")
}

func TestSkillRowRelatedSkillsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldMissingPreservesValue(t, &value, "RelatedSkills")
}

func TestSkillRowRelatedSkillsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldNullSemantics(t, &value, "RelatedSkills", "relatedSkills")
}

func TestSkillRowSourceBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Source", "source")
}

func TestSkillRowSourceRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldRejectsWrongShape(t, &value, "Source", "source")
}

func TestSkillRowSourceMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldMissingPreservesValue(t, &value, "Source")
}

func TestSkillRowSourceNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldNullSemantics(t, &value, "Source", "source")
}

func TestSkillRowVersionBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Version", "version")
}

func TestSkillRowVersionRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldRejectsWrongShape(t, &value, "Version", "version")
}

func TestSkillRowVersionMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldMissingPreservesValue(t, &value, "Version")
}

func TestSkillRowVersionNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldNullSemantics(t, &value, "Version", "version")
}

func TestSkillRowOriginBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Origin", "origin")
}

func TestSkillRowOriginRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldRejectsWrongShape(t, &value, "Origin", "origin")
}

func TestSkillRowOriginMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldMissingPreservesValue(t, &value, "Origin")
}

func TestSkillRowOriginNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldNullSemantics(t, &value, "Origin", "origin")
}

func TestSkillRowCreatedAtBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldBoundaryRoundTrip(t, &value, "CreatedAt", "createdAt")
}

func TestSkillRowCreatedAtRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldRejectsWrongShape(t, &value, "CreatedAt", "createdAt")
}

func TestSkillRowCreatedAtMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldMissingPreservesValue(t, &value, "CreatedAt")
}

func TestSkillRowCreatedAtNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldNullSemantics(t, &value, "CreatedAt", "createdAt")
}

func TestSkillRowEvolveCountBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldBoundaryRoundTrip(t, &value, "EvolveCount", "evolveCount")
}

func TestSkillRowEvolveCountRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldRejectsWrongShape(t, &value, "EvolveCount", "evolveCount")
}

func TestSkillRowEvolveCountMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldMissingPreservesValue(t, &value, "EvolveCount")
}

func TestSkillRowEvolveCountNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldNullSemantics(t, &value, "EvolveCount", "evolveCount")
}

func TestSkillRowLastEvolvedAtBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldBoundaryRoundTrip(t, &value, "LastEvolvedAt", "lastEvolvedAt")
}

func TestSkillRowLastEvolvedAtRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldRejectsWrongShape(t, &value, "LastEvolvedAt", "lastEvolvedAt")
}

func TestSkillRowLastEvolvedAtMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldMissingPreservesValue(t, &value, "LastEvolvedAt")
}

func TestSkillRowLastEvolvedAtNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldNullSemantics(t, &value, "LastEvolvedAt", "lastEvolvedAt")
}

func TestSkillRowTotalUsesBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldBoundaryRoundTrip(t, &value, "TotalUses", "totalUses")
}

func TestSkillRowTotalUsesRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldRejectsWrongShape(t, &value, "TotalUses", "totalUses")
}

func TestSkillRowTotalUsesMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldMissingPreservesValue(t, &value, "TotalUses")
}

func TestSkillRowTotalUsesNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldNullSemantics(t, &value, "TotalUses", "totalUses")
}

func TestSkillRowLastUsedAtBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldBoundaryRoundTrip(t, &value, "LastUsedAt", "lastUsedAt")
}

func TestSkillRowLastUsedAtRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldRejectsWrongShape(t, &value, "LastUsedAt", "lastUsedAt")
}

func TestSkillRowLastUsedAtMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldMissingPreservesValue(t, &value, "LastUsedAt")
}

func TestSkillRowLastUsedAtNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldNullSemantics(t, &value, "LastUsedAt", "lastUsedAt")
}

func TestSkillRowCuratorStateBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldBoundaryRoundTrip(t, &value, "CuratorState", "curatorState")
}

func TestSkillRowCuratorStateRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldRejectsWrongShape(t, &value, "CuratorState", "curatorState")
}

func TestSkillRowCuratorStateMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldMissingPreservesValue(t, &value, "CuratorState")
}

func TestSkillRowCuratorStateNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldNullSemantics(t, &value, "CuratorState", "curatorState")
}

func TestSkillRowEditableBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Editable", "editable")
}

func TestSkillRowEditableRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldRejectsWrongShape(t, &value, "Editable", "editable")
}

func TestSkillRowEditableMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldMissingPreservesValue(t, &value, "Editable")
}

func TestSkillRowEditableNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldNullSemantics(t, &value, "Editable", "editable")
}

func TestSkillRowDeletableBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Deletable", "deletable")
}

func TestSkillRowDeletableRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldRejectsWrongShape(t, &value, "Deletable", "deletable")
}

func TestSkillRowDeletableMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldMissingPreservesValue(t, &value, "Deletable")
}

func TestSkillRowDeletableNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldNullSemantics(t, &value, "Deletable", "deletable")
}

func TestSkillRowDependencySummaryBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldBoundaryRoundTrip(t, &value, "DependencySummary", "dependencySummary")
}

func TestSkillRowDependencySummaryRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldRejectsWrongShape(t, &value, "DependencySummary", "dependencySummary")
}

func TestSkillRowDependencySummaryMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldMissingPreservesValue(t, &value, "DependencySummary")
}

func TestSkillRowDependencySummaryNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldNullSemantics(t, &value, "DependencySummary", "dependencySummary")
}

func TestSkillRowInstallSummaryBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldBoundaryRoundTrip(t, &value, "InstallSummary", "installSummary")
}

func TestSkillRowInstallSummaryRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldRejectsWrongShape(t, &value, "InstallSummary", "installSummary")
}

func TestSkillRowInstallSummaryMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldMissingPreservesValue(t, &value, "InstallSummary")
}

func TestSkillRowInstallSummaryNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldNullSemantics(t, &value, "InstallSummary", "installSummary")
}

func TestSkillsListResponseSkillsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillsListResponse
	assertWireFieldBoundaryRoundTrip(t, &value, "Skills", "skills")
}

func TestSkillsListResponseSkillsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillsListResponse
	assertWireFieldRejectsWrongShape(t, &value, "Skills", "skills")
}

func TestSkillsListResponseSkillsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillsListResponse
	assertWireFieldMissingPreservesValue(t, &value, "Skills")
}

func TestSkillsListResponseSkillsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillsListResponse
	assertWireFieldNullSemantics(t, &value, "Skills", "skills")
}

func TestSkillsListResponseCountBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillsListResponse
	assertWireFieldBoundaryRoundTrip(t, &value, "Count", "count")
}

func TestSkillsListResponseCountRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillsListResponse
	assertWireFieldRejectsWrongShape(t, &value, "Count", "count")
}

func TestSkillsListResponseCountMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillsListResponse
	assertWireFieldMissingPreservesValue(t, &value, "Count")
}

func TestSkillsListResponseCountNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillsListResponse
	assertWireFieldNullSemantics(t, &value, "Count", "count")
}

func TestSkillLifecycleEventTypeBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldBoundaryRoundTrip(t, &value, "Type", "type")
}

func TestSkillLifecycleEventTypeRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldRejectsWrongShape(t, &value, "Type", "type")
}

func TestSkillLifecycleEventTypeMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldMissingPreservesValue(t, &value, "Type")
}

func TestSkillLifecycleEventTypeNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldNullSemantics(t, &value, "Type", "type")
}

func TestSkillLifecycleEventSkillNameBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldBoundaryRoundTrip(t, &value, "SkillName", "skillName")
}

func TestSkillLifecycleEventSkillNameRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldRejectsWrongShape(t, &value, "SkillName", "skillName")
}

func TestSkillLifecycleEventSkillNameMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldMissingPreservesValue(t, &value, "SkillName")
}

func TestSkillLifecycleEventSkillNameNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldNullSemantics(t, &value, "SkillName", "skillName")
}

func TestSkillLifecycleEventAtBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldBoundaryRoundTrip(t, &value, "At", "at")
}

func TestSkillLifecycleEventAtRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldRejectsWrongShape(t, &value, "At", "at")
}

func TestSkillLifecycleEventAtMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldMissingPreservesValue(t, &value, "At")
}

func TestSkillLifecycleEventAtNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldNullSemantics(t, &value, "At", "at")
}

func TestSkillLifecycleEventVersionBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldBoundaryRoundTrip(t, &value, "Version", "version")
}

func TestSkillLifecycleEventVersionRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldRejectsWrongShape(t, &value, "Version", "version")
}

func TestSkillLifecycleEventVersionMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldMissingPreservesValue(t, &value, "Version")
}

func TestSkillLifecycleEventVersionNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldNullSemantics(t, &value, "Version", "version")
}

func TestSkillLifecycleEventDetailBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldBoundaryRoundTrip(t, &value, "Detail", "detail")
}

func TestSkillLifecycleEventDetailRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldRejectsWrongShape(t, &value, "Detail", "detail")
}

func TestSkillLifecycleEventDetailMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldMissingPreservesValue(t, &value, "Detail")
}

func TestSkillLifecycleEventDetailNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldNullSemantics(t, &value, "Detail", "detail")
}

func TestSkillLifecycleEventRouteBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldBoundaryRoundTrip(t, &value, "Route", "route")
}

func TestSkillLifecycleEventRouteRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldRejectsWrongShape(t, &value, "Route", "route")
}

func TestSkillLifecycleEventRouteMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldMissingPreservesValue(t, &value, "Route")
}

func TestSkillLifecycleEventRouteNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldNullSemantics(t, &value, "Route", "route")
}

func TestSkillLifecycleEventEvidenceBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldBoundaryRoundTrip(t, &value, "Evidence", "evidence")
}

func TestSkillLifecycleEventEvidenceRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldRejectsWrongShape(t, &value, "Evidence", "evidence")
}

func TestSkillLifecycleEventEvidenceMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldMissingPreservesValue(t, &value, "Evidence")
}

func TestSkillLifecycleEventEvidenceNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldNullSemantics(t, &value, "Evidence", "evidence")
}

func TestSkillLifecycleEventTargetSignatureBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldBoundaryRoundTrip(t, &value, "TargetSignature", "targetSignature")
}

func TestSkillLifecycleEventTargetSignatureRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldRejectsWrongShape(t, &value, "TargetSignature", "targetSignature")
}

func TestSkillLifecycleEventTargetSignatureMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldMissingPreservesValue(t, &value, "TargetSignature")
}

func TestSkillLifecycleEventTargetSignatureNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldNullSemantics(t, &value, "TargetSignature", "targetSignature")
}

func TestSkillLifecycleEventEditedSurfaceBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldBoundaryRoundTrip(t, &value, "EditedSurface", "editedSurface")
}

func TestSkillLifecycleEventEditedSurfaceRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldRejectsWrongShape(t, &value, "EditedSurface", "editedSurface")
}

func TestSkillLifecycleEventEditedSurfaceMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldMissingPreservesValue(t, &value, "EditedSurface")
}

func TestSkillLifecycleEventEditedSurfaceNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldNullSemantics(t, &value, "EditedSurface", "editedSurface")
}

func TestSkillLifecycleEventExpectedBehaviorChangeBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldBoundaryRoundTrip(t, &value, "ExpectedBehaviorChange", "expectedBehaviorChange")
}

func TestSkillLifecycleEventExpectedBehaviorChangeRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldRejectsWrongShape(t, &value, "ExpectedBehaviorChange", "expectedBehaviorChange")
}

func TestSkillLifecycleEventExpectedBehaviorChangeMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldMissingPreservesValue(t, &value, "ExpectedBehaviorChange")
}

func TestSkillLifecycleEventExpectedBehaviorChangeNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldNullSemantics(t, &value, "ExpectedBehaviorChange", "expectedBehaviorChange")
}

func TestSkillLifecycleEventRegressionRiskBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldBoundaryRoundTrip(t, &value, "RegressionRisk", "regressionRisk")
}

func TestSkillLifecycleEventRegressionRiskRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldRejectsWrongShape(t, &value, "RegressionRisk", "regressionRisk")
}

func TestSkillLifecycleEventRegressionRiskMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldMissingPreservesValue(t, &value, "RegressionRisk")
}

func TestSkillLifecycleEventRegressionRiskNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldNullSemantics(t, &value, "RegressionRisk", "regressionRisk")
}

func TestPropusLifecycleSummarySystemBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldBoundaryRoundTrip(t, &value, "System", "system")
}

func TestPropusLifecycleSummarySystemRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldRejectsWrongShape(t, &value, "System", "system")
}

func TestPropusLifecycleSummarySystemMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldMissingPreservesValue(t, &value, "System")
}

func TestPropusLifecycleSummarySystemNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldNullSemantics(t, &value, "System", "system")
}

func TestPropusLifecycleSummaryStateBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldBoundaryRoundTrip(t, &value, "State", "state")
}

func TestPropusLifecycleSummaryStateRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldRejectsWrongShape(t, &value, "State", "state")
}

func TestPropusLifecycleSummaryStateMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldMissingPreservesValue(t, &value, "State")
}

func TestPropusLifecycleSummaryStateNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldNullSemantics(t, &value, "State", "state")
}

func TestPropusLifecycleSummaryTotalBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldBoundaryRoundTrip(t, &value, "Total", "total")
}

func TestPropusLifecycleSummaryTotalRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldRejectsWrongShape(t, &value, "Total", "total")
}

func TestPropusLifecycleSummaryTotalMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldMissingPreservesValue(t, &value, "Total")
}

func TestPropusLifecycleSummaryTotalNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldNullSemantics(t, &value, "Total", "total")
}

func TestPropusLifecycleSummaryGenesisBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldBoundaryRoundTrip(t, &value, "Genesis", "genesis")
}

func TestPropusLifecycleSummaryGenesisRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldRejectsWrongShape(t, &value, "Genesis", "genesis")
}

func TestPropusLifecycleSummaryGenesisMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldMissingPreservesValue(t, &value, "Genesis")
}

func TestPropusLifecycleSummaryGenesisNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldNullSemantics(t, &value, "Genesis", "genesis")
}

func TestPropusLifecycleSummaryEvolvedBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldBoundaryRoundTrip(t, &value, "Evolved", "evolved")
}

func TestPropusLifecycleSummaryEvolvedRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldRejectsWrongShape(t, &value, "Evolved", "evolved")
}

func TestPropusLifecycleSummaryEvolvedMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldMissingPreservesValue(t, &value, "Evolved")
}

func TestPropusLifecycleSummaryEvolvedNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldNullSemantics(t, &value, "Evolved", "evolved")
}

func TestPropusLifecycleSummaryReviewBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldBoundaryRoundTrip(t, &value, "Review", "review")
}

func TestPropusLifecycleSummaryReviewRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldRejectsWrongShape(t, &value, "Review", "review")
}

func TestPropusLifecycleSummaryReviewMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldMissingPreservesValue(t, &value, "Review")
}

func TestPropusLifecycleSummaryReviewNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldNullSemantics(t, &value, "Review", "review")
}

func TestPropusLifecycleSummaryRejectedBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldBoundaryRoundTrip(t, &value, "Rejected", "rejected")
}

func TestPropusLifecycleSummaryRejectedRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldRejectsWrongShape(t, &value, "Rejected", "rejected")
}

func TestPropusLifecycleSummaryRejectedMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldMissingPreservesValue(t, &value, "Rejected")
}

func TestPropusLifecycleSummaryRejectedNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldNullSemantics(t, &value, "Rejected", "rejected")
}

func TestPropusLifecycleSummaryRolledBackBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldBoundaryRoundTrip(t, &value, "RolledBack", "rolledBack")
}

func TestPropusLifecycleSummaryRolledBackRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldRejectsWrongShape(t, &value, "RolledBack", "rolledBack")
}

func TestPropusLifecycleSummaryRolledBackMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldMissingPreservesValue(t, &value, "RolledBack")
}

func TestPropusLifecycleSummaryRolledBackNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldNullSemantics(t, &value, "RolledBack", "rolledBack")
}

func TestPropusLifecycleSummaryAttentionBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldBoundaryRoundTrip(t, &value, "Attention", "attention")
}

func TestPropusLifecycleSummaryAttentionRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldRejectsWrongShape(t, &value, "Attention", "attention")
}

func TestPropusLifecycleSummaryAttentionMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldMissingPreservesValue(t, &value, "Attention")
}

func TestPropusLifecycleSummaryAttentionNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldNullSemantics(t, &value, "Attention", "attention")
}

func TestPropusLifecycleSummaryLatestAtBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldBoundaryRoundTrip(t, &value, "LatestAt", "latestAt")
}

func TestPropusLifecycleSummaryLatestAtRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldRejectsWrongShape(t, &value, "LatestAt", "latestAt")
}

func TestPropusLifecycleSummaryLatestAtMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldMissingPreservesValue(t, &value, "LatestAt")
}

func TestPropusLifecycleSummaryLatestAtNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldNullSemantics(t, &value, "LatestAt", "latestAt")
}

func TestPropusLifecycleSummaryLatestTypeBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldBoundaryRoundTrip(t, &value, "LatestType", "latestType")
}

func TestPropusLifecycleSummaryLatestTypeRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldRejectsWrongShape(t, &value, "LatestType", "latestType")
}

func TestPropusLifecycleSummaryLatestTypeMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldMissingPreservesValue(t, &value, "LatestType")
}

func TestPropusLifecycleSummaryLatestTypeNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldNullSemantics(t, &value, "LatestType", "latestType")
}

func TestPropusLifecycleSummaryLatestSkillBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldBoundaryRoundTrip(t, &value, "LatestSkill", "latestSkill")
}

func TestPropusLifecycleSummaryLatestSkillRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldRejectsWrongShape(t, &value, "LatestSkill", "latestSkill")
}

func TestPropusLifecycleSummaryLatestSkillMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldMissingPreservesValue(t, &value, "LatestSkill")
}

func TestPropusLifecycleSummaryLatestSkillNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldNullSemantics(t, &value, "LatestSkill", "latestSkill")
}

func TestPropusLifecycleSummaryDoctrineVersionBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldBoundaryRoundTrip(t, &value, "DoctrineVersion", "doctrineVersion")
}

func TestPropusLifecycleSummaryDoctrineVersionRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldRejectsWrongShape(t, &value, "DoctrineVersion", "doctrineVersion")
}

func TestPropusLifecycleSummaryDoctrineVersionMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldMissingPreservesValue(t, &value, "DoctrineVersion")
}

func TestPropusLifecycleSummaryDoctrineVersionNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldNullSemantics(t, &value, "DoctrineVersion", "doctrineVersion")
}

func TestPropusLifecycleSummaryDoctrineBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldBoundaryRoundTrip(t, &value, "Doctrine", "doctrine")
}

func TestPropusLifecycleSummaryDoctrineRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldRejectsWrongShape(t, &value, "Doctrine", "doctrine")
}

func TestPropusLifecycleSummaryDoctrineMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldMissingPreservesValue(t, &value, "Doctrine")
}

func TestPropusLifecycleSummaryDoctrineNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldNullSemantics(t, &value, "Doctrine", "doctrine")
}

func TestPropusLifecycleSummarySourcePapersBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldBoundaryRoundTrip(t, &value, "SourcePapers", "sourcePapers")
}

func TestPropusLifecycleSummarySourcePapersRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldRejectsWrongShape(t, &value, "SourcePapers", "sourcePapers")
}

func TestPropusLifecycleSummarySourcePapersMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldMissingPreservesValue(t, &value, "SourcePapers")
}

func TestPropusLifecycleSummarySourcePapersNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldNullSemantics(t, &value, "SourcePapers", "sourcePapers")
}

func TestPropusLifecycleSummaryFilteredSourcesBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldBoundaryRoundTrip(t, &value, "FilteredSources", "filteredSources")
}

func TestPropusLifecycleSummaryFilteredSourcesRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldRejectsWrongShape(t, &value, "FilteredSources", "filteredSources")
}

func TestPropusLifecycleSummaryFilteredSourcesMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldMissingPreservesValue(t, &value, "FilteredSources")
}

func TestPropusLifecycleSummaryFilteredSourcesNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldNullSemantics(t, &value, "FilteredSources", "filteredSources")
}

func TestPropusLifecycleSummaryPrinciplesBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldBoundaryRoundTrip(t, &value, "Principles", "principles")
}

func TestPropusLifecycleSummaryPrinciplesRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldRejectsWrongShape(t, &value, "Principles", "principles")
}

func TestPropusLifecycleSummaryPrinciplesMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldMissingPreservesValue(t, &value, "Principles")
}

func TestPropusLifecycleSummaryPrinciplesNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldNullSemantics(t, &value, "Principles", "principles")
}

func TestPropusLifecycleSummaryQualityGatesBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldBoundaryRoundTrip(t, &value, "QualityGates", "qualityGates")
}

func TestPropusLifecycleSummaryQualityGatesRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldRejectsWrongShape(t, &value, "QualityGates", "qualityGates")
}

func TestPropusLifecycleSummaryQualityGatesMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldMissingPreservesValue(t, &value, "QualityGates")
}

func TestPropusLifecycleSummaryQualityGatesNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldNullSemantics(t, &value, "QualityGates", "qualityGates")
}

func TestPropusLifecycleSummaryNextActionsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldBoundaryRoundTrip(t, &value, "NextActions", "nextActions")
}

func TestPropusLifecycleSummaryNextActionsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldRejectsWrongShape(t, &value, "NextActions", "nextActions")
}

func TestPropusLifecycleSummaryNextActionsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldMissingPreservesValue(t, &value, "NextActions")
}

func TestPropusLifecycleSummaryNextActionsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldNullSemantics(t, &value, "NextActions", "nextActions")
}

func TestPropusLifecycleSummaryCoverageStateBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldBoundaryRoundTrip(t, &value, "CoverageState", "coverageState")
}

func TestPropusLifecycleSummaryCoverageStateRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldRejectsWrongShape(t, &value, "CoverageState", "coverageState")
}

func TestPropusLifecycleSummaryCoverageStateMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldMissingPreservesValue(t, &value, "CoverageState")
}

func TestPropusLifecycleSummaryCoverageStateNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldNullSemantics(t, &value, "CoverageState", "coverageState")
}

func TestPropusLifecycleSummaryCoverageGapsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldBoundaryRoundTrip(t, &value, "CoverageGaps", "coverageGaps")
}

func TestPropusLifecycleSummaryCoverageGapsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldRejectsWrongShape(t, &value, "CoverageGaps", "coverageGaps")
}

func TestPropusLifecycleSummaryCoverageGapsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldMissingPreservesValue(t, &value, "CoverageGaps")
}

func TestPropusLifecycleSummaryCoverageGapsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldNullSemantics(t, &value, "CoverageGaps", "coverageGaps")
}

func TestPropusLifecycleSummaryNextCueBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldBoundaryRoundTrip(t, &value, "NextCue", "nextCue")
}

func TestPropusLifecycleSummaryNextCueRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldRejectsWrongShape(t, &value, "NextCue", "nextCue")
}

func TestPropusLifecycleSummaryNextCueMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldMissingPreservesValue(t, &value, "NextCue")
}

func TestPropusLifecycleSummaryNextCueNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldNullSemantics(t, &value, "NextCue", "nextCue")
}

func TestPropusLifecycleSummaryQualityGateBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldBoundaryRoundTrip(t, &value, "QualityGate", "qualityGate")
}

func TestPropusLifecycleSummaryQualityGateRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldRejectsWrongShape(t, &value, "QualityGate", "qualityGate")
}

func TestPropusLifecycleSummaryQualityGateMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldMissingPreservesValue(t, &value, "QualityGate")
}

func TestPropusLifecycleSummaryQualityGateNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldNullSemantics(t, &value, "QualityGate", "qualityGate")
}

func TestPropusLifecycleSummaryAttentionCueBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldBoundaryRoundTrip(t, &value, "AttentionCue", "attentionCue")
}

func TestPropusLifecycleSummaryAttentionCueRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldRejectsWrongShape(t, &value, "AttentionCue", "attentionCue")
}

func TestPropusLifecycleSummaryAttentionCueMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldMissingPreservesValue(t, &value, "AttentionCue")
}

func TestPropusLifecycleSummaryAttentionCueNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldNullSemantics(t, &value, "AttentionCue", "attentionCue")
}

func TestSkillsLifecycleResponseEventsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillsLifecycleResponse
	assertWireFieldBoundaryRoundTrip(t, &value, "Events", "events")
}

func TestSkillsLifecycleResponseEventsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillsLifecycleResponse
	assertWireFieldRejectsWrongShape(t, &value, "Events", "events")
}

func TestSkillsLifecycleResponseEventsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillsLifecycleResponse
	assertWireFieldMissingPreservesValue(t, &value, "Events")
}

func TestSkillsLifecycleResponseEventsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillsLifecycleResponse
	assertWireFieldNullSemantics(t, &value, "Events", "events")
}

func TestSkillsLifecycleResponseCountBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillsLifecycleResponse
	assertWireFieldBoundaryRoundTrip(t, &value, "Count", "count")
}

func TestSkillsLifecycleResponseCountRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillsLifecycleResponse
	assertWireFieldRejectsWrongShape(t, &value, "Count", "count")
}

func TestSkillsLifecycleResponseCountMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillsLifecycleResponse
	assertWireFieldMissingPreservesValue(t, &value, "Count")
}

func TestSkillsLifecycleResponseCountNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillsLifecycleResponse
	assertWireFieldNullSemantics(t, &value, "Count", "count")
}

func TestSkillsLifecycleResponseSummaryBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillsLifecycleResponse
	assertWireFieldBoundaryRoundTrip(t, &value, "Summary", "summary")
}

func TestSkillsLifecycleResponseSummaryRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillsLifecycleResponse
	assertWireFieldRejectsWrongShape(t, &value, "Summary", "summary")
}

func TestSkillsLifecycleResponseSummaryMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillsLifecycleResponse
	assertWireFieldMissingPreservesValue(t, &value, "Summary")
}

func TestSkillsLifecycleResponseSummaryNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillsLifecycleResponse
	assertWireFieldNullSemantics(t, &value, "Summary", "summary")
}

func TestSkillDetailResponseSkillBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillDetailResponse
	assertWireFieldBoundaryRoundTrip(t, &value, "Skill", "skill")
}

func TestSkillDetailResponseSkillRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillDetailResponse
	assertWireFieldRejectsWrongShape(t, &value, "Skill", "skill")
}

func TestSkillDetailResponseSkillMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillDetailResponse
	assertWireFieldMissingPreservesValue(t, &value, "Skill")
}

func TestSkillDetailResponseSkillNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillDetailResponse
	assertWireFieldNullSemantics(t, &value, "Skill", "skill")
}

func TestSkillDetailResponseBodyBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillDetailResponse
	assertWireFieldBoundaryRoundTrip(t, &value, "Body", "body")
}

func TestSkillDetailResponseBodyRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillDetailResponse
	assertWireFieldRejectsWrongShape(t, &value, "Body", "body")
}

func TestSkillDetailResponseBodyMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillDetailResponse
	assertWireFieldMissingPreservesValue(t, &value, "Body")
}

func TestSkillDetailResponseBodyNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillDetailResponse
	assertWireFieldNullSemantics(t, &value, "Body", "body")
}

func TestSkillDetailResponseBodyTruncatedBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillDetailResponse
	assertWireFieldBoundaryRoundTrip(t, &value, "BodyTruncated", "bodyTruncated")
}

func TestSkillDetailResponseBodyTruncatedRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillDetailResponse
	assertWireFieldRejectsWrongShape(t, &value, "BodyTruncated", "bodyTruncated")
}

func TestSkillDetailResponseBodyTruncatedMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillDetailResponse
	assertWireFieldMissingPreservesValue(t, &value, "BodyTruncated")
}

func TestSkillDetailResponseBodyTruncatedNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillDetailResponse
	assertWireFieldNullSemantics(t, &value, "BodyTruncated", "bodyTruncated")
}

func TestSkillDetailResponsePathBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SkillDetailResponse
	assertWireFieldBoundaryRoundTrip(t, &value, "Path", "path")
}

func TestSkillDetailResponsePathRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SkillDetailResponse
	assertWireFieldRejectsWrongShape(t, &value, "Path", "path")
}

func TestSkillDetailResponsePathMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SkillDetailResponse
	assertWireFieldMissingPreservesValue(t, &value, "Path")
}

func TestSkillDetailResponsePathNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SkillDetailResponse
	assertWireFieldNullSemantics(t, &value, "Path", "path")
}

func TestWormholeStatusOutReachableBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value WormholeStatusOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Reachable", "reachable")
}

func TestWormholeStatusOutReachableRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value WormholeStatusOut
	assertWireFieldRejectsWrongShape(t, &value, "Reachable", "reachable")
}

func TestWormholeStatusOutReachableMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value WormholeStatusOut
	assertWireFieldMissingPreservesValue(t, &value, "Reachable")
}

func TestWormholeStatusOutReachableNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value WormholeStatusOut
	assertWireFieldNullSemantics(t, &value, "Reachable", "reachable")
}

func TestWormholeStatusOutListenBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value WormholeStatusOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Listen", "listen")
}

func TestWormholeStatusOutListenRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value WormholeStatusOut
	assertWireFieldRejectsWrongShape(t, &value, "Listen", "listen")
}

func TestWormholeStatusOutListenMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value WormholeStatusOut
	assertWireFieldMissingPreservesValue(t, &value, "Listen")
}

func TestWormholeStatusOutListenNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value WormholeStatusOut
	assertWireFieldNullSemantics(t, &value, "Listen", "listen")
}

func TestWormholeStatusOutLocalOnlyBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value WormholeStatusOut
	assertWireFieldBoundaryRoundTrip(t, &value, "LocalOnly", "localOnly")
}

func TestWormholeStatusOutLocalOnlyRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value WormholeStatusOut
	assertWireFieldRejectsWrongShape(t, &value, "LocalOnly", "localOnly")
}

func TestWormholeStatusOutLocalOnlyMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value WormholeStatusOut
	assertWireFieldMissingPreservesValue(t, &value, "LocalOnly")
}

func TestWormholeStatusOutLocalOnlyNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value WormholeStatusOut
	assertWireFieldNullSemantics(t, &value, "LocalOnly", "localOnly")
}

func TestWormholeStatusOutEffortRoutingBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value WormholeStatusOut
	assertWireFieldBoundaryRoundTrip(t, &value, "EffortRouting", "effortRouting")
}

func TestWormholeStatusOutEffortRoutingRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value WormholeStatusOut
	assertWireFieldRejectsWrongShape(t, &value, "EffortRouting", "effortRouting")
}

func TestWormholeStatusOutEffortRoutingMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value WormholeStatusOut
	assertWireFieldMissingPreservesValue(t, &value, "EffortRouting")
}

func TestWormholeStatusOutEffortRoutingNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value WormholeStatusOut
	assertWireFieldNullSemantics(t, &value, "EffortRouting", "effortRouting")
}

func TestWormholeStatusOutAutoBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value WormholeStatusOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Auto", "auto")
}

func TestWormholeStatusOutAutoRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value WormholeStatusOut
	assertWireFieldRejectsWrongShape(t, &value, "Auto", "auto")
}

func TestWormholeStatusOutAutoMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value WormholeStatusOut
	assertWireFieldMissingPreservesValue(t, &value, "Auto")
}

func TestWormholeStatusOutAutoNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value WormholeStatusOut
	assertWireFieldNullSemantics(t, &value, "Auto", "auto")
}

func TestWormholeStatusOutModelsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value WormholeStatusOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Models", "models")
}

func TestWormholeStatusOutModelsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value WormholeStatusOut
	assertWireFieldRejectsWrongShape(t, &value, "Models", "models")
}

func TestWormholeStatusOutModelsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value WormholeStatusOut
	assertWireFieldMissingPreservesValue(t, &value, "Models")
}

func TestWormholeStatusOutModelsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value WormholeStatusOut
	assertWireFieldNullSemantics(t, &value, "Models", "models")
}

func TestWormholeModelOutNameBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value WormholeModelOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Name", "name")
}

func TestWormholeModelOutNameRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value WormholeModelOut
	assertWireFieldRejectsWrongShape(t, &value, "Name", "name")
}

func TestWormholeModelOutNameMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value WormholeModelOut
	assertWireFieldMissingPreservesValue(t, &value, "Name")
}

func TestWormholeModelOutNameNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value WormholeModelOut
	assertWireFieldNullSemantics(t, &value, "Name", "name")
}

func TestWormholeModelOutProtocolBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value WormholeModelOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Protocol", "protocol")
}

func TestWormholeModelOutProtocolRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value WormholeModelOut
	assertWireFieldRejectsWrongShape(t, &value, "Protocol", "protocol")
}

func TestWormholeModelOutProtocolMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value WormholeModelOut
	assertWireFieldMissingPreservesValue(t, &value, "Protocol")
}

func TestWormholeModelOutProtocolNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value WormholeModelOut
	assertWireFieldNullSemantics(t, &value, "Protocol", "protocol")
}

func TestWormholeModelOutLocalBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value WormholeModelOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Local", "local")
}

func TestWormholeModelOutLocalRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value WormholeModelOut
	assertWireFieldRejectsWrongShape(t, &value, "Local", "local")
}

func TestWormholeModelOutLocalMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value WormholeModelOut
	assertWireFieldMissingPreservesValue(t, &value, "Local")
}

func TestWormholeModelOutLocalNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value WormholeModelOut
	assertWireFieldNullSemantics(t, &value, "Local", "local")
}

func TestWormholeModelOutThinkingBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value WormholeModelOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Thinking", "thinking")
}

func TestWormholeModelOutThinkingRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value WormholeModelOut
	assertWireFieldRejectsWrongShape(t, &value, "Thinking", "thinking")
}

func TestWormholeModelOutThinkingMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value WormholeModelOut
	assertWireFieldMissingPreservesValue(t, &value, "Thinking")
}

func TestWormholeModelOutThinkingNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value WormholeModelOut
	assertWireFieldNullSemantics(t, &value, "Thinking", "thinking")
}

func TestWormholeModelOutSourceBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value WormholeModelOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Source", "source")
}

func TestWormholeModelOutSourceRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value WormholeModelOut
	assertWireFieldRejectsWrongShape(t, &value, "Source", "source")
}

func TestWormholeModelOutSourceMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value WormholeModelOut
	assertWireFieldMissingPreservesValue(t, &value, "Source")
}

func TestWormholeModelOutSourceNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value WormholeModelOut
	assertWireFieldNullSemantics(t, &value, "Source", "source")
}

func TestWormholeModelOutKeyHealthBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value WormholeModelOut
	assertWireFieldBoundaryRoundTrip(t, &value, "KeyHealth", "keyHealth")
}

func TestWormholeModelOutKeyHealthRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value WormholeModelOut
	assertWireFieldRejectsWrongShape(t, &value, "KeyHealth", "keyHealth")
}

func TestWormholeModelOutKeyHealthMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value WormholeModelOut
	assertWireFieldMissingPreservesValue(t, &value, "KeyHealth")
}

func TestWormholeModelOutKeyHealthNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value WormholeModelOut
	assertWireFieldNullSemantics(t, &value, "KeyHealth", "keyHealth")
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

func TestContactRowPhonesOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value ContactRow
	assertWireFieldOmitsZeroValue(t, &value, "phones")
}

func TestContactRowEmailsOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value ContactRow
	assertWireFieldOmitsZeroValue(t, &value, "emails")
}

func TestContactRowOrgOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value ContactRow
	assertWireFieldOmitsZeroValue(t, &value, "org")
}

func TestDashboardItemSubtitleOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value DashboardItem
	assertWireFieldOmitsZeroValue(t, &value, "subtitle")
}

func TestDashboardItemRefTypeOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value DashboardItem
	assertWireFieldOmitsZeroValue(t, &value, "refType")
}

func TestDashboardItemRefIDOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value DashboardItem
	assertWireFieldOmitsZeroValue(t, &value, "refId")
}

func TestDashboardItemWhenMsOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value DashboardItem
	assertWireFieldOmitsZeroValue(t, &value, "whenMs")
}

func TestMailRowOutMailboxOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldOmitsZeroValue(t, &value, "mailbox")
}

func TestMailRowOutHasAttachmentOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldOmitsZeroValue(t, &value, "hasAttachment")
}

func TestMailRowOutAttachmentCountOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldOmitsZeroValue(t, &value, "attachmentCount")
}

func TestMailRowOutPriorityOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldOmitsZeroValue(t, &value, "priority")
}

func TestMailRowOutPriorityHintOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldOmitsZeroValue(t, &value, "priorityHint")
}

func TestMailRowOutAnalysisStatusOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldOmitsZeroValue(t, &value, "analysisStatus")
}

func TestMailRowOutAnalysisQualityOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldOmitsZeroValue(t, &value, "analysisQuality")
}

func TestMailRowOutFeedStatusOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldOmitsZeroValue(t, &value, "feedStatus")
}

func TestMailRowOutCalendarProposalCountOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldOmitsZeroValue(t, &value, "calendarProposalCount")
}

func TestMailRowOutTodoCountOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldOmitsZeroValue(t, &value, "todoCount")
}

func TestMailRowOutWorkStateHintOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldOmitsZeroValue(t, &value, "workStateHint")
}

func TestMailRowOutRelatedProjectsOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailRowOut
	assertWireFieldOmitsZeroValue(t, &value, "relatedProjects")
}

func TestMailMessageOutCCOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldOmitsZeroValue(t, &value, "cc")
}

func TestMailMessageOutRawBodyOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldOmitsZeroValue(t, &value, "rawBody")
}

func TestMailMessageOutRawBodyTotalOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldOmitsZeroValue(t, &value, "rawBodyTotal")
}

func TestMailMessageOutBodyCleanedOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldOmitsZeroValue(t, &value, "bodyCleaned")
}

func TestMailMessageOutBodyHiddenBlockCountOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldOmitsZeroValue(t, &value, "bodyHiddenBlockCount")
}

func TestMailMessageOutBodyHiddenLineCountOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldOmitsZeroValue(t, &value, "bodyHiddenLineCount")
}

func TestMailMessageOutAnalysisStatusOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldOmitsZeroValue(t, &value, "analysisStatus")
}

func TestMailMessageOutAnalysisQualityOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldOmitsZeroValue(t, &value, "analysisQuality")
}

func TestMailMessageOutFeedStatusOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldOmitsZeroValue(t, &value, "feedStatus")
}

func TestMailMessageOutCalendarProposalCountOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldOmitsZeroValue(t, &value, "calendarProposalCount")
}

func TestMailMessageOutTodoCountOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldOmitsZeroValue(t, &value, "todoCount")
}

func TestMailMessageOutWorkStateHintOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldOmitsZeroValue(t, &value, "workStateHint")
}

func TestMailMessageOutRelatedProjectsOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailMessageOut
	assertWireFieldOmitsZeroValue(t, &value, "relatedProjects")
}

func TestMailNativeStatusOutGeneratedAtOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldOmitsZeroValue(t, &value, "generatedAt")
}

func TestMailNativeStatusOutErrorOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value MailNativeStatusOut
	assertWireFieldOmitsZeroValue(t, &value, "error")
}

func TestProjectRefTitleOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value ProjectRef
	assertWireFieldOmitsZeroValue(t, &value, "title")
}

func TestProjectRefSummaryOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value ProjectRef
	assertWireFieldOmitsZeroValue(t, &value, "summary")
}

func TestMailAnalysisOutSubjectOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldOmitsZeroValue(t, &value, "subject")
}

func TestMailAnalysisOutFromOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldOmitsZeroValue(t, &value, "from")
}

func TestMailAnalysisOutDateOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldOmitsZeroValue(t, &value, "date")
}

func TestMailAnalysisOutRelatedProjectsOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldOmitsZeroValue(t, &value, "relatedProjects")
}

func TestMailAnalysisOutAnalysisStatusOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldOmitsZeroValue(t, &value, "analysisStatus")
}

func TestMailAnalysisOutAnalysisQualityOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldOmitsZeroValue(t, &value, "analysisQuality")
}

func TestMailAnalysisOutFeedStatusOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldOmitsZeroValue(t, &value, "feedStatus")
}

func TestMailAnalysisOutCalendarProposalCountOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldOmitsZeroValue(t, &value, "calendarProposalCount")
}

func TestMailAnalysisOutTodoCountOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldOmitsZeroValue(t, &value, "todoCount")
}

func TestMailAnalysisOutWorkStateHintOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MailAnalysisOut
	assertWireFieldOmitsZeroValue(t, &value, "workStateHint")
}

func TestSenderWikiHitOutTitleOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SenderWikiHitOut
	assertWireFieldOmitsZeroValue(t, &value, "title")
}

func TestSenderWikiHitOutSummaryOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SenderWikiHitOut
	assertWireFieldOmitsZeroValue(t, &value, "summary")
}

func TestSenderWikiHitOutCategoryOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SenderWikiHitOut
	assertWireFieldOmitsZeroValue(t, &value, "category")
}

func TestSenderRecentOutLastReceivedAtOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SenderRecentOut
	assertWireFieldOmitsZeroValue(t, &value, "lastReceivedAt")
}

func TestSenderRecentOutTruncatedOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value SenderRecentOut
	assertWireFieldOmitsZeroValue(t, &value, "truncated")
}

func TestModelOptionProviderOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldOmitsZeroValue(t, &value, "provider")
}

func TestModelOptionDisplayOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldOmitsZeroValue(t, &value, "display")
}

func TestModelOptionHealthOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldOmitsZeroValue(t, &value, "health")
}

func TestModelOptionCustomOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldOmitsZeroValue(t, &value, "custom")
}

func TestModelOptionDeletableOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldOmitsZeroValue(t, &value, "deletable")
}

func TestModelOptionUnhealthyOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldOmitsZeroValue(t, &value, "unhealthy")
}

func TestModelOptionNoteOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value ModelOption
	assertWireFieldOmitsZeroValue(t, &value, "note")
}

func TestModelDeleteResultClearedRolesOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value ModelDeleteResult
	assertWireFieldOmitsZeroValue(t, &value, "clearedRoles")
}

func TestModelsListResultAdvisoriesOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value ModelsListResult
	assertWireFieldOmitsZeroValue(t, &value, "advisories")
}

func TestMemberOutRankOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MemberOut
	assertWireFieldOmitsZeroValue(t, &value, "rank")
}

func TestMemberOutPositionOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MemberOut
	assertWireFieldOmitsZeroValue(t, &value, "position")
}

func TestMemberOutPhonesOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MemberOut
	assertWireFieldOmitsZeroValue(t, &value, "phones")
}

func TestMemberOutEmailsOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MemberOut
	assertWireFieldOmitsZeroValue(t, &value, "emails")
}

func TestMemberOutPersonPathOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MemberOut
	assertWireFieldOmitsZeroValue(t, &value, "personPath")
}

func TestOrgNodeOutParentIDOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldOmitsZeroValue(t, &value, "parentId")
}

func TestOrgNodeOutLaneOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldOmitsZeroValue(t, &value, "lane")
}

func TestOrgNodeOutMembersOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldOmitsZeroValue(t, &value, "members")
}

func TestOrgNodeOutKeywordsOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldOmitsZeroValue(t, &value, "keywords")
}

func TestOrgNodeOutCompaniesOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value OrgNodeOut
	assertWireFieldOmitsZeroValue(t, &value, "companies")
}

func TestProjectDigestRowHeadlineOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldOmitsZeroValue(t, &value, "headline")
}

func TestProjectDigestRowBulletsOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldOmitsZeroValue(t, &value, "bullets")
}

func TestProjectDigestRowDueOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldOmitsZeroValue(t, &value, "due")
}

func TestProjectDigestRowUpdatedAtMsOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldOmitsZeroValue(t, &value, "updatedAtMs")
}

func TestProjectDigestRowPathOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldOmitsZeroValue(t, &value, "path")
}

func TestProjectDigestRowCodeOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldOmitsZeroValue(t, &value, "code")
}

func TestProjectDigestRowClientOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldOmitsZeroValue(t, &value, "client")
}

func TestProjectDigestRowRefsOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value ProjectDigestRow
	assertWireFieldOmitsZeroValue(t, &value, "refs")
}

func TestPromptTunerReportErrorOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldOmitsZeroValue(t, &value, "error")
}

func TestPromptTunerReportProposedOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldOmitsZeroValue(t, &value, "proposed")
}

func TestPromptTunerReportAddedOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value PromptTunerReport
	assertWireFieldOmitsZeroValue(t, &value, "added")
}

func TestPromptRowDescriptionOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldOmitsZeroValue(t, &value, "description")
}

func TestPromptRowCategoryOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldOmitsZeroValue(t, &value, "category")
}

func TestPromptRowUpdatedAtMsOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value PromptRow
	assertWireFieldOmitsZeroValue(t, &value, "updatedAtMs")
}

func TestPromptDetailOutDescriptionOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldOmitsZeroValue(t, &value, "description")
}

func TestPromptDetailOutCategoryOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldOmitsZeroValue(t, &value, "category")
}

func TestPromptDetailOutUpdatedAtMsOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value PromptDetailOut
	assertWireFieldOmitsZeroValue(t, &value, "updatedAtMs")
}

func TestSelfCorrectionCandidateStatusOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldOmitsZeroValue(t, &value, "status")
}

func TestSelfCorrectionCandidateScopeOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldOmitsZeroValue(t, &value, "scope")
}

func TestSelfCorrectionCandidateSkillNameOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldOmitsZeroValue(t, &value, "skillName")
}

func TestSelfCorrectionCandidateSessionKeyOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldOmitsZeroValue(t, &value, "sessionKey")
}

func TestSelfCorrectionCandidateTitleOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldOmitsZeroValue(t, &value, "title")
}

func TestSelfCorrectionCandidateCandidateOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldOmitsZeroValue(t, &value, "candidate")
}

func TestSelfCorrectionCandidateEvidenceOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldOmitsZeroValue(t, &value, "evidence")
}

func TestSelfCorrectionCandidateReasonOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldOmitsZeroValue(t, &value, "reason")
}

func TestSelfCorrectionCandidateTargetFilesOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldOmitsZeroValue(t, &value, "targetFiles")
}

func TestSelfCorrectionCandidateProposedChangeOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldOmitsZeroValue(t, &value, "proposedChange")
}

func TestSelfCorrectionCandidateRiskOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldOmitsZeroValue(t, &value, "risk")
}

func TestSelfCorrectionCandidateSourceOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldOmitsZeroValue(t, &value, "source")
}

func TestSelfCorrectionCandidateReviewerOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldOmitsZeroValue(t, &value, "reviewer")
}

func TestSelfCorrectionCandidateReviewNoteOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldOmitsZeroValue(t, &value, "reviewNote")
}

func TestSelfCorrectionCandidateEvidenceKindsOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldOmitsZeroValue(t, &value, "evidenceKinds")
}

func TestSelfCorrectionCandidateReviewActionsOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldOmitsZeroValue(t, &value, "reviewActions")
}

func TestSelfCorrectionCandidateCreatedAtOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldOmitsZeroValue(t, &value, "createdAt")
}

func TestSelfCorrectionCandidateUpdatedAtOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value SelfCorrectionCandidate
	assertWireFieldOmitsZeroValue(t, &value, "updatedAt")
}

func TestSelfImprovementCodingFunnelLastCaptureAtOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldOmitsZeroValue(t, &value, "lastCaptureAt")
}

func TestSelfImprovementCodingFunnelLastReviewAtOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldOmitsZeroValue(t, &value, "lastReviewAt")
}

func TestSelfImprovementCodingFunnelRejections7dOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldOmitsZeroValue(t, &value, "rejections7d")
}

func TestSelfImprovementCodingFunnelPromotableRejections7dOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldOmitsZeroValue(t, &value, "promotableRejections7d")
}

func TestSelfImprovementCodingFunnelLastRejectionAtOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldOmitsZeroValue(t, &value, "lastRejectionAt")
}

func TestSelfImprovementCodingFunnelLastNudgeAtOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingFunnel
	assertWireFieldOmitsZeroValue(t, &value, "lastNudgeAt")
}

func TestSelfImprovementCodingListResponseStatusCountsOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SelfImprovementCodingListResponse
	assertWireFieldOmitsZeroValue(t, &value, "statusCounts")
}

func TestSessionRowOutKindOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldOmitsZeroValue(t, &value, "kind")
}

func TestSessionRowOutStatusOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldOmitsZeroValue(t, &value, "status")
}

func TestSessionRowOutChannelOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldOmitsZeroValue(t, &value, "channel")
}

func TestSessionRowOutModelOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldOmitsZeroValue(t, &value, "model")
}

func TestSessionRowOutLabelOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldOmitsZeroValue(t, &value, "label")
}

func TestSessionRowOutUpdatedAtMsOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldOmitsZeroValue(t, &value, "updatedAtMs")
}

func TestSessionRowOutStartedAtMsOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldOmitsZeroValue(t, &value, "startedAtMs")
}

func TestSessionRowOutRuntimeMsOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldOmitsZeroValue(t, &value, "runtimeMs")
}

func TestSessionRowOutTotalTokensOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value sessionRowOut
	assertWireFieldOmitsZeroValue(t, &value, "totalTokens")
}

func TestTranscriptMsgOutIDOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value transcriptMsgOut
	assertWireFieldOmitsZeroValue(t, &value, "id")
}

func TestTranscriptMsgOutAttachmentsOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value transcriptMsgOut
	assertWireFieldOmitsZeroValue(t, &value, "attachments")
}

func TestTranscriptMsgOutTimestampMsOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value transcriptMsgOut
	assertWireFieldOmitsZeroValue(t, &value, "timestampMs")
}

func TestSkillRowDescriptionOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldOmitsZeroValue(t, &value, "description")
}

func TestSkillRowCategoryOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldOmitsZeroValue(t, &value, "category")
}

func TestSkillRowHomepageOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldOmitsZeroValue(t, &value, "homepage")
}

func TestSkillRowTagsOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldOmitsZeroValue(t, &value, "tags")
}

func TestSkillRowRelatedSkillsOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldOmitsZeroValue(t, &value, "relatedSkills")
}

func TestSkillRowSourceOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldOmitsZeroValue(t, &value, "source")
}

func TestSkillRowVersionOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldOmitsZeroValue(t, &value, "version")
}

func TestSkillRowOriginOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldOmitsZeroValue(t, &value, "origin")
}

func TestSkillRowCreatedAtOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldOmitsZeroValue(t, &value, "createdAt")
}

func TestSkillRowEvolveCountOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldOmitsZeroValue(t, &value, "evolveCount")
}

func TestSkillRowLastEvolvedAtOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldOmitsZeroValue(t, &value, "lastEvolvedAt")
}

func TestSkillRowTotalUsesOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldOmitsZeroValue(t, &value, "totalUses")
}

func TestSkillRowLastUsedAtOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldOmitsZeroValue(t, &value, "lastUsedAt")
}

func TestSkillRowCuratorStateOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldOmitsZeroValue(t, &value, "curatorState")
}

func TestSkillRowEditableOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldOmitsZeroValue(t, &value, "editable")
}

func TestSkillRowDeletableOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldOmitsZeroValue(t, &value, "deletable")
}

func TestSkillRowDependencySummaryOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldOmitsZeroValue(t, &value, "dependencySummary")
}

func TestSkillRowInstallSummaryOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SkillRow
	assertWireFieldOmitsZeroValue(t, &value, "installSummary")
}

func TestSkillLifecycleEventSkillNameOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldOmitsZeroValue(t, &value, "skillName")
}

func TestSkillLifecycleEventAtOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldOmitsZeroValue(t, &value, "at")
}

func TestSkillLifecycleEventVersionOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldOmitsZeroValue(t, &value, "version")
}

func TestSkillLifecycleEventDetailOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldOmitsZeroValue(t, &value, "detail")
}

func TestSkillLifecycleEventRouteOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldOmitsZeroValue(t, &value, "route")
}

func TestSkillLifecycleEventEvidenceOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldOmitsZeroValue(t, &value, "evidence")
}

func TestSkillLifecycleEventTargetSignatureOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldOmitsZeroValue(t, &value, "targetSignature")
}

func TestSkillLifecycleEventEditedSurfaceOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldOmitsZeroValue(t, &value, "editedSurface")
}

func TestSkillLifecycleEventExpectedBehaviorChangeOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldOmitsZeroValue(t, &value, "expectedBehaviorChange")
}

func TestSkillLifecycleEventRegressionRiskOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SkillLifecycleEvent
	assertWireFieldOmitsZeroValue(t, &value, "regressionRisk")
}

func TestPropusLifecycleSummaryLatestAtOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldOmitsZeroValue(t, &value, "latestAt")
}

func TestPropusLifecycleSummaryLatestTypeOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldOmitsZeroValue(t, &value, "latestType")
}

func TestPropusLifecycleSummaryLatestSkillOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldOmitsZeroValue(t, &value, "latestSkill")
}

func TestPropusLifecycleSummaryDoctrineVersionOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldOmitsZeroValue(t, &value, "doctrineVersion")
}

func TestPropusLifecycleSummaryDoctrineOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldOmitsZeroValue(t, &value, "doctrine")
}

func TestPropusLifecycleSummarySourcePapersOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldOmitsZeroValue(t, &value, "sourcePapers")
}

func TestPropusLifecycleSummaryFilteredSourcesOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldOmitsZeroValue(t, &value, "filteredSources")
}

func TestPropusLifecycleSummaryPrinciplesOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldOmitsZeroValue(t, &value, "principles")
}

func TestPropusLifecycleSummaryQualityGatesOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldOmitsZeroValue(t, &value, "qualityGates")
}

func TestPropusLifecycleSummaryNextActionsOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldOmitsZeroValue(t, &value, "nextActions")
}

func TestPropusLifecycleSummaryCoverageStateOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldOmitsZeroValue(t, &value, "coverageState")
}

func TestPropusLifecycleSummaryCoverageGapsOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldOmitsZeroValue(t, &value, "coverageGaps")
}

func TestPropusLifecycleSummaryNextCueOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldOmitsZeroValue(t, &value, "nextCue")
}

func TestPropusLifecycleSummaryQualityGateOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldOmitsZeroValue(t, &value, "qualityGate")
}

func TestPropusLifecycleSummaryAttentionCueOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value PropusLifecycleSummary
	assertWireFieldOmitsZeroValue(t, &value, "attentionCue")
}

func TestSkillDetailResponseBodyOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SkillDetailResponse
	assertWireFieldOmitsZeroValue(t, &value, "body")
}

func TestSkillDetailResponseBodyTruncatedOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value SkillDetailResponse
	assertWireFieldOmitsZeroValue(t, &value, "bodyTruncated")
}

func TestSkillDetailResponsePathOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SkillDetailResponse
	assertWireFieldOmitsZeroValue(t, &value, "path")
}

func TestWormholeStatusOutListenOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value WormholeStatusOut
	assertWireFieldOmitsZeroValue(t, &value, "listen")
}

func TestWormholeStatusOutAutoOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value WormholeStatusOut
	assertWireFieldOmitsZeroValue(t, &value, "auto")
}

func TestWormholeModelOutKeyHealthOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value WormholeModelOut
	assertWireFieldOmitsZeroValue(t, &value, "keyHealth")
}
