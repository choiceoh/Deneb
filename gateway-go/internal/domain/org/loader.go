package org

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/classification"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
)

// orgFileName is the operator-maintained org chart under the state dir. Like
// classification_rules.json it lives OUTSIDE the repo (it holds real
// person/company names — privacy). The repo ships only org.example.json with
// fake data for the operator to copy.
const orgFileName = "org.json"

// orgEnvVar overrides the org file path (tests, non-standard deployments).
// Mirrors the DENEB_*_FILE/PATH override convention used across the codebase.
const orgEnvVar = "DENEB_ORG_FILE"

// resolveOrgPath returns the org file path: the DENEB_ORG_FILE override if set,
// else {stateDir}/org.json (DENEB_STATE_DIR-aware via config.ResolveStateDir,
// so a dev gateway reads its own dir).
func resolveOrgPath() string {
	if v := strings.TrimSpace(os.Getenv(orgEnvVar)); v != "" {
		return v
	}
	return filepath.Join(config.ResolveStateDir(), orgFileName)
}

// Load reads the operator's org chart, resolving the path itself (env override
// → state dir). A missing file is NOT an error: it returns an empty tree (the
// caller — LoadRules — then falls back to the legacy classification rules). A
// present-but-corrupt or invalid file IS an error so a bad edit surfaces
// instead of silently reverting.
func Load() (OrgTree, error) {
	return LoadFromFile(resolveOrgPath())
}

// LoadFromFile is Load against an explicit path (the testable core). A missing
// file yields an empty, valid tree with no error. A present file is parsed and
// validated; parse/validation failures are returned as errors.
func LoadFromFile(path string) (OrgTree, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return OrgTree{}, nil // no chart yet — caller falls back
		}
		return OrgTree{}, fmt.Errorf("org: read %s: %w", path, err)
	}
	var tree OrgTree
	if err := json.Unmarshal(data, &tree); err != nil {
		return OrgTree{}, fmt.Errorf("org: parse %s: %w", path, err)
	}
	if err := tree.Validate(); err != nil {
		return OrgTree{}, fmt.Errorf("org: invalid chart %s: %w", path, err)
	}
	return tree, nil
}

// Save validates the tree and atomically writes it to {stateDir}/org.json (or
// the DENEB_ORG_FILE override). Validation runs first so an invalid chart is
// rejected before it can overwrite a good file. Writing is delegated to the
// RPC layer (which owns atomicfile); this resolves the path + validates and
// returns the marshaled bytes the caller persists.
//
// Kept here (not in the handler) so the path/validation contract lives with the
// model. The handler calls SaveTo with the resolved path via ResolvePath.
func (t OrgTree) marshal() ([]byte, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	return json.MarshalIndent(t, "", "  ")
}

// ResolvePath exposes the resolved org file path to the RPC layer (which owns
// the atomic write). Encapsulates the env-override + state-dir policy so the
// handler never hardcodes the path.
func ResolvePath() string { return resolveOrgPath() }

// Marshal validates the tree and returns its canonical JSON encoding (2-space
// indent) for the handler to persist atomically. Exported wrapper over the
// private marshal so the handler can write without re-implementing validation
// or the on-disk shape.
func (t OrgTree) Marshal() ([]byte, error) { return t.marshal() }

// LoadRules is the single ruleset entry the dashboard uses: the org chart is
// the MASTER. Resolution order (backward compatible):
//
//  1. {stateDir}/org.json exists and defines ≥1 lane → derive rules from the
//     chart (DeriveRules). The chart wholly owns classification.
//  2. No chart (or a chart with no lane nodes) → fall back to the legacy
//     classification.Load (operator's classification_rules.json, else the
//     in-code keyword defaults).
//
// A corrupt/invalid org.json is surfaced as an error (so a bad chart is
// visible) — the dashboard handler degrades that to keyword defaults, same as a
// bad classification file. This keeps every prior deployment (no org.json,
// only classification_rules.json) working unchanged while letting the chart
// take over the moment it defines parts.
func LoadRules() (classification.Rules, error) {
	tree, err := Load()
	if err != nil {
		// Bad chart — don't silently fall back to the legacy file (that would
		// hide a real edit error). Surface it; the caller decides how to degrade.
		return classification.Rules{}, err
	}
	if tree.HasLanes() {
		return tree.DeriveRules(), nil
	}
	// No chart / no parts defined — legacy path (full backward compat).
	return classification.Load()
}

// LoadLanes returns the dashboard lane definitions: the chart's lane nodes when
// org.json defines parts, else nil (the dashboard then uses its legacy
// hardcoded classification.AllLanes). A bad chart returns nil + error; the
// handler falls back to the legacy lanes. Pairs with LoadRules so the dashboard
// gets both its grouping rules and its column set from the same source.
func LoadLanes() ([]LaneDef, error) {
	tree, err := Load()
	if err != nil {
		return nil, err
	}
	if !tree.HasLanes() {
		return nil, nil
	}
	return tree.DeriveLanes(), nil
}
