// Late-phase RPC domain registration (depends on chatHandler).
package late

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/artifact"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/document"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/routine"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailwork"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/configresolve"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/cronrunner"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/modelpicker"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/phoneevents"
	handlerchat "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/chat"
	miniknowledge "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/knowledge"
	handlermail "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/mail"
	handlersession "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/session"
	handlerskill "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/skill"
	handlerwiki "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/serverwire"
)

// RegisterLate registers RPC domains that depend on chatHandler.
// Called after registerSessionRPCMethods() which creates the chat handler.
func RegisterDomains(hub *rpcutil.GatewayHub, w *serverwire.Ports) {
	hub.AdvancePhase(rpcutil.PhaseLate)
	hub.SetWikiStore(w.WikiStore) // late-bound: created during session phase

	domains := []map[string]rpcutil.HandlerFunc{
		handlerchat.Methods(handlerchat.Deps{
			Chat:        w.ChatHandler,
			Broadcaster: hub.Broadcast,
		}),
		handlerchat.BtwMethods(handlerchat.BtwDeps{
			Chat:        w.ChatHandler,
			Broadcaster: hub.Broadcast,
		}),
		// Native-client chat bridge (miniapp.chat.send/history): lets the
		// standalone app drive a turn over the miniapp.* RPC surface via
		// SendSync, with deneb-ui emission enabled (channel "client").
		handlerchat.MiniappMethods(handlerchat.Deps{
			Chat:       w.ChatHandler,
			OcrImage:   document.OCRImage,
			Transcribe: artifact.TranscribeAudio,
			// Document attach (pdf/doc/sheet) → in-house extractor (PDF/Excel/Word/
			// PowerPoint/CSV/text, with a scanned-PDF / image OCR fallback).
			ExtractDocument: document.ExtractAttachmentText,
			// In-app browser in-place translation (en/ru → ko) — DeepL-only.
			Translate: tools.TranslateSegments,
			// Raw capture persistence: full OCR text / diarized transcript →
			// {memory}/captures/ + diary breadcrumb (recallable, dream-distilled,
			// backed up). The agent turn only summarizes; this keeps the original.
			SaveCapture: func(kind, context, text string) (string, error) {
				ws := hub.WikiStore()
				if ws == nil {
					return "", fmt.Errorf("wiki store unavailable")
				}
				return ws.SaveCapture(kind, context, text)
			},
			// Proper-noun bias for audio transcription, merged from two sources:
			// the wiki (people/companies/deals/domain terms) and the contacts
			// address book (every saved name + org). Either may be empty.
			Hotwords: func() string {
				var parts []string
				if ws := hub.WikiStore(); ws != nil {
					if h := ws.HotwordHints(150); h != "" {
						parts = append(parts, h)
					}
				}
				if cs := hub.ContactsStore(); cs != nil {
					if h := cs.HotwordHints(100); h != "" {
						parts = append(parts, h)
					}
				}
				return strings.Join(parts, ", ")
			},
			// Primary contacts sync: persist the whole address book into the
			// contacts store (phone lookup / name search / ASR hotwords).
			SaveContacts: func(contactsJSON []byte) (int, error) {
				cs := hub.ContactsStore()
				if cs == nil {
					return 0, fmt.Errorf("contacts store unavailable")
				}
				var p struct {
					Contacts []contacts.Contact `json:"contacts"`
				}
				if err := json.Unmarshal(contactsJSON, &p); err != nil {
					return 0, err
				}
				return cs.ReplaceAll(p.Contacts)
			},
			// Bonus: enrich existing wiki people (native-client contacts sync).
			// Enriches only 사람 pages already in the wiki — it creates none — so
			// the phone book strengthens the curated set without flooding it.
			EnrichContacts: func(contactsJSON []byte) (wiki.ContactEnrichResult, error) {
				ws := hub.WikiStore()
				if ws == nil {
					return wiki.ContactEnrichResult{}, fmt.Errorf("wiki store unavailable")
				}
				res, err := ws.EnrichContacts(contactsJSON)
				if err != nil {
					return res, err
				}
				// Also backfill each 인물 page's identity email(s) into frontmatter, so
				// mail senders / org members resolve to the page by email — the key that
				// disambiguates 동명이인 the name cannot. Best-effort (a bonus alongside
				// the 연락처 body enrichment); homonyms are flagged, not guessed.
				var p struct {
					Contacts []contacts.Contact `json:"contacts"`
				}
				if json.Unmarshal(contactsJSON, &p) == nil {
					// Give our own staff (company-domain contacts) an 인물 page even without
					// a wiki mention — they should be first-class identities. External people
					// stay mention-curated (the dreamer), so this never floods with the whole
					// phone book. Runs BEFORE the email backfill so the new pages get seeded.
					ourDomains := mailanalysis.OurMailDomains()
					if cr, cerr := ws.EnrichEmployeePages(p.Contacts, ourDomains); cerr == nil && len(cr.Created) > 0 {
						w.Logger.Info("employee pages created", "count", len(cr.Created))
					}
					// Also give a page to important EXTERNAL people — the counterparties our
					// 프로젝트/거래 pages actually name. Everyone else stays mention-curated.
					if dr, derr := ws.EnrichDealMentionedPages(p.Contacts, ourDomains); derr == nil && len(dr.Created) > 0 {
						w.Logger.Info("deal-mentioned pages created", "count", len(dr.Created))
					}
					if er, eerr := ws.EnrichPersonEmails(p.Contacts); eerr == nil && len(er.Ambiguous) > 0 {
						w.Logger.Info("person email backfill", "seeded", len(er.Updated), "homonyms_flagged", len(er.Ambiguous))
					}
				}
				return res, nil
			},
			WorkFeed: w.WorkFeed.Store,
			// Upgrade a shared document's feed card to a proper doc_analysis
			// deliverable. Wrapped so w.ProactiveRelay (built after the chat handler)
			// is resolved at call time.
			PublishDeliverable: func(text string) (bool, error) {
				return w.ProactiveRelay.PublishDeliverable(text)
			},
			IngestEvent: phoneevents.New(phoneevents.Config{
				ChatHandler:        w.ChatHandler,
				Relay:              &w.ProactiveRelay,
				ShutdownContext:    w.Phone.ShutdownCtx(),
				Logger:             w.Logger,
				Ledger:             w.Phone.Ledger(),
				OnLocationPlace:    w.Phone.OnLocationPlace(),
				ResolvePhoneAction: w.Phone.ResolveAction,
			}).IngestAsync,
		}),
		handlersession.ExecMethods(handlersession.ExecDeps{
			Chat:       w.ChatHandler,
			JobTracker: hub.JobTracker(),
		}),
		// --- Wiki knowledge base (feature-flagged, late-bound) ---
		handlerwiki.Methods(handlerwiki.Deps{
			Store: hub.WikiStore(),
		}),

		// --- Native model picker (miniapp.models.*) ---
		// Late on purpose: the Controller snapshots the registry and chat
		// handler at construction, and both exist only after
		// registerSessionRPCMethods. Registered early (#3457) the snapshot
		// stayed nil forever — the native picker showed every role as 미설정
		// and models.set was rejected as "not ready".
		modelpicker.NewController(modelpicker.ControllerConfig{
			Registry:    w.ModelRegistry,
			ChatHandler: w.ChatHandler,
			Logger:      w.Logger,
			RoleHealthVerdicts: func() map[string]string {
				if w.RoleHealth == nil {
					return nil
				}
				return w.RoleHealth.Verdicts()
			},
			RefreshCodingModelConsumers: w.RefreshCodingModelConsumers,
			ProviderConfigs: func() map[string]chatport.ProviderConfig {
				return configresolve.LoadProviderConfigs(w.Logger)
			},
		}).Methods(),

		// --- Skill genesis (depends on chatHandler for LLM client) ---
		handlerskill.GenesisMethods(handlerskill.GenesisDeps{
			Genesis:     w.Genesis.Svc,
			Evolver:     w.Genesis.Evolver,
			Tracker:     w.Genesis.Tracker,
			Transcripts: w.Genesis.Transcripts,
		}),

		// --- Mini App email analysis (miniapp.gmail.analyze) ---
		// Late-bound because the analyzer needs a configured LLM client
		// from the model registry, which is wired during memory subsystem
		// init right before this phase. Lazy factory still — operator
		// runs without any provider configured, the call returns
		// UNAVAILABLE rather than crashing the gateway.
		serverwire.WithMailAliases(handlermail.GmailAnalyzeMethods(handlermail.GmailAnalyzeDeps{
			// Archive-first client — the same factory the native mail list/detail
			// surface uses. Mail now arrives via LMTP and lives in the on-box
			// archive keyed by RFC822 Message-ID. The old gmail.DefaultClient()
			// fetched by Gmail-API message id, so "🔄 다시 분석" on an archived mail
			// handed an archive id (…@amazonses.com) to the Gmail API → HTTP 400
			// "Invalid id value". The miniapp mail surface is now native-archive-only
			// (the Gmail fallback was removed — see server_mail_repository.go).
			Client: w.Mail.ClientFactory(w.DenebDir),
			Pipeline: func() (handlermail.AnalyzePipeline, error) {
				// Role selection is shared with the autonomous poller via
				// mailAnalysisModels (stage-2 = main role, stage-1 = tiny
				// role) so the two mail-analysis paths cannot drift apart.
				// This replaces a #1816-era pin to the fallback role
				// ("step3.7 streams unstoppable thinking") that the poller
				// has since disproven — the pipeline disables thinking and
				// scrubs reasoning leaks — and that broke the interactive
				// button alone when the fallback provider's key died (401,
				// 2026-06-10).
				llmClient, model, localClient, localModel := w.Mail.Models()
				if llmClient == nil {
					return nil, handlermail.ErrAnalyzeNoLLM
				}
				gmailClient, err := gmail.DefaultClient()
				if err != nil {
					return nil, err
				}
				return handlermail.PipelineFromMailAnalysis(gmailClient, llmClient, localClient, model, localModel, w.Mail.Prompt(), w.Mail.ProjectsFn(), w.Mail.SenderFacts, document.ExtractAttachmentText, func(domain string) []string {
					if w.Mail.CpProjects == nil {
						return nil
					}
					return w.Mail.CpProjects.Lookup(w.WikiStore, domain)
				})
			},
			Cache:      handlermail.NewAnalysisStore(filepath.Join(w.DenebDir, "cache", "mail_analysis")),
			WorkState:  mailwork.New(filepath.Join(w.DenebDir, "mail_work_state.json")),
			SaveToWiki: w.Mail.WikiSink,
			WikiStore: func() (miniknowledge.MemorySearcher, error) {
				store := hub.WikiStore()
				if store == nil {
					return nil, serverwire.ErrWikiDisabled
				}
				return store, nil
			},
			Ask: w.Mail.Ask(),
		})),
	}

	for _, d := range domains {
		if d != nil {
			w.Dispatcher.RegisterDomain(d)
		}
	}

	// Wire agent runner and subagent poller to cron service. Cron output is
	// delivered to the native client via the main-session handoff wired in
	// registerSessionRPCMethods (proactive relay), not Telegram.
	if w.CronService != nil {
		// Pre-collect wiki weekly-report data for "/weekly" cron payloads so the
		// LLM writes inside a fixed 양식 (cronChatAdapter.resolveCronCommand), and
		// render the formal form image to post to the 업무 chat alongside the text.
		var weeklyDataFn func(ctx context.Context) (string, error)
		var weeklyFormFn func(ctx context.Context) error
		var weeklyTextFn func(ctx context.Context) (string, error)
		if w.WikiStore != nil {
			wikiDir := w.WikiStore.Dir()
			weeklyDataFn = func(ctx context.Context) (string, error) {
				return routine.CollectWeeklyReportData(ctx, routine.WeeklyReportOpts{WikiDir: wikiDir}, time.Now())
			}
			// Deterministic report body — a head line + server-assembled deneb-ui
			// card (RenderWeeklyReportCard), preferred over the LLM turn so the
			// format is identical every run and no model ever touches a figure.
			// The plain-text 양식 (RenderWeeklyReportText) remains the PDF path's
			// fallback composition.
			weeklyTextFn = func(_ context.Context) (string, error) {
				return routine.RenderWeeklyReportCard(routine.WeeklyReportOpts{WikiDir: wikiDir}, time.Now()), nil
			}
			weeklyFormFn = func(ctx context.Context) error {
				img, ok := routine.BuildWeeklyReportImage(ctx, routine.WeeklyReportOpts{WikiDir: wikiDir}, time.Now())
				if !ok {
					return nil // render unavailable (low memory/disk) → text report only
				}
				_, err := w.ProactiveRelay.DeliverNativeImage("📋 주간업무보고 — 정식 양식", img)
				return err
			}
		}
		w.CronService.SetAgentRunner(cronrunner.New(cronrunner.Config{
			Chat:              w.ChatHandler,
			Logger:            w.Logger,
			WeeklyReportData:  weeklyDataFn,
			WeeklyReportText:  weeklyTextFn,
			WeeklyFormDeliver: weeklyFormFn,
		}))
		// Interactive /weekly (/주간보고) reuses the same deterministic generators
		// so a manually typed command matches the Saturday cron output (this path
		// was cron-only before — typed input fell through to the LLM).
		if w.ChatHandler != nil {
			w.ChatHandler.SetWeeklyReport(weeklyTextFn, weeklyFormFn)
		}
		if w.ACPDeps != nil {
			w.CronService.SetSubagentPoller(cronrunner.NewSubagentPoller(
				w.ACPDeps.Registry,
				w.Sessions,
			))
		}
	}
}
