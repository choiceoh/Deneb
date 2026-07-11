package briefcase

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	casepack "github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolpreset"
)

func TestRecordFixturePagingBoundsAndDeepMatch(t *testing.T) {
	now := time.Date(2031, time.January, 2, 3, 4, 5, 0, time.UTC)
	world := &World{
		clock: NewManualClock(now), visible: make(map[string]Record),
		sources: make(map[string]casepack.Source), released: make(map[string]struct{}), withheld: make(map[string]struct{}),
	}
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("mail-%02d", i)
		content := fmt.Sprintf("record %02d", i)
		if i == 9 {
			content = "x" + strings.Repeat("가", 4_000) + " 깊은-바늘 " + strings.Repeat("나", 1_000)
		}
		source := casepack.Source{
			ID: id, Kind: casepack.SourceMail, Origin: casepack.SourceOriginSynthetic,
			Access: casepack.SourceAccessSnapshot, EventAt: now.Add(time.Duration(i) * time.Minute),
			AvailableAt: now.Add(time.Duration(i) * time.Minute), CapturedAt: now,
		}
		source.ProjectRefs = []string{strings.Repeat("p", 128), strings.Repeat("q", 128), strings.Repeat("r", 128), strings.Repeat("s", 128)}
		source.SourceRef = strings.Repeat("x", 256)
		source.Supersedes = []string{strings.Repeat("a", 128), strings.Repeat("b", 128), strings.Repeat("c", 128), strings.Repeat("d", 128)}
		source.Sensitivity = strings.Repeat("z", 64)
		world.visible[id] = Record{Source: source, Content: []byte(content)}
	}
	root, err := NewRunRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	policy, err := NewPolicy(root, PolicyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	paths, err := root.Paths()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewFixtureRegistry(FixtureRegistryConfig{
		Workspace: paths.Workspace, World: world, Policy: policy,
		ToolPolicy: fixtureRegistryPolicy("mail_archive"),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := toolctx.WithToolPreset(context.Background(), string(toolpreset.PresetBriefcase))

	type page struct {
		Count            int `json:"count"`
		TotalCount       int `json:"totalCount"`
		NextRecordOffset int `json:"nextRecordOffset"`
		Records          []struct {
			ID          string `json:"id"`
			Content     string `json:"content"`
			OffsetBytes int    `json:"offsetBytes"`
			Encoding    string `json:"contentEncoding"`
		} `json:"records"`
	}
	readPage := func(input string) page {
		t.Helper()
		output, err := registry.Execute(ctx, "mail_archive", json.RawMessage(input))
		if err != nil {
			t.Fatalf("record fixture %s: %v", input, err)
		}
		if !json.Valid([]byte(output)) || len(output) > fixtureMaxWireOutput {
			t.Fatalf("record output is not bounded valid JSON: bytes=%d", len(output))
		}
		var result page
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := readPage(`{"query":""}`)
	if first.Count != 4 || first.TotalCount != 10 || first.NextRecordOffset != 4 {
		t.Fatalf("first page = %+v", first)
	}
	second := readPage(`{"query":"","recordOffset":4}`)
	if second.Count != 4 || second.TotalCount != 10 || second.NextRecordOffset != 8 {
		t.Fatalf("second page = %+v", second)
	}
	third := readPage(`{"query":"","recordOffset":8}`)
	if third.Count != 2 || third.TotalCount != 10 || third.NextRecordOffset != 0 {
		t.Fatalf("third page = %+v", third)
	}
	seen := make(map[string]struct{}, 10)
	for _, record := range append(append(first.Records, second.Records...), third.Records...) {
		if _, duplicate := seen[record.ID]; duplicate {
			t.Fatalf("duplicate paged record %q", record.ID)
		}
		seen[record.ID] = struct{}{}
	}
	if len(seen) != 10 {
		t.Fatalf("paged records = %v", seen)
	}

	controlID := "mail-control"
	world.visible[controlID] = Record{Source: casepack.Source{
		ID: controlID, Kind: casepack.SourceMail, Origin: casepack.SourceOriginSynthetic,
		Access: casepack.SourceAccessSnapshot, EventAt: now.Add(20 * time.Minute),
		AvailableAt: now.Add(20 * time.Minute), CapturedAt: now,
	}, Content: bytes.Repeat([]byte{0}, fixtureMaxRecordContent)}
	control := readPage(`{"id":"mail-control"}`)
	if len(control.Records) != 1 || control.Records[0].Encoding != "base64" || control.Records[0].Content == "" {
		t.Fatalf("control-heavy record was not represented safely: %+v", control.Records)
	}

	deep := readPage(`{"query":"깊은-바늘"}`)
	if len(deep.Records) != 1 || deep.Records[0].Encoding != "utf-8" ||
		deep.Records[0].OffsetBytes == 0 || !strings.Contains(deep.Records[0].Content, "깊은-바늘") {
		t.Fatalf("deep match was not centered and readable: %+v", deep.Records)
	}

	for _, input := range []string{
		`{"id":"mail-00","limitBytes":-1}`,
		`{"id":"mail-00","limitBytes":8193}`,
		`{"query":"","unknown":true}`,
		`{"query":"a","query":"b"}`,
	} {
		if _, err := registry.Execute(ctx, "mail_archive", json.RawMessage(input)); err == nil {
			t.Fatalf("invalid fixture input was accepted: %s", input)
		}
	}
}

func TestFixtureRegistryAdvertisesOnlySignedAllowedTools(t *testing.T) {
	world := &World{visible: make(map[string]Record)}
	root, err := NewRunRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	policy, err := NewPolicy(root, PolicyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	paths, _ := root.Paths()
	registry, err := NewFixtureRegistry(FixtureRegistryConfig{
		Workspace: paths.Workspace, World: world, Policy: policy,
		ToolPolicy: fixtureRegistryPolicy("mail_archive", "read"),
	})
	if err != nil {
		t.Fatal(err)
	}
	definitions := registry.Definitions()
	if len(definitions) != 2 || definitions[0].Name != "read" || definitions[1].Name != "mail_archive" {
		names := make([]string, 0, len(definitions))
		for _, definition := range definitions {
			names = append(names, definition.Name)
		}
		t.Fatalf("advertised tools do not match signed allow rules: %v", names)
	}
}
