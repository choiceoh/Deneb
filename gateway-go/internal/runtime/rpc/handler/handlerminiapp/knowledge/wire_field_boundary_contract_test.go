package knowledge

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

func TestMemoryCategoryRowNameBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MemoryCategoryRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Name", "name")
}

func TestMemoryCategoryRowNameRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MemoryCategoryRow
	assertWireFieldRejectsWrongShape(t, &value, "Name", "name")
}

func TestMemoryCategoryRowNameMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MemoryCategoryRow
	assertWireFieldMissingPreservesValue(t, &value, "Name")
}

func TestMemoryCategoryRowNameNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MemoryCategoryRow
	assertWireFieldNullSemantics(t, &value, "Name", "name")
}

func TestMemoryCategoryRowPageCountBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MemoryCategoryRow
	assertWireFieldBoundaryRoundTrip(t, &value, "PageCount", "pageCount")
}

func TestMemoryCategoryRowPageCountRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MemoryCategoryRow
	assertWireFieldRejectsWrongShape(t, &value, "PageCount", "pageCount")
}

func TestMemoryCategoryRowPageCountMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MemoryCategoryRow
	assertWireFieldMissingPreservesValue(t, &value, "PageCount")
}

func TestMemoryCategoryRowPageCountNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MemoryCategoryRow
	assertWireFieldNullSemantics(t, &value, "PageCount", "pageCount")
}

func TestMemoryPageRowPathBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MemoryPageRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Path", "path")
}

func TestMemoryPageRowPathRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MemoryPageRow
	assertWireFieldRejectsWrongShape(t, &value, "Path", "path")
}

func TestMemoryPageRowPathMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MemoryPageRow
	assertWireFieldMissingPreservesValue(t, &value, "Path")
}

func TestMemoryPageRowPathNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MemoryPageRow
	assertWireFieldNullSemantics(t, &value, "Path", "path")
}

func TestMemoryPageRowTitleBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MemoryPageRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Title", "title")
}

func TestMemoryPageRowTitleRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MemoryPageRow
	assertWireFieldRejectsWrongShape(t, &value, "Title", "title")
}

func TestMemoryPageRowTitleMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MemoryPageRow
	assertWireFieldMissingPreservesValue(t, &value, "Title")
}

func TestMemoryPageRowTitleNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MemoryPageRow
	assertWireFieldNullSemantics(t, &value, "Title", "title")
}

func TestMemoryPageRowSummaryBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MemoryPageRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Summary", "summary")
}

func TestMemoryPageRowSummaryRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MemoryPageRow
	assertWireFieldRejectsWrongShape(t, &value, "Summary", "summary")
}

func TestMemoryPageRowSummaryMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MemoryPageRow
	assertWireFieldMissingPreservesValue(t, &value, "Summary")
}

func TestMemoryPageRowSummaryNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MemoryPageRow
	assertWireFieldNullSemantics(t, &value, "Summary", "summary")
}

func TestMemoryPageRowUpdatedBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value MemoryPageRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Updated", "updated")
}

func TestMemoryPageRowUpdatedRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value MemoryPageRow
	assertWireFieldRejectsWrongShape(t, &value, "Updated", "updated")
}

func TestMemoryPageRowUpdatedMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value MemoryPageRow
	assertWireFieldMissingPreservesValue(t, &value, "Updated")
}

func TestMemoryPageRowUpdatedNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value MemoryPageRow
	assertWireFieldNullSemantics(t, &value, "Updated", "updated")
}

func TestNotebookSummaryOutIDBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldBoundaryRoundTrip(t, &value, "ID", "id")
}

func TestNotebookSummaryOutIDRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldRejectsWrongShape(t, &value, "ID", "id")
}

func TestNotebookSummaryOutIDMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldMissingPreservesValue(t, &value, "ID")
}

func TestNotebookSummaryOutIDNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldNullSemantics(t, &value, "ID", "id")
}

func TestNotebookSummaryOutNameBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Name", "name")
}

func TestNotebookSummaryOutNameRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldRejectsWrongShape(t, &value, "Name", "name")
}

func TestNotebookSummaryOutNameMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldMissingPreservesValue(t, &value, "Name")
}

func TestNotebookSummaryOutNameNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldNullSemantics(t, &value, "Name", "name")
}

func TestNotebookSummaryOutDescriptionBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Description", "description")
}

func TestNotebookSummaryOutDescriptionRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldRejectsWrongShape(t, &value, "Description", "description")
}

func TestNotebookSummaryOutDescriptionMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldMissingPreservesValue(t, &value, "Description")
}

func TestNotebookSummaryOutDescriptionNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldNullSemantics(t, &value, "Description", "description")
}

func TestNotebookSummaryOutDealRefBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldBoundaryRoundTrip(t, &value, "DealRef", "dealRef")
}

func TestNotebookSummaryOutDealRefRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldRejectsWrongShape(t, &value, "DealRef", "dealRef")
}

func TestNotebookSummaryOutDealRefMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldMissingPreservesValue(t, &value, "DealRef")
}

func TestNotebookSummaryOutDealRefNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldNullSemantics(t, &value, "DealRef", "dealRef")
}

func TestNotebookSummaryOutProjectRefsBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldBoundaryRoundTrip(t, &value, "ProjectRefs", "projectRefs")
}

func TestNotebookSummaryOutProjectRefsRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldRejectsWrongShape(t, &value, "ProjectRefs", "projectRefs")
}

func TestNotebookSummaryOutProjectRefsMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldMissingPreservesValue(t, &value, "ProjectRefs")
}

func TestNotebookSummaryOutProjectRefsNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldNullSemantics(t, &value, "ProjectRefs", "projectRefs")
}

func TestNotebookSummaryOutSourceCountBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldBoundaryRoundTrip(t, &value, "SourceCount", "sourceCount")
}

func TestNotebookSummaryOutSourceCountRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldRejectsWrongShape(t, &value, "SourceCount", "sourceCount")
}

func TestNotebookSummaryOutSourceCountMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldMissingPreservesValue(t, &value, "SourceCount")
}

func TestNotebookSummaryOutSourceCountNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldNullSemantics(t, &value, "SourceCount", "sourceCount")
}

func TestNotebookSummaryOutUpdatedBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Updated", "updated")
}

func TestNotebookSummaryOutUpdatedRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldRejectsWrongShape(t, &value, "Updated", "updated")
}

func TestNotebookSummaryOutUpdatedMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldMissingPreservesValue(t, &value, "Updated")
}

func TestNotebookSummaryOutUpdatedNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldNullSemantics(t, &value, "Updated", "updated")
}

func TestNotebookListOutNotebooksBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value NotebookListOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Notebooks", "notebooks")
}

func TestNotebookListOutNotebooksRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value NotebookListOut
	assertWireFieldRejectsWrongShape(t, &value, "Notebooks", "notebooks")
}

func TestNotebookListOutNotebooksMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value NotebookListOut
	assertWireFieldMissingPreservesValue(t, &value, "Notebooks")
}

func TestNotebookListOutNotebooksNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value NotebookListOut
	assertWireFieldNullSemantics(t, &value, "Notebooks", "notebooks")
}

func TestNotebookSourceOutCiteBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value NotebookSourceOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Cite", "cite")
}

func TestNotebookSourceOutCiteRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value NotebookSourceOut
	assertWireFieldRejectsWrongShape(t, &value, "Cite", "cite")
}

func TestNotebookSourceOutCiteMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value NotebookSourceOut
	assertWireFieldMissingPreservesValue(t, &value, "Cite")
}

func TestNotebookSourceOutCiteNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value NotebookSourceOut
	assertWireFieldNullSemantics(t, &value, "Cite", "cite")
}

func TestNotebookSourceOutKindBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value NotebookSourceOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Kind", "kind")
}

func TestNotebookSourceOutKindRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value NotebookSourceOut
	assertWireFieldRejectsWrongShape(t, &value, "Kind", "kind")
}

func TestNotebookSourceOutKindMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value NotebookSourceOut
	assertWireFieldMissingPreservesValue(t, &value, "Kind")
}

func TestNotebookSourceOutKindNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value NotebookSourceOut
	assertWireFieldNullSemantics(t, &value, "Kind", "kind")
}

func TestNotebookSourceOutRefBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value NotebookSourceOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Ref", "ref")
}

func TestNotebookSourceOutRefRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value NotebookSourceOut
	assertWireFieldRejectsWrongShape(t, &value, "Ref", "ref")
}

func TestNotebookSourceOutRefMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value NotebookSourceOut
	assertWireFieldMissingPreservesValue(t, &value, "Ref")
}

func TestNotebookSourceOutRefNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value NotebookSourceOut
	assertWireFieldNullSemantics(t, &value, "Ref", "ref")
}

func TestNotebookSourceOutTitleBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value NotebookSourceOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Title", "title")
}

func TestNotebookSourceOutTitleRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value NotebookSourceOut
	assertWireFieldRejectsWrongShape(t, &value, "Title", "title")
}

func TestNotebookSourceOutTitleMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value NotebookSourceOut
	assertWireFieldMissingPreservesValue(t, &value, "Title")
}

func TestNotebookSourceOutTitleNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value NotebookSourceOut
	assertWireFieldNullSemantics(t, &value, "Title", "title")
}

func TestNotebookSourceOutTextBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value NotebookSourceOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Text", "text")
}

func TestNotebookSourceOutTextRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value NotebookSourceOut
	assertWireFieldRejectsWrongShape(t, &value, "Text", "text")
}

func TestNotebookSourceOutTextMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value NotebookSourceOut
	assertWireFieldMissingPreservesValue(t, &value, "Text")
}

func TestNotebookSourceOutTextNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value NotebookSourceOut
	assertWireFieldNullSemantics(t, &value, "Text", "text")
}

func TestNotebookOutIDBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldBoundaryRoundTrip(t, &value, "ID", "id")
}

func TestNotebookOutIDRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldRejectsWrongShape(t, &value, "ID", "id")
}

func TestNotebookOutIDMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldMissingPreservesValue(t, &value, "ID")
}

func TestNotebookOutIDNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldNullSemantics(t, &value, "ID", "id")
}

func TestNotebookOutNameBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Name", "name")
}

func TestNotebookOutNameRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldRejectsWrongShape(t, &value, "Name", "name")
}

func TestNotebookOutNameMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldMissingPreservesValue(t, &value, "Name")
}

func TestNotebookOutNameNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldNullSemantics(t, &value, "Name", "name")
}

func TestNotebookOutDescriptionBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Description", "description")
}

func TestNotebookOutDescriptionRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldRejectsWrongShape(t, &value, "Description", "description")
}

func TestNotebookOutDescriptionMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldMissingPreservesValue(t, &value, "Description")
}

func TestNotebookOutDescriptionNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldNullSemantics(t, &value, "Description", "description")
}

func TestNotebookOutDealRefBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldBoundaryRoundTrip(t, &value, "DealRef", "dealRef")
}

func TestNotebookOutDealRefRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldRejectsWrongShape(t, &value, "DealRef", "dealRef")
}

func TestNotebookOutDealRefMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldMissingPreservesValue(t, &value, "DealRef")
}

func TestNotebookOutDealRefNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldNullSemantics(t, &value, "DealRef", "dealRef")
}

func TestNotebookOutModeBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Mode", "mode")
}

func TestNotebookOutModeRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldRejectsWrongShape(t, &value, "Mode", "mode")
}

func TestNotebookOutModeMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldMissingPreservesValue(t, &value, "Mode")
}

func TestNotebookOutModeNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldNullSemantics(t, &value, "Mode", "mode")
}

func TestNotebookOutSourcesBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Sources", "sources")
}

func TestNotebookOutSourcesRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldRejectsWrongShape(t, &value, "Sources", "sources")
}

func TestNotebookOutSourcesMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldMissingPreservesValue(t, &value, "Sources")
}

func TestNotebookOutSourcesNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldNullSemantics(t, &value, "Sources", "sources")
}

func TestNotebookOutUpdatedBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Updated", "updated")
}

func TestNotebookOutUpdatedRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldRejectsWrongShape(t, &value, "Updated", "updated")
}

func TestNotebookOutUpdatedMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldMissingPreservesValue(t, &value, "Updated")
}

func TestNotebookOutUpdatedNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldNullSemantics(t, &value, "Updated", "updated")
}

func TestPersonRowEmailBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Email", "email")
}

func TestPersonRowEmailRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldRejectsWrongShape(t, &value, "Email", "email")
}

func TestPersonRowEmailMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldMissingPreservesValue(t, &value, "Email")
}

func TestPersonRowEmailNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldNullSemantics(t, &value, "Email", "email")
}

func TestPersonRowNameBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldBoundaryRoundTrip(t, &value, "Name", "name")
}

func TestPersonRowNameRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldRejectsWrongShape(t, &value, "Name", "name")
}

func TestPersonRowNameMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldMissingPreservesValue(t, &value, "Name")
}

func TestPersonRowNameNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldNullSemantics(t, &value, "Name", "name")
}

func TestPersonRowMessageCountBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldBoundaryRoundTrip(t, &value, "MessageCount", "messageCount")
}

func TestPersonRowMessageCountRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldRejectsWrongShape(t, &value, "MessageCount", "messageCount")
}

func TestPersonRowMessageCountMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldMissingPreservesValue(t, &value, "MessageCount")
}

func TestPersonRowMessageCountNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldNullSemantics(t, &value, "MessageCount", "messageCount")
}

func TestPersonRowLastSeenBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldBoundaryRoundTrip(t, &value, "LastSeen", "lastSeen")
}

func TestPersonRowLastSeenRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldRejectsWrongShape(t, &value, "LastSeen", "lastSeen")
}

func TestPersonRowLastSeenMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldMissingPreservesValue(t, &value, "LastSeen")
}

func TestPersonRowLastSeenNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldNullSemantics(t, &value, "LastSeen", "lastSeen")
}

func TestPersonRowLastSubjectBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldBoundaryRoundTrip(t, &value, "LastSubject", "lastSubject")
}

func TestPersonRowLastSubjectRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldRejectsWrongShape(t, &value, "LastSubject", "lastSubject")
}

func TestPersonRowLastSubjectMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldMissingPreservesValue(t, &value, "LastSubject")
}

func TestPersonRowLastSubjectNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldNullSemantics(t, &value, "LastSubject", "lastSubject")
}

func TestPersonRowWikiPathBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldBoundaryRoundTrip(t, &value, "WikiPath", "wikiPath")
}

func TestPersonRowWikiPathRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldRejectsWrongShape(t, &value, "WikiPath", "wikiPath")
}

func TestPersonRowWikiPathMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldMissingPreservesValue(t, &value, "WikiPath")
}

func TestPersonRowWikiPathNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldNullSemantics(t, &value, "WikiPath", "wikiPath")
}

func TestPersonRowWikiSummaryBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldBoundaryRoundTrip(t, &value, "WikiSummary", "wikiSummary")
}

func TestPersonRowWikiSummaryRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldRejectsWrongShape(t, &value, "WikiSummary", "wikiSummary")
}

func TestPersonRowWikiSummaryMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldMissingPreservesValue(t, &value, "WikiSummary")
}

func TestPersonRowWikiSummaryNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldNullSemantics(t, &value, "WikiSummary", "wikiSummary")
}

func TestSearchWikiHitPathBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SearchWikiHit
	assertWireFieldBoundaryRoundTrip(t, &value, "Path", "path")
}

func TestSearchWikiHitPathRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SearchWikiHit
	assertWireFieldRejectsWrongShape(t, &value, "Path", "path")
}

func TestSearchWikiHitPathMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SearchWikiHit
	assertWireFieldMissingPreservesValue(t, &value, "Path")
}

func TestSearchWikiHitPathNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SearchWikiHit
	assertWireFieldNullSemantics(t, &value, "Path", "path")
}

func TestSearchWikiHitTitleBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SearchWikiHit
	assertWireFieldBoundaryRoundTrip(t, &value, "Title", "title")
}

func TestSearchWikiHitTitleRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SearchWikiHit
	assertWireFieldRejectsWrongShape(t, &value, "Title", "title")
}

func TestSearchWikiHitTitleMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SearchWikiHit
	assertWireFieldMissingPreservesValue(t, &value, "Title")
}

func TestSearchWikiHitTitleNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SearchWikiHit
	assertWireFieldNullSemantics(t, &value, "Title", "title")
}

func TestSearchWikiHitSummaryBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SearchWikiHit
	assertWireFieldBoundaryRoundTrip(t, &value, "Summary", "summary")
}

func TestSearchWikiHitSummaryRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SearchWikiHit
	assertWireFieldRejectsWrongShape(t, &value, "Summary", "summary")
}

func TestSearchWikiHitSummaryMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SearchWikiHit
	assertWireFieldMissingPreservesValue(t, &value, "Summary")
}

func TestSearchWikiHitSummaryNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SearchWikiHit
	assertWireFieldNullSemantics(t, &value, "Summary", "summary")
}

func TestSearchWikiHitCategoryBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SearchWikiHit
	assertWireFieldBoundaryRoundTrip(t, &value, "Category", "category")
}

func TestSearchWikiHitCategoryRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SearchWikiHit
	assertWireFieldRejectsWrongShape(t, &value, "Category", "category")
}

func TestSearchWikiHitCategoryMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SearchWikiHit
	assertWireFieldMissingPreservesValue(t, &value, "Category")
}

func TestSearchWikiHitCategoryNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SearchWikiHit
	assertWireFieldNullSemantics(t, &value, "Category", "category")
}

func TestSearchWikiHitSnippetBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SearchWikiHit
	assertWireFieldBoundaryRoundTrip(t, &value, "Snippet", "snippet")
}

func TestSearchWikiHitSnippetRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SearchWikiHit
	assertWireFieldRejectsWrongShape(t, &value, "Snippet", "snippet")
}

func TestSearchWikiHitSnippetMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SearchWikiHit
	assertWireFieldMissingPreservesValue(t, &value, "Snippet")
}

func TestSearchWikiHitSnippetNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SearchWikiHit
	assertWireFieldNullSemantics(t, &value, "Snippet", "snippet")
}

func TestSearchWikiHitScoreBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SearchWikiHit
	assertWireFieldBoundaryRoundTrip(t, &value, "Score", "score")
}

func TestSearchWikiHitScoreRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SearchWikiHit
	assertWireFieldRejectsWrongShape(t, &value, "Score", "score")
}

func TestSearchWikiHitScoreMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SearchWikiHit
	assertWireFieldMissingPreservesValue(t, &value, "Score")
}

func TestSearchWikiHitScoreNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SearchWikiHit
	assertWireFieldNullSemantics(t, &value, "Score", "score")
}

func TestSearchDiaryHitFileBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SearchDiaryHit
	assertWireFieldBoundaryRoundTrip(t, &value, "File", "file")
}

func TestSearchDiaryHitFileRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SearchDiaryHit
	assertWireFieldRejectsWrongShape(t, &value, "File", "file")
}

func TestSearchDiaryHitFileMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SearchDiaryHit
	assertWireFieldMissingPreservesValue(t, &value, "File")
}

func TestSearchDiaryHitFileNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SearchDiaryHit
	assertWireFieldNullSemantics(t, &value, "File", "file")
}

func TestSearchDiaryHitHeaderBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SearchDiaryHit
	assertWireFieldBoundaryRoundTrip(t, &value, "Header", "header")
}

func TestSearchDiaryHitHeaderRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SearchDiaryHit
	assertWireFieldRejectsWrongShape(t, &value, "Header", "header")
}

func TestSearchDiaryHitHeaderMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SearchDiaryHit
	assertWireFieldMissingPreservesValue(t, &value, "Header")
}

func TestSearchDiaryHitHeaderNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SearchDiaryHit
	assertWireFieldNullSemantics(t, &value, "Header", "header")
}

func TestSearchDiaryHitContentBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SearchDiaryHit
	assertWireFieldBoundaryRoundTrip(t, &value, "Content", "content")
}

func TestSearchDiaryHitContentRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SearchDiaryHit
	assertWireFieldRejectsWrongShape(t, &value, "Content", "content")
}

func TestSearchDiaryHitContentMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SearchDiaryHit
	assertWireFieldMissingPreservesValue(t, &value, "Content")
}

func TestSearchDiaryHitContentNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SearchDiaryHit
	assertWireFieldNullSemantics(t, &value, "Content", "content")
}

func TestSearchDiaryHitAtBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SearchDiaryHit
	assertWireFieldBoundaryRoundTrip(t, &value, "At", "at")
}

func TestSearchDiaryHitAtRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SearchDiaryHit
	assertWireFieldRejectsWrongShape(t, &value, "At", "at")
}

func TestSearchDiaryHitAtMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SearchDiaryHit
	assertWireFieldMissingPreservesValue(t, &value, "At")
}

func TestSearchDiaryHitAtNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SearchDiaryHit
	assertWireFieldNullSemantics(t, &value, "At", "at")
}

func TestSearchDiaryHitScoreBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SearchDiaryHit
	assertWireFieldBoundaryRoundTrip(t, &value, "Score", "score")
}

func TestSearchDiaryHitScoreRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SearchDiaryHit
	assertWireFieldRejectsWrongShape(t, &value, "Score", "score")
}

func TestSearchDiaryHitScoreMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SearchDiaryHit
	assertWireFieldMissingPreservesValue(t, &value, "Score")
}

func TestSearchDiaryHitScoreNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SearchDiaryHit
	assertWireFieldNullSemantics(t, &value, "Score", "score")
}

func TestSearchAllResultWikiBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SearchAllResult
	assertWireFieldBoundaryRoundTrip(t, &value, "Wiki", "wiki")
}

func TestSearchAllResultWikiRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SearchAllResult
	assertWireFieldRejectsWrongShape(t, &value, "Wiki", "wiki")
}

func TestSearchAllResultWikiMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SearchAllResult
	assertWireFieldMissingPreservesValue(t, &value, "Wiki")
}

func TestSearchAllResultWikiNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SearchAllResult
	assertWireFieldNullSemantics(t, &value, "Wiki", "wiki")
}

func TestSearchAllResultDiaryBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SearchAllResult
	assertWireFieldBoundaryRoundTrip(t, &value, "Diary", "diary")
}

func TestSearchAllResultDiaryRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SearchAllResult
	assertWireFieldRejectsWrongShape(t, &value, "Diary", "diary")
}

func TestSearchAllResultDiaryMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SearchAllResult
	assertWireFieldMissingPreservesValue(t, &value, "Diary")
}

func TestSearchAllResultDiaryNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SearchAllResult
	assertWireFieldNullSemantics(t, &value, "Diary", "diary")
}

func TestSearchAllResultPeopleBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value SearchAllResult
	assertWireFieldBoundaryRoundTrip(t, &value, "People", "people")
}

func TestSearchAllResultPeopleRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value SearchAllResult
	assertWireFieldRejectsWrongShape(t, &value, "People", "people")
}

func TestSearchAllResultPeopleMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value SearchAllResult
	assertWireFieldMissingPreservesValue(t, &value, "People")
}

func TestSearchAllResultPeopleNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value SearchAllResult
	assertWireFieldNullSemantics(t, &value, "People", "people")
}

func TestTopicDocOutKeyBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value TopicDocOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Key", "key")
}

func TestTopicDocOutKeyRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value TopicDocOut
	assertWireFieldRejectsWrongShape(t, &value, "Key", "key")
}

func TestTopicDocOutKeyMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value TopicDocOut
	assertWireFieldMissingPreservesValue(t, &value, "Key")
}

func TestTopicDocOutKeyNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value TopicDocOut
	assertWireFieldNullSemantics(t, &value, "Key", "key")
}

func TestTopicDocOutNameBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value TopicDocOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Name", "name")
}

func TestTopicDocOutNameRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value TopicDocOut
	assertWireFieldRejectsWrongShape(t, &value, "Name", "name")
}

func TestTopicDocOutNameMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value TopicDocOut
	assertWireFieldMissingPreservesValue(t, &value, "Name")
}

func TestTopicDocOutNameNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value TopicDocOut
	assertWireFieldNullSemantics(t, &value, "Name", "name")
}

func TestTopicDocOutContentBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value TopicDocOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Content", "content")
}

func TestTopicDocOutContentRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value TopicDocOut
	assertWireFieldRejectsWrongShape(t, &value, "Content", "content")
}

func TestTopicDocOutContentMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value TopicDocOut
	assertWireFieldMissingPreservesValue(t, &value, "Content")
}

func TestTopicDocOutContentNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value TopicDocOut
	assertWireFieldNullSemantics(t, &value, "Content", "content")
}

func TestTopicDocOutSizeBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value TopicDocOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Size", "size")
}

func TestTopicDocOutSizeRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value TopicDocOut
	assertWireFieldRejectsWrongShape(t, &value, "Size", "size")
}

func TestTopicDocOutSizeMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value TopicDocOut
	assertWireFieldMissingPreservesValue(t, &value, "Size")
}

func TestTopicDocOutSizeNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value TopicDocOut
	assertWireFieldNullSemantics(t, &value, "Size", "size")
}

func TestTopicDocOutModifiedBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value TopicDocOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Modified", "modified")
}

func TestTopicDocOutModifiedRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value TopicDocOut
	assertWireFieldRejectsWrongShape(t, &value, "Modified", "modified")
}

func TestTopicDocOutModifiedMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value TopicDocOut
	assertWireFieldMissingPreservesValue(t, &value, "Modified")
}

func TestTopicDocOutModifiedNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value TopicDocOut
	assertWireFieldNullSemantics(t, &value, "Modified", "modified")
}

func TestTopicDocWriteOutKeyBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value TopicDocWriteOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Key", "key")
}

func TestTopicDocWriteOutKeyRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value TopicDocWriteOut
	assertWireFieldRejectsWrongShape(t, &value, "Key", "key")
}

func TestTopicDocWriteOutKeyMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value TopicDocWriteOut
	assertWireFieldMissingPreservesValue(t, &value, "Key")
}

func TestTopicDocWriteOutKeyNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value TopicDocWriteOut
	assertWireFieldNullSemantics(t, &value, "Key", "key")
}

func TestTopicDocWriteOutNameBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value TopicDocWriteOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Name", "name")
}

func TestTopicDocWriteOutNameRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value TopicDocWriteOut
	assertWireFieldRejectsWrongShape(t, &value, "Name", "name")
}

func TestTopicDocWriteOutNameMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value TopicDocWriteOut
	assertWireFieldMissingPreservesValue(t, &value, "Name")
}

func TestTopicDocWriteOutNameNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value TopicDocWriteOut
	assertWireFieldNullSemantics(t, &value, "Name", "name")
}

func TestTopicDocWriteOutSizeBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value TopicDocWriteOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Size", "size")
}

func TestTopicDocWriteOutSizeRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value TopicDocWriteOut
	assertWireFieldRejectsWrongShape(t, &value, "Size", "size")
}

func TestTopicDocWriteOutSizeMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value TopicDocWriteOut
	assertWireFieldMissingPreservesValue(t, &value, "Size")
}

func TestTopicDocWriteOutSizeNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value TopicDocWriteOut
	assertWireFieldNullSemantics(t, &value, "Size", "size")
}

func TestTopicDocWriteOutModifiedBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value TopicDocWriteOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Modified", "modified")
}

func TestTopicDocWriteOutModifiedRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value TopicDocWriteOut
	assertWireFieldRejectsWrongShape(t, &value, "Modified", "modified")
}

func TestTopicDocWriteOutModifiedMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value TopicDocWriteOut
	assertWireFieldMissingPreservesValue(t, &value, "Modified")
}

func TestTopicDocWriteOutModifiedNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value TopicDocWriteOut
	assertWireFieldNullSemantics(t, &value, "Modified", "modified")
}

func TestTopicDocWriteOutAppliedBoundaryRoundTrip(t *testing.T) {
	t.Parallel()

	var value TopicDocWriteOut
	assertWireFieldBoundaryRoundTrip(t, &value, "Applied", "applied")
}

func TestTopicDocWriteOutAppliedRejectsWrongShape(t *testing.T) {
	t.Parallel()

	var value TopicDocWriteOut
	assertWireFieldRejectsWrongShape(t, &value, "Applied", "applied")
}

func TestTopicDocWriteOutAppliedMissingPreservesPatchValue(t *testing.T) {
	t.Parallel()

	var value TopicDocWriteOut
	assertWireFieldMissingPreservesValue(t, &value, "Applied")
}

func TestTopicDocWriteOutAppliedNullFollowsEncodingJSONSemantics(t *testing.T) {
	t.Parallel()

	var value TopicDocWriteOut
	assertWireFieldNullSemantics(t, &value, "Applied", "applied")
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

func TestMemoryPageRowTitleOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MemoryPageRow
	assertWireFieldOmitsZeroValue(t, &value, "title")
}

func TestMemoryPageRowSummaryOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value MemoryPageRow
	assertWireFieldOmitsZeroValue(t, &value, "summary")
}

func TestMemoryPageRowUpdatedOmitsZeroValue(t *testing.T) {
	t.Parallel()

	var value MemoryPageRow
	assertWireFieldOmitsZeroValue(t, &value, "updated")
}

func TestNotebookSummaryOutDescriptionOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldOmitsZeroValue(t, &value, "description")
}

func TestNotebookSummaryOutDealRefOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldOmitsZeroValue(t, &value, "dealRef")
}

func TestNotebookSummaryOutProjectRefsOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value NotebookSummaryOut
	assertWireFieldOmitsZeroValue(t, &value, "projectRefs")
}

func TestNotebookSourceOutRefOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value NotebookSourceOut
	assertWireFieldOmitsZeroValue(t, &value, "ref")
}

func TestNotebookSourceOutTitleOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value NotebookSourceOut
	assertWireFieldOmitsZeroValue(t, &value, "title")
}

func TestNotebookSourceOutTextOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value NotebookSourceOut
	assertWireFieldOmitsZeroValue(t, &value, "text")
}

func TestNotebookOutDescriptionOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldOmitsZeroValue(t, &value, "description")
}

func TestNotebookOutDealRefOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldOmitsZeroValue(t, &value, "dealRef")
}

func TestNotebookOutModeOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value NotebookOut
	assertWireFieldOmitsZeroValue(t, &value, "mode")
}

func TestPersonRowNameOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldOmitsZeroValue(t, &value, "name")
}

func TestPersonRowLastSeenOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldOmitsZeroValue(t, &value, "lastSeen")
}

func TestPersonRowLastSubjectOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldOmitsZeroValue(t, &value, "lastSubject")
}

func TestPersonRowWikiPathOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldOmitsZeroValue(t, &value, "wikiPath")
}

func TestPersonRowWikiSummaryOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value PersonRow
	assertWireFieldOmitsZeroValue(t, &value, "wikiSummary")
}

func TestSearchWikiHitTitleOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SearchWikiHit
	assertWireFieldOmitsZeroValue(t, &value, "title")
}

func TestSearchWikiHitSummaryOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SearchWikiHit
	assertWireFieldOmitsZeroValue(t, &value, "summary")
}

func TestSearchWikiHitCategoryOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SearchWikiHit
	assertWireFieldOmitsZeroValue(t, &value, "category")
}

func TestSearchDiaryHitAtOmittedWhenZero(t *testing.T) {
	t.Parallel()

	var value SearchDiaryHit
	assertWireFieldOmitsZeroValue(t, &value, "at")
}
