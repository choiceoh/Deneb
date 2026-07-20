// Package wikiport exposes the stable, narrow contracts that non-wiki
// packages need from the file-backed wiki domain.
package wikiport

import (
	"context"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

type (
	Store                   = wiki.Store
	Config                  = wiki.Config
	SearchOptions           = wiki.SearchOptions
	RecallEvent             = wiki.RecallEvent
	RecallUsage             = wiki.RecallUsage
	WikiDreamer             = wiki.WikiDreamer
	Page                    = wiki.Page
	Frontmatter             = wiki.Frontmatter
	H2Section               = wiki.H2Section
	SearchResult            = wiki.SearchResult
	SearchMode              = wiki.SearchMode
	QueryOptions            = wiki.QueryOptions
	QueryKind               = wiki.QueryKind
	QueryClause             = wiki.QueryClause
	QueryPlan               = wiki.QueryPlan
	QueryClauseDiagnostic   = wiki.QueryClauseDiagnostic
	SearchSignalExplanation = wiki.SearchSignalExplanation
	SearchExplanation       = wiki.SearchExplanation
	SearchDropSummary       = wiki.SearchDropSummary
	SearchDiagnostics       = wiki.SearchDiagnostics
	SearchReport            = wiki.SearchReport
	SemanticIndexStatus     = wiki.SemanticIndexStatus
	SearchProbeStatus       = wiki.SearchProbeStatus
	RerankerStatus          = wiki.RerankerStatus
	SearchDoctorReport      = wiki.SearchDoctorReport
	DiaryHit                = wiki.DiaryHit
	StoreStats              = wiki.StoreStats
	Tier1Result             = wiki.Tier1Result
	IndexEntry              = wiki.IndexEntry
	ProjectStatus           = wiki.ProjectStatus
	ProjectSite             = wiki.ProjectSite
	SiteFields              = wiki.SiteFields
	ContactEnrichResult     = wiki.ContactEnrichResult
	DealRecord              = wiki.DealRecord
	DealRecordFilter        = wiki.DealRecordFilter
	DealTotals              = wiki.DealTotals
	DealPageInput           = wiki.DealPageInput
	DealTerms               = wiki.DealTerms
	QuotedTerm              = wiki.QuotedTerm
	ProjectRef              = wiki.ProjectRef
	CounterpartyRef         = wiki.CounterpartyRef
	SimilarQuery            = wiki.SimilarQuery
	SimilarHit              = wiki.SimilarHit
	OpenQuestionItem        = wiki.OpenQuestionItem
	OpenQuestion            = wiki.OpenQuestion
	CloseResult             = wiki.CloseResult
	ReopenResult            = wiki.ReopenResult
	MergeOptions            = wiki.MergeOptions
	MergeResult             = wiki.MergeResult
	SnapshotResult          = wiki.SnapshotResult
	PersonSeed              = wiki.PersonSeed
)

const (
	RepPageFile        = wiki.RepPageFile
	LogPageFile        = wiki.LogPageFile
	LogKeepSections    = wiki.LogKeepSections
	RepSkeletonMarker  = wiki.RepSkeletonMarker
	SearchModeAuto     = wiki.SearchModeAuto
	SearchModeBM25     = wiki.SearchModeBM25
	SearchModeSemantic = wiki.SearchModeSemantic
	SearchModeHybrid   = wiki.SearchModeHybrid
	SearchModeFull     = wiki.SearchModeFull
	QueryKindLex       = wiki.QueryKindLex
	QueryKindVec       = wiki.QueryKindVec
	QueryKindHyDE      = wiki.QueryKindHyDE
)

// NormalizeSiteStatus accepts 후보/계약/개설/준공 or "" (미분류).
func NormalizeSiteStatus(status string) (string, error) {
	return wiki.NormalizeSiteStatus(status)
}

// Tier1Store is the prompt-injection read surface for high-importance pages.
type Tier1Store interface {
	Tier1Pages(minImportance float64) []Tier1Result
}

// RelatedSearchStore is the small cross-feature enrichment surface used to find
// nearby wiki context for another record.
type RelatedSearchStore interface {
	Search(ctx context.Context, query string, limit int) ([]SearchResult, error)
}

// DiaryRoot exposes only the diary directory needed by source ingesters.
type DiaryRoot interface {
	DiaryDir() string
}

// WikiPageReader is the live page-read surface used by grounded brief tools.
type WikiPageReader interface {
	ReadPage(relPath string) (*Page, error)
}

// CounterpartyDomainStore is the active-counterparty boost contract.
type CounterpartyDomainStore interface {
	ActiveCounterpartyDomains(cutoff string) map[string]struct{}
}

// CounterpartyProjectStore is the mail-analysis project-anchor contract.
type CounterpartyProjectStore interface {
	CounterpartyProjects(cutoff string) map[string][]string
}

// ProjectStatusSource is the miniapp project digest contract.
type ProjectStatusSource interface {
	ProjectStatuses() ([]ProjectStatus, error)
}

// MemoryStore is the miniapp memory read/write contract. It is intentionally
// named at the feature boundary so handlers do not depend on wiki.Store itself.
type MemoryStore interface {
	Search(ctx context.Context, query string, limit int) ([]SearchResult, error)
	SearchDiary(ctx context.Context, query string, limit int) ([]DiaryHit, error)
	ReadPage(relPath string) (*Page, error)
	WritePage(relPath string, page *Page) error
	DeletePage(relPath string) error
	MovePage(from, to string) error
	Stats() StoreStats
	ListPages(category string) ([]string, error)
	RecentDiaryEntries(limit int) []DiaryHit
}

func NewPage(title, category string, tags []string) *Page {
	return wiki.NewPage(title, category, tags)
}

func NewStore(dir, diaryDir string) (*Store, error) {
	return wiki.NewStore(dir, diaryDir)
}

func NewStoreWithSearchOptions(dir, diaryDir string, options SearchOptions) (*Store, error) {
	return wiki.NewStoreWithSearchOptions(dir, diaryDir, options)
}

func ConfigFromEnv() Config {
	return wiki.ConfigFromEnv()
}

func NewWikiDreamer(store *Store, client *llm.Client, model string, cfg Config, logger *slog.Logger) *WikiDreamer {
	return wiki.NewWikiDreamer(store, client, model, cfg, logger)
}

func ParsePageFile(path string) (*Page, error) {
	return wiki.ParsePageFile(path)
}

func ValidateExternalPath(rel string) error {
	return wiki.ValidateExternalPath(rel)
}

func NormalizePagePath(relPath string) string {
	return wiki.NormalizePagePath(relPath)
}

func NormalizeProjectPagePath(relPath string) string {
	return wiki.NormalizeProjectPagePath(relPath)
}

func ValidateCategory(cat string) bool {
	return wiki.ValidateCategory(cat)
}

func Categories() []string {
	return append([]string(nil), wiki.Categories...)
}

func DroppedEnumNotes(stage string, kinds []string) []string {
	return wiki.DroppedEnumNotes(stage, kinds)
}

func AppendDiaryTo(diaryDir, content string) error {
	return wiki.AppendDiaryTo(diaryDir, content)
}

func ExtractWikiLinks(body string) []string {
	return wiki.ExtractWikiLinks(body)
}

func ParseQueryPlan(input string) QueryPlan {
	return wiki.ParseQueryPlan(input)
}

func LogArchivePath(project string) string {
	return wiki.LogArchivePath(project)
}

func LogPagePath(project string) string {
	return wiki.LogPagePath(project)
}

func MailAnalysisPagePath(project, msgID string) string {
	return wiki.MailAnalysisPagePath(project, msgID)
}

func MaterialPagePath(project, filename string) string {
	return wiki.MaterialPagePath(project, filename)
}

func MeetingPagePath(project, filename string) string {
	return wiki.MeetingPagePath(project, filename)
}

func ProjectNameOf(relPath string) (string, bool) {
	return wiki.ProjectNameOf(relPath)
}

func ProjectFolderOf(relPath string) (string, bool) {
	return wiki.ProjectFolderOf(relPath)
}

func IsProjectRepPage(relPath string) bool {
	return wiki.IsProjectRepPage(relPath)
}

func IsProjectRawDataPath(relPath string) bool {
	return wiki.IsProjectRawDataPath(relPath)
}

func IsProjectLogPage(relPath string) bool {
	return wiki.IsProjectLogPage(relPath)
}

func IsMailAnalysisPath(relPath string) bool {
	return wiki.IsMailAnalysisPath(relPath)
}

func IsMaterialPath(relPath string) bool {
	return wiki.IsMaterialPath(relPath)
}

// Recall-utility ledger event kinds (see wiki.RecordRecallEvents).
const (
	RecallEventInject = wiki.RecallEventInject
	RecallEventRead   = wiki.RecallEventRead
	RecallEventCite   = wiki.RecallEventCite
)

func OpenQuestionsIn(body string) []OpenQuestionItem {
	return wiki.OpenQuestionsIn(body)
}

func CollectStaleOpenQuestions(wikiDir string, minAgeDays int, now time.Time) []OpenQuestion {
	return wiki.CollectStaleOpenQuestions(wikiDir, minAgeDays, now)
}

func LoadWikiBrief(workspaceDir string) string {
	return wiki.LoadWikiBrief(workspaceDir)
}

func WikiBriefSection(brief string) string {
	return wiki.WikiBriefSection(brief)
}

func BuildGraphSnapshot(ctx context.Context, store *Store, outDir string, runCluster bool) (*SnapshotResult, error) {
	return wiki.BuildGraphSnapshot(ctx, store, outDir, runCluster)
}

func SumDealRecords(recs []DealRecord) DealTotals {
	return wiki.SumDealRecords(recs)
}
