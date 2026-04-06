// wiki-migrate migrates existing memory.db facts, Vega project data,
// and user model entries into the wiki knowledge base.
//
// Usage:
//
//	go run ./cmd/wiki-migrate [--dry-run] [--memory-db PATH] [--projects-dir PATH] [--wiki-dir PATH]
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/choiceoh/deneb/gateway-go/internal/memory"
	"github.com/choiceoh/deneb/gateway-go/internal/wiki"
)

func main() {
	home, _ := os.UserHomeDir()

	dryRun := flag.Bool("dry-run", false, "Preview without writing files")
	memoryDB := flag.String("memory-db", filepath.Join(home, ".deneb", "memory.db"), "Path to memory.db")
	projectsDir := flag.String("projects-dir", filepath.Join(home, ".deneb", "projects"), "Path to projects directory")
	wikiDir := flag.String("wiki-dir", filepath.Join(home, ".deneb", "wiki"), "Wiki output directory")
	diaryDir := flag.String("diary-dir", filepath.Join(home, ".deneb", "memory", "diary"), "Diary directory")
	flag.Parse()

	ctx := context.Background()

	log.Printf("wiki-migrate: memory-db=%s projects-dir=%s wiki-dir=%s dry-run=%v",
		*memoryDB, *projectsDir, *wikiDir, *dryRun)

	// Initialize wiki store (creates directories).
	var store *wiki.Store
	if !*dryRun {
		var err error
		store, err = wiki.NewStore(*wikiDir, *diaryDir)
		if err != nil {
			log.Fatalf("failed to create wiki store: %v", err)
		}
		defer store.Close()
	}

	var totalCreated int

	// Phase 1: Migrate memory.db facts.
	if _, err := os.Stat(*memoryDB); err == nil {
		n, err := migrateFacts(ctx, *memoryDB, *wikiDir, store, *dryRun)
		if err != nil {
			log.Printf("WARNING: facts migration error: %v", err)
		} else {
			totalCreated += n
			log.Printf("Phase 1 (facts): %d pages", n)
		}
	} else {
		log.Printf("Phase 1 (facts): skipped (no memory.db)")
	}

	// Phase 2: Migrate Vega project data.
	if info, err := os.Stat(*projectsDir); err == nil && info.IsDir() {
		n, err := migrateProjects(*projectsDir, *wikiDir, store, *dryRun)
		if err != nil {
			log.Printf("WARNING: projects migration error: %v", err)
		} else {
			totalCreated += n
			log.Printf("Phase 2 (projects): %d pages", n)
		}
	} else {
		log.Printf("Phase 2 (projects): skipped (no projects dir)")
	}

	// Phase 3: Migrate user model.
	if _, err := os.Stat(*memoryDB); err == nil {
		n, err := migrateUserModel(ctx, *memoryDB, *wikiDir, store, *dryRun)
		if err != nil {
			log.Printf("WARNING: user model migration error: %v", err)
		} else {
			totalCreated += n
			log.Printf("Phase 3 (user model): %d pages", n)
		}
	} else {
		log.Printf("Phase 3 (user model): skipped")
	}

	log.Printf("wiki-migrate: complete. %d pages total.", totalCreated)
}

// migrateFacts reads facts from memory.db and creates wiki pages grouped by entity.
func migrateFacts(ctx context.Context, dbPath, wikiDir string, store *wiki.Store, dryRun bool) (int, error) {
	db, err := sql.Open("sqlite3", dbPath+"?mode=ro")
	if err != nil {
		return 0, fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	memStore, err := memory.NewStoreFromDB(db)
	if err != nil {
		return 0, fmt.Errorf("create memory store: %w", err)
	}

	// Get all active facts above minimum importance.
	facts, err := memStore.GetActiveFactsAboveImportance(ctx, 0.4)
	if err != nil {
		return 0, fmt.Errorf("get facts: %w", err)
	}

	if len(facts) == 0 {
		return 0, nil
	}

	// Group facts by category → wiki category mapping.
	categoryMap := map[string]string{
		"decision":   "결정",
		"preference": "선호",
		"solution":   "기술",
		"context":    "업무",
		"user_model": "선호",
		"mutual":     "선호",
	}

	// Group by wiki category.
	byCategory := map[string][]memory.Fact{}
	for _, f := range facts {
		wikiCat := categoryMap[f.Category]
		if wikiCat == "" {
			wikiCat = "업무"
		}
		byCategory[wikiCat] = append(byCategory[wikiCat], f)
	}

	created := 0
	today := time.Now().Format("2006-01-02")

	for wikiCat, catFacts := range byCategory {
		// Create one page per category with all facts.
		slug := fmt.Sprintf("migration-%s", today)
		relPath := wikiCat + "/" + slug + ".md"
		title := fmt.Sprintf("%s 이관 (%s)", wikiCat, today)

		var body strings.Builder
		body.WriteString(fmt.Sprintf("# %s\n\n", title))
		body.WriteString("## 요약\n")
		body.WriteString(fmt.Sprintf("memory.db에서 이관된 %d개 팩트.\n\n", len(catFacts)))
		body.WriteString("## 핵심 사실\n")

		// Sort by importance descending.
		sort.Slice(catFacts, func(i, j int) bool {
			return catFacts[i].Importance > catFacts[j].Importance
		})

		for _, f := range catFacts {
			date := f.CreatedAt.Format("2006-01-02")
			body.WriteString(fmt.Sprintf("- %s [%s, %s, %.1f]\n", f.Content, f.Category, date, f.Importance))
		}

		body.WriteString(fmt.Sprintf("\n## 변경 이력\n- %s: memory.db에서 자동 이관\n", today))

		if dryRun {
			log.Printf("  [dry-run] would create: %s (%d facts)", relPath, len(catFacts))
		} else {
			page := wiki.NewPage(title, wikiCat, []string{"이관", "memory-db"})
			page.Meta.Importance = highestImportance(catFacts)
			page.Body = body.String()
			if err := store.WritePage(relPath, page); err != nil {
				log.Printf("  WARNING: write %s: %v", relPath, err)
				continue
			}
		}
		created++
	}

	return created, nil
}

// migrateProjects copies project markdown files from projects/ to wiki/프로젝트/.
func migrateProjects(projectsDir, wikiDir string, store *wiki.Store, dryRun bool) (int, error) {
	created := 0
	today := time.Now().Format("2006-01-02")

	err := filepath.Walk(projectsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		// Skip INDEX.md.
		if strings.ToUpper(filepath.Base(path)) == "INDEX.MD" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		rel, _ := filepath.Rel(projectsDir, path)
		wikiPath := filepath.Join("프로젝트", rel)

		// Extract title from first heading or filename.
		title := strings.TrimSuffix(filepath.Base(path), ".md")
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "# ") {
				title = strings.TrimPrefix(line, "# ")
				title = strings.TrimSpace(title)
				break
			}
		}

		if dryRun {
			log.Printf("  [dry-run] would create: %s (%s, %d bytes)", wikiPath, title, len(data))
		} else {
			page := wiki.NewPage(title, "프로젝트", []string{"이관", "vega"})
			page.Meta.Created = today
			page.Body = string(data)
			if err := store.WritePage(wikiPath, page); err != nil {
				log.Printf("  WARNING: write %s: %v", wikiPath, err)
				return nil
			}
		}
		created++
		return nil
	})

	return created, err
}

// migrateUserModel reads user_model entries and creates 사용자.md.
func migrateUserModel(ctx context.Context, dbPath, wikiDir string, store *wiki.Store, dryRun bool) (int, error) {
	db, err := sql.Open("sqlite3", dbPath+"?mode=ro")
	if err != nil {
		return 0, fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	memStore, err := memory.NewStoreFromDB(db)
	if err != nil {
		return 0, fmt.Errorf("create memory store: %w", err)
	}

	entries, err := memStore.GetUserModel(ctx)
	if err != nil {
		return 0, fmt.Errorf("get user model: %w", err)
	}

	if len(entries) == 0 {
		return 0, nil
	}

	today := time.Now().Format("2006-01-02")
	var body strings.Builder
	body.WriteString("# 사용자 프로필\n\n")
	body.WriteString("## 요약\nmemory.db user_model에서 이관된 사용자 정보.\n\n")
	body.WriteString("## 핵심 사실\n")

	for _, e := range entries {
		body.WriteString(fmt.Sprintf("- **%s**: %s (신뢰도: %.2f)\n", e.Key, e.Value, e.Confidence))
	}

	body.WriteString(fmt.Sprintf("\n## 변경 이력\n- %s: memory.db에서 자동 이관\n", today))

	if dryRun {
		log.Printf("  [dry-run] would create: 사용자.md (%d entries)", len(entries))
	} else {
		page := wiki.NewPage("사용자 프로필", "사람", []string{"사용자", "이관"})
		page.Meta.Importance = 0.9
		page.Body = body.String()
		if err := store.WritePage("사용자.md", page); err != nil {
			return 0, fmt.Errorf("write 사용자.md: %w", err)
		}
	}

	return 1, nil
}

func highestImportance(facts []memory.Fact) float64 {
	max := 0.5
	for _, f := range facts {
		if f.Importance > max {
			max = f.Importance
		}
	}
	return max
}
