package secret

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestNewResolver_ReportsMissingSecretsInitially(t *testing.T) {
	before := time.Now().UnixMilli()
	resolver := NewResolver()
	after := time.Now().UnixMilli()
	if resolver.loadedAtMs < before || resolver.loadedAtMs > after || resolver.secrets == nil {
		t.Fatalf("resolver initial state = %+v", resolver)
	}
	result := resolver.Resolve("openai", []string{"apiKey", "orgId"})
	if !result.OK || len(result.Assignments) != 0 ||
		!reflect.DeepEqual(result.InactiveRefPaths, []string{"openai.apiKey", "openai.orgId"}) {
		t.Fatalf("miss result = %+v", result)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics = %#v", result.Diagnostics)
	}
	empty := resolver.Resolve("openai", nil)
	if !empty.OK || len(empty.Assignments) != 0 || len(empty.InactiveRefPaths) != 0 {
		t.Fatalf("empty result = %+v", empty)
	}
}

func TestResolverPreservesTargetOrderDuplicatesAndValueTypes(t *testing.T) {
	resolver := NewResolver()
	resolver.SetValue("svc.first", "secret")
	resolver.SetValue("svc.second", 42)
	resolver.SetValue("svc.flag", true)
	targets := []string{"second", "missing", "first", "second", "flag"}
	result := resolver.Resolve("svc", targets)
	if !result.OK || len(result.Assignments) != 4 || len(result.InactiveRefPaths) != 1 {
		t.Fatalf("result = %+v", result)
	}
	wantPaths := []string{"svc.second", "svc.first", "svc.second", "svc.flag"}
	gotPaths := make([]string, len(result.Assignments))
	for i, assignment := range result.Assignments {
		gotPaths[i] = assignment.Path
		if !reflect.DeepEqual(assignment.PathSegments, []string{"svc", targets[indexForAssignment(i)]}) {
			// Duplicates make a direct targets[i] comparison misleading; verify
			// the segments reconstruct the path instead.
			if assignment.Path != assignment.PathSegments[0]+"."+assignment.PathSegments[1] {
				t.Fatalf("assignment segments = %+v", assignment)
			}
		}
	}
	if !reflect.DeepEqual(gotPaths, wantPaths) || result.InactiveRefPaths[0] != "svc.missing" {
		t.Fatalf("paths = %#v inactive=%#v", gotPaths, result.InactiveRefPaths)
	}
	if result.Assignments[0].Value != 42 || result.Assignments[1].Value != "secret" || result.Assignments[3].Value != true {
		t.Fatalf("values = %+v", result.Assignments)
	}
}

func indexForAssignment(i int) int {
	return []int{0, 2, 3, 4}[i]
}

func TestResolverSetOverwritesAndBlankPathIsAddressable(t *testing.T) {
	resolver := NewResolver()
	resolver.SetValue("svc.key", "first")
	resolver.SetValue("svc.key", "second")
	resolver.SetValue(".", "blank")
	result := resolver.Resolve("svc", []string{"key"})
	if len(result.Assignments) != 1 || result.Assignments[0].Value != "second" {
		t.Fatalf("overwrite result = %+v", result)
	}
	blank := resolver.Resolve("", []string{""})
	if len(blank.Assignments) != 1 || blank.Assignments[0].Path != "." || blank.Assignments[0].Value != "blank" {
		t.Fatalf("blank path result = %+v", blank)
	}
}

func TestReloadClearsWarningsButRetainsSecrets(t *testing.T) {
	resolver := NewResolver()
	resolver.SetValue("svc.key", "value")
	resolver.warnings = []string{"one", "two"}
	resolver.loadedAtMs = 1
	result := resolver.Reload()
	if result == nil || !result.OK || result.WarningCount != 2 {
		t.Fatalf("Reload = %+v", result)
	}
	if resolver.loadedAtMs <= 1 || resolver.warnings != nil {
		t.Fatalf("reload state timestamp=%d warnings=%#v", resolver.loadedAtMs, resolver.warnings)
	}
	resolved := resolver.Resolve("svc", []string{"key"})
	if len(resolved.Assignments) != 1 || resolved.Assignments[0].Value != "value" {
		t.Fatalf("Reload discarded secrets: %+v", resolved)
	}
	second := resolver.Reload()
	if second.WarningCount != 0 {
		t.Fatalf("second Reload warning count = %d", second.WarningCount)
	}
}

func TestResolverConcurrentSetResolveAndReload(t *testing.T) {
	resolver := NewResolver()
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				key := fmt.Sprintf("svc.key-%d", (i+j)%40)
				switch i % 3 {
				case 0:
					resolver.SetValue(key, i*1000+j)
				case 1:
					_ = resolver.Resolve("svc", []string{fmt.Sprintf("key-%d", (i+j)%40), "missing"})
				default:
					_ = resolver.Reload()
				}
			}
		}(i)
	}
	wg.Wait()
	resolver.mu.RLock()
	count := len(resolver.secrets)
	resolver.mu.RUnlock()
	if count == 0 || count > 40 {
		t.Fatalf("secret count = %d", count)
	}
}
