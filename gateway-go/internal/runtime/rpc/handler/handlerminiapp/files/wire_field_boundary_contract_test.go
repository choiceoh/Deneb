package files

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

func TestFilesEntryOutTagBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Tag", "tag")
}

func TestFilesEntryOutTagRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldRejectsWrongShape(t, &value, "Tag", "tag")
}

func TestFilesEntryOutTagMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldMissingPreservesValue(t, &value, "Tag")
}

func TestFilesEntryOutTagNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldNullSemantics(t, &value, "Tag", "tag")
}

func TestFilesEntryOutNameBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Name", "name")
}

func TestFilesEntryOutNameRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldRejectsWrongShape(t, &value, "Name", "name")
}

func TestFilesEntryOutNameMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldMissingPreservesValue(t, &value, "Name")
}

func TestFilesEntryOutNameNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldNullSemantics(t, &value, "Name", "name")
}

func TestFilesEntryOutPathDisplayBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldBoundaryRoundTrip(t, &value, "PathDisplay", "pathDisplay")
}

func TestFilesEntryOutPathDisplayRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldRejectsWrongShape(t, &value, "PathDisplay", "pathDisplay")
}

func TestFilesEntryOutPathDisplayMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldMissingPreservesValue(t, &value, "PathDisplay")
}

func TestFilesEntryOutPathDisplayNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldNullSemantics(t, &value, "PathDisplay", "pathDisplay")
}

func TestFilesEntryOutPathLowerBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldBoundaryRoundTrip(t, &value, "PathLower", "pathLower")
}

func TestFilesEntryOutPathLowerRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldRejectsWrongShape(t, &value, "PathLower", "pathLower")
}

func TestFilesEntryOutPathLowerMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldMissingPreservesValue(t, &value, "PathLower")
}

func TestFilesEntryOutPathLowerNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldNullSemantics(t, &value, "PathLower", "pathLower")
}

func TestFilesEntryOutIDBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldBoundaryRoundTrip(t, &value, "ID", "id")
}

func TestFilesEntryOutIDRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldRejectsWrongShape(t, &value, "ID", "id")
}

func TestFilesEntryOutIDMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldMissingPreservesValue(t, &value, "ID")
}

func TestFilesEntryOutIDNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldNullSemantics(t, &value, "ID", "id")
}

func TestFilesEntryOutSizeBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Size", "size")
}

func TestFilesEntryOutSizeRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldRejectsWrongShape(t, &value, "Size", "size")
}

func TestFilesEntryOutSizeMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldMissingPreservesValue(t, &value, "Size")
}

func TestFilesEntryOutSizeNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldNullSemantics(t, &value, "Size", "size")
}

func TestFilesEntryOutServerModifiedBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldBoundaryRoundTrip(t, &value, "ServerModified", "serverModified")
}

func TestFilesEntryOutServerModifiedRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldRejectsWrongShape(t, &value, "ServerModified", "serverModified")
}

func TestFilesEntryOutServerModifiedMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldMissingPreservesValue(t, &value, "ServerModified")
}

func TestFilesEntryOutServerModifiedNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldNullSemantics(t, &value, "ServerModified", "serverModified")
}

func TestFilesListOutEntriesBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value FilesListOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Entries", "entries")
}

func TestFilesListOutEntriesRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value FilesListOut
	assertWireFieldRejectsWrongShape(t, &value, "Entries", "entries")
}

func TestFilesListOutEntriesMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value FilesListOut
	assertWireFieldMissingPreservesValue(t, &value, "Entries")
}

func TestFilesListOutEntriesNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value FilesListOut
	assertWireFieldNullSemantics(t, &value, "Entries", "entries")
}

func TestFilesListOutPathBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value FilesListOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Path", "path")
}

func TestFilesListOutPathRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value FilesListOut
	assertWireFieldRejectsWrongShape(t, &value, "Path", "path")
}

func TestFilesListOutPathMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value FilesListOut
	assertWireFieldMissingPreservesValue(t, &value, "Path")
}

func TestFilesListOutPathNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value FilesListOut
	assertWireFieldNullSemantics(t, &value, "Path", "path")
}

func TestFilesShareOutURLBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value FilesShareOut
	assertWireFieldBoundaryRoundTrip(t, &value, "URL", "url")
}

func TestFilesShareOutURLRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value FilesShareOut
	assertWireFieldRejectsWrongShape(t, &value, "URL", "url")
}

func TestFilesShareOutURLMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value FilesShareOut
	assertWireFieldMissingPreservesValue(t, &value, "URL")
}

func TestFilesShareOutURLNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value FilesShareOut
	assertWireFieldNullSemantics(t, &value, "URL", "url")
}

func TestFilesUploadOutEntryBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value FilesUploadOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Entry", "entry")
}

func TestFilesUploadOutEntryRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value FilesUploadOut
	assertWireFieldRejectsWrongShape(t, &value, "Entry", "entry")
}

func TestFilesUploadOutEntryMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value FilesUploadOut
	assertWireFieldMissingPreservesValue(t, &value, "Entry")
}

func TestFilesUploadOutEntryNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value FilesUploadOut
	assertWireFieldNullSemantics(t, &value, "Entry", "entry")
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

func TestFilesEntryOutIDZeroValueLeavesKeyMissingFromJSON(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldOmitsZeroValue(t, &value, "id")
}

func TestFilesEntryOutSizeZeroValueLeavesKeyMissingFromJSON(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldOmitsZeroValue(t, &value, "size")
}

func TestFilesEntryOutServerModifiedZeroValueLeavesKeyMissingFromJSON(t *testing.T) {
	t.Parallel()

	var value FilesEntryOut
	assertWireFieldOmitsZeroValue(t, &value, "serverModified")
}
