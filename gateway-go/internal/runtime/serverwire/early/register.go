// Early-phase RPC domain registration (table-driven Handler Deps assembly).
package early

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/notebook"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/document"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localcal"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailwork"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/configresolve"
	runtimeheartbeat "github.com/choiceoh/deneb/gateway-go/internal/runtime/heartbeat"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/mailflow"
	handleragent "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/agent"
	handlercheckpoint "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/checkpoint"
	handlerevents "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerevents"
	handlerminiapp "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp"
	minifiles "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/files"
	miniknowledge "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/knowledge"
	minischedule "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/schedule"
	handlerinsights "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/insights"
	handlermail "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/mail"
	handlerobservatory "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/observatory"
	handlerobserve "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/observe"
	handlerprocess "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/process"
	handlerprovider "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/provider"
	handlersession "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/session"
	handlerskill "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/skill"
	handlersystem "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/system"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/serverwire"
)

// RegisterEarlyDomains preserves the historical domain order while
// keeping phase orchestration independent from handler dependency assembly.
func RegisterDomains(hub *rpcutil.GatewayHub, denebDir string, w *serverwire.Ports, capabilities EarlyCapabilities) {
	// Table-driven domain registration: one slice, one loop.
	// Deps assembled inline from hub accessors — no adapter layer.
	domains := earlyCoreMethods(w, hub, denebDir, capabilities)
	domains = append(domains, earlyNativeClientMethods(w, hub, capabilities)...)
	domains = append(domains, earlyMailAndCalendarMethods(w, denebDir)...)
	domains = append(domains, earlyPlanningMethods(w, hub)...)
	domains = append(domains, earlyKnowledgeMethods(w, hub)...)
	domains = append(domains, earlyImprovementMethods(w, hub)...)

	// Provider methods are the only capability group omitted entirely when its
	// backing registry is unavailable.
	domains = append(domains, EarlyProviderMethods(w)...)

	for _, d := range domains {
		if d != nil {
			w.Dispatcher.RegisterDomain(d)
		}
	}
}

// earlyCoreMethods owns the transport-agnostic control plane. Its order is
// intentionally stable: sessions and health precede orchestration, followed by
// tools, events, scheduling, observability, and lifecycle operations.
func earlyCoreMethods(w *serverwire.Ports, hub *rpcutil.GatewayHub, denebDir string, capabilities EarlyCapabilities) []map[string]rpcutil.HandlerFunc {
	return []map[string]rpcutil.HandlerFunc{
		handlersession.CRUDMethods(handlersession.Deps{
			Sessions:    hub.Sessions(),
			GatewaySubs: hub.GatewaySubs(),
			Transcripts: func() (handlersession.TranscriptDeleter, error) {
				if w.ToolDeps == nil || w.ToolDeps.Sessions.Transcript == nil {
					return nil, serverwire.ErrTranscriptUnavailable
				}
				return w.ToolDeps.Sessions.Transcript, nil
			},
		}),
		handlersystem.HealthMethods(handlersystem.HealthDeps{
			SessionCount: hub.Sessions().Count,
			Version:      hub.Version(),
		}),
		handleragent.ExtendedMethods(handleragent.ExtendedDeps{
			Sessions:    hub.Sessions(),
			GatewaySubs: hub.GatewaySubs(),
			Processes:   hub.Processes(),
			CronService: hub.CronService(),
			Broadcaster: hub.Broadcast,
		}),
		handlerprocess.ACPMethods(w.ACPDeps),
		handlerskill.ToolMethods(handlerskill.ToolDeps{Processes: hub.Processes()}),
		handlerskill.Methods(handlerskill.Deps{
			Skills:      hub.Skills(),
			Broadcaster: hub.Broadcast,
		}),
		handlerevents.BroadcastMethods(handlerevents.EventsDeps{
			Broadcaster: hub.Broadcaster(),
			Logger:      hub.Logger(),
		}),
		handlerevents.EventsMethods(handlerevents.EventsDeps{
			Broadcaster: hub.Broadcaster(),
			Logger:      hub.Logger(),
		}),
		handlerprocess.CronAdvancedMethods(handlerprocess.CronAdvancedDeps{
			Service:     hub.CronService(),
			RunLog:      hub.CronPersistLog(),
			Broadcaster: hub.Broadcast,
		}),
		handlerprocess.CronServiceMethods(handlerprocess.CronServiceDeps{Service: hub.CronService()}),
		handlersystem.IdentityMethods(hub.Version()),
		handlersystem.MonitoringMethods(handlersystem.MonitoringDeps{Dispatcher: w.Dispatcher}),
		handlersystem.ConfigAdvancedMethods(handlersystem.ConfigAdvancedDeps{Broadcaster: hub.Broadcast}),
		handlersystem.UsageMethods(handlersystem.UsageDeps{Tracker: w.UsageTracker}),
		handlersystem.LogsMethods(handlersystem.LogsDeps{LogDir: filepath.Join(denebDir, "logs")}),
		handlerobserve.Methods(capabilities.Observe),
		handlerobservatory.Methods(capabilities.Observatory),
		handlerinsights.Methods(handlerinsights.Deps{
			Engine: hub.Insights(),
			Logger: hub.Logger(),
		}),
		handlercheckpoint.Methods(handlercheckpoint.Deps{
			Root:   filepath.Join(denebDir, "checkpoints"),
			Logger: hub.Logger(),
		}),
		handlersystem.MaintenanceMethods(handlersystem.MaintenanceDeps{Runner: w.MaintRunner}),
		handlersystem.UpdateMethods(handlersystem.UpdateDeps{DenebDir: denebDir}),
	}
}

// earlyNativeClientMethods owns the authenticated miniapp transport surface
// and shared native stores. Product domains are registered by later groups.
func earlyNativeClientMethods(w *serverwire.Ports, hub *rpcutil.GatewayHub, capabilities EarlyCapabilities) []map[string]rpcutil.HandlerFunc {
	return []map[string]rpcutil.HandlerFunc{
		handlerobserve.MiniappMethods(capabilities.Observe),
		handlerobservatory.MiniappMethods(capabilities.Observatory),
		EarlyMiniappGatewayMethods(w, hub),
		capabilities.Miniapp,
		// DeliveryEnabled keeps the client on background SSE when device tokens
		// exist but the server has no configured FCM sender.
		handlerminiapp.PushMethods(handlerminiapp.PushDeps{
			Store:           w.Caps.PushTokenStore,
			DeliveryEnabled: func() bool { return w.Caps.PushNotifier != nil },
		}),
		handlerminiapp.WormholeMethods(handlerminiapp.WormholeDeps{}),
		handlerminiapp.WorkFeedMethods(handlerminiapp.WorkFeedDeps{
			Store:          capabilities.NativeWorkFeed,
			OnAnswer:       w.WorkFeed.OnAnswer,
			OnMetaProposal: w.WorkFeed.OnMetaProposal,
		}),
		// miniapp.models.* is deliberately registered in registerLateMethods:
		// the picker snapshots the model registry and chat handler at creation.
		earlyFileMethods(w),
	}
}

func earlyMailAndCalendarMethods(w *serverwire.Ports, denebDir string) []map[string]rpcutil.HandlerFunc {
	return []map[string]rpcutil.HandlerFunc{
		serverwire.WithMailAliases(handlermail.GmailMethods(handlermail.GmailDeps{
			Client:        w.Mail.ClientFactory(denebDir),
			AnalysisCache: handlermail.NewAnalysisStore(filepath.Join(denebDir, "cache", "mail_analysis")),
			WorkState:     mailwork.New(filepath.Join(denebDir, "mail_work_state.json")),
			MailStore: func() handlermail.MailStoreReader {
				if w.MailStore == nil {
					return nil
				}
				return w.MailStore
			},
			Priority: func() func(from, subject, snippet string) (string, string) {
				counterparties := mailflow.NewCounterpartyLookup(func() *wiki.Store { return w.WikiStore })
				return func(from, subject, snippet string) (string, string) {
					tier, hint := serverwire.MailPriorityScorer(w.ContactsStore, counterparties).Score(from, subject, snippet)
					return string(tier), hint
				}
			}(),
		})),
		minischedule.CalendarMethods(minischedule.CalendarDeps{
			Client: func() (minischedule.CalendarClient, error) {
				return calendar.DefaultClient()
			},
			Local:     serverwire.ResolveLocalCalendar(w.Logger),
			Proposals: serverwire.ResolveCalendarProposals(w.Logger),
		}),
	}
}

func earlyPlanningMethods(w *serverwire.Ports, hub *rpcutil.GatewayHub) []map[string]rpcutil.HandlerFunc {
	return []map[string]rpcutil.HandlerFunc{
		EarlyProjectMethods(w, hub),
		handlerminiapp.OrgMethods(w.OrgDeps),
		minischedule.TodoMethods(minischedule.TodoDeps{Store: serverwire.ResolveLocalTodos(w.Logger)}),
	}
}

// earlyKnowledgeMethods keeps every late-bound knowledge source behind its
// request-time factory so an unavailable wiki, notebook, or cron service does
// not prevent gateway startup.
func earlyKnowledgeMethods(w *serverwire.Ports, hub *rpcutil.GatewayHub) []map[string]rpcutil.HandlerFunc {
	wikiStore := func() (miniknowledge.MemorySearcher, error) {
		store := hub.WikiStore()
		if store == nil {
			return nil, serverwire.ErrWikiDisabled
		}
		return store, nil
	}

	return []map[string]rpcutil.HandlerFunc{
		miniknowledge.MemoryMethods(miniknowledge.MemoryDeps{
			Store:      wikiStore,
			StartMerge: w.MakeWikiMergeStarter(hub),
		}),
		miniknowledge.NotebookMethods(miniknowledge.NotebookDeps{
			Store: func() (*notebook.Store, error) {
				if w.NotebookStore == nil {
					return nil, serverwire.ErrNotebookDisabled
				}
				return w.NotebookStore, nil
			},
		}),
		minischedule.CronsMethods(minischedule.CronsDeps{
			Service: func() (minischedule.CronService, error) {
				service := hub.CronService()
				if service == nil {
					return nil, serverwire.ErrCronUnavailable
				}
				return service, nil
			},
		}),
		handlerminiapp.PromptMethods(handlerminiapp.PromptDeps{Store: w.PromptStore}),
		handlerminiapp.PromptTunerMethods(handlerminiapp.PromptTunerDeps{
			Tuner: func() handlerminiapp.PromptTuner {
				if w.ModelMaintenance == nil {
					return nil
				}
				return w.ModelMaintenance.PromptTuner()
			},
		}),
		miniknowledge.TopicDocsMethods(miniknowledge.TopicDocsDeps{
			TopicsDir:  func() (string, error) { return configresolve.TopicsDir(), nil },
			CurrentKey: configresolve.CurrentTopicKey,
			ApplyNow:   prompt.Cache.ClearAllTopicSnapshots,
		}),
		serverwire.WithMailAliases(handlermail.GmailContextMethods(handlermail.GmailContextDeps{
			Client: func() (handlermail.GmailClient, error) {
				return gmail.DefaultClient()
			},
			WikiStore: wikiStore,
			SenderFacts: func(ctx context.Context, from string) string {
				if facts := w.Mail.SenderFacts(ctx, from); facts != "" {
					return facts
				}
				return mailanalysis.ExtractSenderFacts(ctx, from)
			},
		})),
		miniknowledge.PeopleMethods(miniknowledge.PeopleDeps{
			Client: func() (miniknowledge.PeopleClient, error) {
				return gmail.DefaultClient()
			},
			WikiStore: wikiStore,
		}),
	}
}

func earlyImprovementMethods(w *serverwire.Ports, hub *rpcutil.GatewayHub) []map[string]rpcutil.HandlerFunc {
	return []map[string]rpcutil.HandlerFunc{
		EarlySkillMethods(w),
		EarlySelfImprovementMethods(w),
		earlyRSIStatusMethods(w),
		miniknowledge.SearchMethods(miniknowledge.SearchDeps{
			Store: func() (miniknowledge.MemorySearcher, error) {
				store := hub.WikiStore()
				if store == nil {
					return nil, serverwire.ErrWikiDisabled
				}
				return store, nil
			},
			Client: func() (miniknowledge.PeopleClient, error) {
				return gmail.DefaultClient()
			},
		}),
	}
}

// earlyMiniappGatewayMethods wires the native client's boot identity and
// capability snapshot. All readiness probes are lazy because chat, wiki, and
// model services become available after the early phase.
func EarlyMiniappGatewayMethods(w *serverwire.Ports, hub *rpcutil.GatewayHub) map[string]rpcutil.HandlerFunc {
	return handlerminiapp.Methods(handlerminiapp.Deps{
		Version: hub.Version(),
		CurrentModel: func() string {
			if w.ChatHandler != nil {
				if model := w.ChatHandler.DefaultModel(); model != "" {
					return model
				}
			}
			if w.ModelRegistry != nil {
				return w.ModelRegistry.FullModelID(modelrole.RoleMain)
			}
			return ""
		},
		Capabilities: func() map[string]bool {
			wikiReady := hub.WikiStore() != nil
			chatReady := w.ChatHandler != nil
			return map[string]bool{
				"rpc":             true,
				"chat":            chatReady,
				"chatStream":      chatReady,
				"events":          w.Caps.PushHub != nil,
				"models":          w.ModelRegistry != nil,
				"gmail":           true,
				"calendar":        true,
				"wiki":            wikiReady,
				"search":          wikiReady,
				"people":          true,
				"crons":           hub.CronService() != nil,
				"captureImage":    chatReady,
				"captureAudio":    chatReady,
				"captureContacts": hub.ContactsStore() != nil,
				"workFeed":        w.Caps.WorkFeedStore != nil,
				"workFeedActions": w.Caps.WorkFeedStore != nil,
				"nativeSync":      w.Caps.NativeSyncStore != nil,
				"pushRegister":    w.Caps.PushTokenStore != nil,
				"pushFallback":    w.Caps.PushNotifier != nil,
				"gmailAttachment": true,
				"updateManifest":  true,
				"prompts":         w.PromptStore != nil,
				"promptTuner":     w.ModelMaintenance != nil && w.ModelMaintenance.PromptTuner() != nil,
				"topicDocs":       configresolve.CurrentTopicKey() != "",
			}
		},
	})
}

// earlyFileMethods wires local file operations and the late-bound semantic
// index. Mutations invalidate stale vectors immediately; the periodic indexer
// repopulates them with current content.
func earlyFileMethods(w *serverwire.Ports) map[string]rpcutil.HandlerFunc {
	return minifiles.FilesBrowseMethods(minifiles.FilesBrowseDeps{
		Store: serverwire.LocalFileStoreOrNil(w.Logger),
		ExtractText: func(ctx context.Context, data []byte, name string) string {
			text, _ := document.ExtractDocumentText(ctx, data, name, "")
			return text
		},
		SemanticSearch: func(ctx context.Context, query string, max int) ([]filestore.ScoredEntry, error) {
			if w.FileSemindex == nil {
				return nil, nil
			}
			return w.FileSemindex.Search(ctx, query, max)
		},
		OnDelete: func(path string) {
			if w.FileSemindex != nil {
				w.FileSemindex.Remove(path)
			}
		},
		OnMove: func(oldPath, newPath string) {
			if w.FileSemindex != nil {
				w.FileSemindex.Rename(oldPath, newPath)
			}
		},
		OnUpload: func(path string) {
			if w.FileSemindex != nil {
				w.FileSemindex.Remove(path)
			}
		},
	})
}

// earlyProjectMethods wires the project dashboard to lazy snapshots. Every
// source is resolved at request time so session-phase stores are visible.
func EarlyProjectMethods(w *serverwire.Ports, hub *rpcutil.GatewayHub) map[string]rpcutil.HandlerFunc {
	return handlerminiapp.ProjectMethods(handlerminiapp.ProjectDeps{
		Wiki: func() (handlerminiapp.ProjectStatusSource, error) {
			store := hub.WikiStore()
			if store == nil {
				return nil, serverwire.ErrWikiDisabled
			}
			return store, nil
		},
		Notebooks: func() []handlerminiapp.ProjectLinkedNotebook {
			if w.NotebookStore == nil {
				return nil
			}
			notebooks := w.NotebookStore.List()
			out := make([]handlerminiapp.ProjectLinkedNotebook, 0, len(notebooks))
			for _, notebook := range notebooks {
				out = append(out, handlerminiapp.ProjectLinkedNotebook{
					ID: notebook.ID, DealRef: notebook.DealRef, ProjectRefs: notebook.ProjectRefs,
				})
			}
			return out
		},
		WorkItems: func() []handlerminiapp.ProjectLinkedWorkItem {
			feed := w.WorkFeed.Store
			if feed == nil {
				return nil
			}
			items, _, err := feed.List(1000, true)
			if err != nil {
				return nil
			}
			out := make([]handlerminiapp.ProjectLinkedWorkItem, 0, len(items))
			for _, item := range items {
				out = append(out, handlerminiapp.ProjectLinkedWorkItem{ID: item.ID, RefID: item.RefID})
			}
			return out
		},
		Calendar: func() []handlerminiapp.ProjectLinkedCalEvent {
			store, err := localcal.Default()
			if err != nil || store == nil {
				return nil
			}
			now := time.Now()
			events := store.ListRange(now.AddDate(-1, 0, 0), now.AddDate(1, 0, 0))
			out := make([]handlerminiapp.ProjectLinkedCalEvent, 0, len(events))
			for _, event := range events {
				if event.Source == "" {
					continue
				}
				out = append(out, handlerminiapp.ProjectLinkedCalEvent{ID: event.ID, Source: event.Source})
			}
			return out
		},
	})
}

// earlySkillMethods wires the native skill catalog to late-bound Genesis
// projections. Missing tracker state degrades to an unenriched catalog.
func EarlySkillMethods(w *serverwire.Ports) map[string]rpcutil.HandlerFunc {
	return handlerminiapp.SkillsMethods(handlerminiapp.SkillsDeps{
		List: func() []skills.SkillEntry {
			var toolNames []string
			if w.ChatHandler != nil {
				toolNames = w.ChatHandler.ToolNames()
			}
			return chat.EligibleWorkspaceSkills(configresolve.WorkspaceDir(), toolNames)
		},
		CuratorRecords: func() ([]genesis.SkillCuratorRecord, error) {
			if w.Genesis.Tracker == nil {
				return nil, nil
			}
			return w.Genesis.Tracker.SkillCuratorReport("")
		},
		UsageStats: func() ([]genesis.UsageStats, error) {
			if w.Genesis.Tracker == nil {
				return nil, nil
			}
			return w.Genesis.Tracker.ListAllStats()
		},
		RecentLifecycle: func(limit int) ([]genesis.LifecycleLogEntry, error) {
			if w.Genesis.Tracker == nil {
				return nil, nil
			}
			return w.Genesis.Tracker.RecentLifecycleLog(limit)
		},
		ValidationSummary: func(skillName string) (genesis.SkillValidationCaseSummary, error) {
			if w.Genesis.Tracker == nil {
				return genesis.SkillValidationCaseSummary{SkillName: strings.TrimSpace(skillName)}, nil
			}
			return w.Genesis.Tracker.ValidationCaseSummary(strings.TrimSpace(skillName))
		},
		RecentOpportunities: func(skillName string, limit int) ([]genesis.SkillOpportunityRecord, error) {
			if w.Genesis.Tracker == nil {
				return nil, nil
			}
			return w.Genesis.Tracker.RecentSkillOpportunities(strings.TrimSpace(skillName), limit)
		},
		RecentSelfCorrections: func(skillName string, limit int) ([]genesis.SelfCorrectionCandidateRecord, error) {
			if w.Genesis.Tracker == nil {
				return nil, nil
			}
			return w.Genesis.Tracker.RecentSelfCorrectionCandidates(strings.TrimSpace(skillName), genesis.SelfCorrectionStatusProposed, limit)
		},
		SelfHarnessSignals: func() genesis.SelfHarnessSignalSummary {
			if w.Genesis.Tracker == nil {
				return genesis.SelfHarnessSignalSummary{}
			}
			return w.Genesis.Tracker.SelfHarnessSignals()
		},
		InvalidateSkills: chat.InvalidateSkillsCache,
	})
}

func EarlySelfImprovementMethods(w *serverwire.Ports) map[string]rpcutil.HandlerFunc {
	return handlerminiapp.SelfImprovementCodingMethods(handlerminiapp.SelfImprovementCodingDeps{
		RecentCandidates: func(status string, limit int) ([]genesis.SelfCorrectionCandidateRecord, error) {
			if w.Genesis.Tracker == nil {
				return nil, nil
			}
			return w.Genesis.Tracker.RecentSelfCorrectionCandidates("", status, limit)
		},
		RecordCandidate: func(rec genesis.SelfCorrectionCandidateRecord) (genesis.SelfCorrectionCandidateRecord, error) {
			if w.Genesis.Tracker == nil {
				return genesis.SelfCorrectionCandidateRecord{}, errors.New("genesis tracker unavailable")
			}
			return w.Genesis.Tracker.RecordSelfCorrectionCandidate(rec)
		},
		RecordDispatch: func(rec genesis.SelfCorrectionCandidateRecord) (genesis.SelfCorrectionCandidateRecord, error) {
			if w.Genesis.Tracker == nil {
				return genesis.SelfCorrectionCandidateRecord{}, errors.New("genesis tracker unavailable")
			}
			return w.Genesis.Tracker.RecordSelfCorrectionDispatch(rec)
		},
		Funnel: func() genesis.SelfCorrectionFunnelSummary {
			if w.Genesis.Tracker == nil {
				return genesis.SelfCorrectionFunnelSummary{}
			}
			return w.Genesis.Tracker.SelfCorrectionFunnel()
		},
		LastNudgeAtMs: runtimeheartbeat.LastSelfCodingNudgeAtMillis,
	})
}

func earlyRSIStatusMethods(w *serverwire.Ports) map[string]rpcutil.HandlerFunc {
	return handlerminiapp.RSIStatusMethods(handlerminiapp.RSIStatusDeps{
		Status: func() genesis.RSILoopStatus {
			if w.Genesis.Tracker == nil {
				return genesis.RSILoopStatus{}
			}
			return w.Genesis.Tracker.RSIStatus()
		},
	})
}

func EarlyProviderMethods(w *serverwire.Ports) []map[string]rpcutil.HandlerFunc {
	if w.Providers == nil {
		return nil
	}
	return []map[string]rpcutil.HandlerFunc{
		handlerprovider.Methods(handlerprovider.Deps{
			Providers:   w.Providers,
			AuthManager: w.AuthManager,
		}),
		handlerprovider.ModelsMethods(handlerprovider.ModelsDeps{
			Providers: w.Providers,
		}),
	}
}
