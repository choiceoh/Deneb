// Package porttypes holds the explicit Ports bag and field groups shared by
// serverwire registration subpackages. Keeping concrete domain fan-out here
// confines wiring imports to one composition-root package near the soft bar.
package porttypes

import (
	"context"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/maintenance"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/nativesync"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/notebook"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/prompts"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/push"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/usage"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailstore"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/filesemindex"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/mailflow"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/modelmaintenance"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/phoneevents"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/proactive"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rolehealth"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc"
	handlerminiapp "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp"
	handlermail "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/mail"
	handlerprocess "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/process"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/skilllifecycle"
)

// WorkFeedMirror is the native work-feed store shared by miniapp RPC and chat capture.
type WorkFeedMirror interface {
	handlerminiapp.WorkFeedStore
	Append(item workfeed.Item) (workfeed.Item, error)
	Correct(id, note string) (workfeed.Item, error)
	Rewrite(id, newBody string) (workfeed.Item, error)
}

// WorkFeed groups work-feed store + operator action callbacks.
type WorkFeed struct {
	Store          WorkFeedMirror
	OnAnswer       func(item workfeed.Item, actionID string)
	OnMetaProposal func(item workfeed.Item, actionID string)
}

// MailAnalysis groups interactive mail-analyze wiring.
type MailAnalysis struct {
	WikiSink      func(handlermail.WikiAnalysisInput) error
	Models        func() (stage2 *llm.Client, stage2Model string, stage1 *llm.Client, stage1Model string)
	Prompt        func() string
	ProjectsFn    func() func() []mailanalysis.ProjectCandidate
	Ask           func() func(context.Context, string, []handlermail.QATurn, string) (string, error)
	ClientFactory func(denebDir string) func() (handlermail.GmailClient, error)
	SenderFacts   func(ctx context.Context, from string) string
	CpProjects    *mailflow.CounterpartyProjectsCache
}

// Phone groups phone-event ingest callbacks used by the miniapp chat bridge.
type Phone struct {
	Ledger          func() *phoneevents.Ledger
	OnLocationPlace func() func(string)
	ResolveAction   func(res phoneevents.ActionResult) bool
	ShutdownCtx     func() context.Context
}

// Genesis groups skill-genesis services registered in the late phase.
type Genesis struct {
	Svc         *skilllifecycle.GenesisService
	Evolver     *skilllifecycle.Evolver
	Tracker     *skilllifecycle.Tracker
	Transcripts toolport.TranscriptStore
}

// Caps are nil-check probes for miniapp.client capability bits.
type Caps struct {
	PushHub         *proactive.Hub
	WorkFeedStore   *workfeed.Store
	NativeSyncStore *nativesync.Store
	PushTokenStore  *push.Store
	PushNotifier    *push.Notifier
}

// WikiMergeStarter builds the wiki merge job starter used by miniapp memory RPCs.
type WikiMergeStarter func(hub *rpcutil.GatewayHub) func(targetPath, sourcePath string, target, source *wiki.Page)

// Ports is the explicit dependency bag Server fills before registering RPC domains.
type Ports struct {
	Logger     *slog.Logger
	Dispatcher *rpc.Dispatcher
	DenebDir   string

	ToolDeps         *chat.CoreToolDeps
	ACPDeps          *handlerprocess.ACPDeps
	UsageTracker     *usage.Tracker
	MaintRunner      *maintenance.Runner
	MailStore        *mailstore.Store
	WikiStore        *wiki.Store
	NotebookStore    *notebook.Store
	PromptStore      *prompts.Store
	ModelMaintenance *modelmaintenance.Suite
	ModelRegistry    *modelrole.Registry
	ChatHandler      *chat.Handler
	Providers        *provider.Registry
	AuthManager      *provider.AuthManager
	CronService      *cron.Service
	Sessions         *session.Manager
	FileSemindex     *filesemindex.Service
	RoleHealth       *rolehealth.Watch
	ProactiveRelay   proactive.Relay
	ContactsStore    *contacts.Store
	OrgDeps          handlerminiapp.OrgDeps

	Caps     Caps
	WorkFeed WorkFeed
	Mail     MailAnalysis
	Phone    Phone
	Genesis  Genesis

	RefreshCodingModelConsumers func()
	MakeWikiMergeStarter        WikiMergeStarter
}
