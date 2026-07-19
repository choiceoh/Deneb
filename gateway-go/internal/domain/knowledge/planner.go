package knowledge

import (
	"sort"
	"strings"
)

// RecallOptions constrains source selection without encoding planner hints into
// the query string. With zero options the conservative plan searches every
// available source, preserving current recall coverage.
type RecallOptions struct {
	Layers       []Layer
	Scopes       []string
	Capabilities []Capability
}

type PlannedSource struct {
	Source     SourceDescriptor
	FetchLimit int
	Priority   int
	Reason     string
}

type RecallPlan struct {
	Query   string
	Limit   int
	Scopes  []string
	Sources []PlannedSource
}

// Catalog returns the compact source catalog the deterministic planner uses.
// The same descriptors carry each connector's sync/freshness contract.
func (r *Router) Catalog() []SourceDescriptor {
	if r == nil {
		return nil
	}
	out := make([]SourceDescriptor, 0, len(r.adapters))
	for _, adapter := range r.adapters {
		out = append(out, descriptorOf(adapter))
	}
	return out
}

// PlanRecall chooses sources from typed constraints and uses query shape only
// for priority. It does not silently exclude a source on a heuristic: default
// plans retain full recall, while explicit layers/capabilities provide the
// narrow, auditable tool selection path.
func (r *Router) PlanRecall(query string, limit int, options RecallOptions) RecallPlan {
	if limit <= 0 {
		limit = 10
	}
	plan := RecallPlan{
		Query: strings.TrimSpace(query), Limit: limit,
		Scopes: normalizeRecallScopes(options.Scopes),
	}
	wantedLayers := make(map[Layer]bool, len(options.Layers))
	for _, layer := range options.Layers {
		if layer != "" {
			wantedLayers[layer] = true
		}
	}
	for _, descriptor := range r.Catalog() {
		if len(wantedLayers) > 0 && !wantedLayers[descriptor.Layer] {
			continue
		}
		if !hasCapabilities(descriptor.Capabilities, options.Capabilities) {
			continue
		}
		priority, reason := sourcePriority(descriptor, plan.Query)
		fetchLimit := limit
		if len(plan.Scopes) > 0 {
			fetchLimit = min(100, max(limit*4, limit+20))
		}
		plan.Sources = append(plan.Sources, PlannedSource{
			Source: descriptor, FetchLimit: fetchLimit, Priority: priority, Reason: reason,
		})
	}
	sort.SliceStable(plan.Sources, func(i, j int) bool {
		return plan.Sources[i].Priority > plan.Sources[j].Priority
	})
	return plan
}

func hasCapabilities(available, required []Capability) bool {
	if len(required) == 0 {
		return true
	}
	set := make(map[Capability]bool, len(available))
	for _, capability := range available {
		set[capability] = true
	}
	for _, capability := range required {
		if !set[capability] {
			return false
		}
	}
	return true
}

func sourcePriority(source SourceDescriptor, query string) (int, string) {
	lower := strings.ToLower(query)
	fileLike := strings.ContainsAny(lower, "/\\") || strings.Contains(lower, "파일") || strings.Contains(lower, "문서") ||
		strings.Contains(lower, "코드") || strings.Contains(lower, "함수") || strings.Contains(lower, "class ") ||
		hasCodeExtension(lower)
	if fileLike && source.Layer == LayerFiles {
		return 120, "file-or-code intent"
	}
	if !fileLike && source.Layer == LayerWiki {
		return 110, "curated knowledge prior"
	}
	return 100, "coverage fallback"
}

func hasCodeExtension(query string) bool {
	for _, extension := range []string{".go", ".kt", ".kts", ".java", ".py", ".js", ".jsx", ".ts", ".tsx", ".rs"} {
		if strings.Contains(query, extension) {
			return true
		}
	}
	return false
}

func normalizeRecallScopes(scopes []string) []string {
	out := make([]string, 0, len(scopes))
	seen := make(map[string]bool, len(scopes))
	for _, scope := range scopes {
		scope = strings.Trim(strings.ReplaceAll(strings.TrimSpace(scope), "\\", "/"), "/")
		if scope == "" || seen[scope] {
			continue
		}
		seen[scope] = true
		out = append(out, scope)
	}
	return out
}

func inRecallScopes(ref Ref, scopes []string) bool {
	if len(scopes) == 0 {
		return true
	}
	id := strings.Trim(strings.ReplaceAll(ref.ID, "\\", "/"), "/")
	for _, raw := range scopes {
		scope := raw
		if prefix, rest, ok := strings.Cut(scope, ":"); ok {
			layer, valid := layerFromName(prefix)
			if valid {
				if layer != ref.Layer {
					continue
				}
				scope = strings.Trim(rest, "/")
			}
		}
		if scope == "" || id == scope || strings.HasPrefix(id, scope+"/") {
			return true
		}
	}
	return false
}

func layerFromName(name string) (Layer, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "w", "wiki":
		return LayerWiki, true
	case "f", "file", "files":
		return LayerFiles, true
	default:
		return "", false
	}
}

// ParseLayerName accepts both human source names and canonical ref prefixes.
func ParseLayerName(name string) (Layer, bool) { return layerFromName(name) }
