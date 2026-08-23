package server

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailwork"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/configresolve"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/cronrunner"
	runtimemeeting "github.com/choiceoh/deneb/gateway-go/internal/runtime/meeting"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/modelpicker"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/phoneevents"
	handlerchat "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/chat"
	handlerchatminiapp "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/chat/miniapp"
	miniknowledge "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/knowledge"
	handlermail "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/mail"
	handlersession "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/session"
	handlerskill "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/skill"
	handlerwiki "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/toolbind"
)

// registerLateMethods registers RPC domains that depend on chatHandler.
// Called after registerSessionRPCMethods() which creates the chat handler.
func (s *Server) registerLateMethods(hub *rpcutil.GatewayHub) {
	hub.AdvancePhase(rpcutil.PhaseLate)
	hub.Opt.WikiStore = s.wikiStore // late-bound: created during session phase

	domains := []map[string]rpcutil.HandlerFunc{
		handlerchat.Methods(handlerchat.Deps{
			Chat:        s.chatHandler,
			Broadcaster: hub.Broadcast,
		}),
		handlerchat.BtwMethods(handlerchat.BtwDeps{
			Chat:        s.chatHandler,
			Broadcaster: hub.Broadcast,
		}),
		// Native-client chat bridge (miniapp.chat.send/history): lets the
		// standalone app drive a turn over the miniapp.* RPC surface via
		// SendSync, with deneb-ui emission enabled (channel "client").
		handlerchatminiapp.Methods(handlerchatminiapp.Deps{
			Chat:     s.chatHandler,
			OcrImage: toolbind.OCRImage,
			// Image understanding: the vision-capable model chain (main model →
			// dedicated vision model) describes the image, falling back to OcrImage
			// glyph extraction — richer than OCR-only for charts/diagrams/photos.
			DescribeImage: func(ctx context.Context, img []byte, mime string) string {
				return chat.DescribeCapturedImage(ctx, img, mime, toolbind.OCRImage)
			},
			Transcribe: toolbind.TranscribeAudio,
			// Computer-use round trip: the desktop's execution report resumes
			// the waiting computer tool dispatch (server_computer.go).
			ResolveComputerResult: s.resolveComputerResult,
			// Document attach (pdf/doc/sheet) → in-house extractor (PDF/Excel/Word/
			// PowerPoint/CSV/text, with a scanned-PDF / image OCR fallback).
			ExtractDocument: toolbind.ExtractAttachmentText,
			// Oversized captures are digested by the local lightweight model
			// (or visibly head-truncated) after raw persistence.
			DigestOversized: chat.DigestOversizedDocumentText,
			// Batch attachment previews: a compact tiny-model summary (~1000자)
			// instead of a raw front-of-text cut, so the pointer turn's preview is
			// representative (falls back to the front cut when local AI is down).
			SummarizePreview: chat.SummarizeAttachmentPreview,
			// In-app browser in-place translation (en/ru → ko) — DeepL-only.
			Translate: toolbind.TranslateSegments,
			// Raw capture persistence: full OCR text / diarized transcript →
			// {memory}/captures/ + diary breadcrumb (recallable, dream-distilled,
			// backed up). The agent turn only summarizes; this keeps the original.
			SaveCapture: func(kind, context, text string) (string, string, int, error) {
				ws := hub.Opt.WikiStore
				if ws == nil {
					return "", "", 0, fmt.Errorf("wiki store unavailable")
				}
				return ws.SaveCaptureAt(kind, context, text)
			},
			// Proper-noun bias for audio transcription, merged from two sources:
			// the wiki (people/companies/deals/domain terms) and the contacts
			// address book (every saved name + org). Either may be empty.
			Hotwords: func() string {
				var parts []string
				if ws := hub.Opt.WikiStore; ws != nil {
					if h := ws.HotwordHints(150); h != "" {
						parts = append(parts, h)
					}
				}
				if cs := hub.Opt.ContactsStore; cs != nil {
					if h := cs.HotwordHints(100); h != "" {
						parts = append(parts, h)
					}
				}
				if h := runtimemeeting.LoadPlaudGlossaryHotwords(configresolve.TopicsDir(), 80); h != "" {
					parts = append(parts, h)
				}
				return strings.Join(parts, ", ")
			},
			// Primary contacts sync: persist the whole address book into the
			// contacts store (phone lookup / name search / ASR hotwords).
			SaveContacts: func(contactsJSON []byte) (int, error) {
				cs := hub.Opt.ContactsStore
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
				ws := hub.Opt.WikiStore
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
						s.logger.Info("employee pages created", "count", len(cr.Created))
					}
					// Also give a page to important EXTERNAL people — the counterparties our
					// 프로젝트/거래 pages actually name. Everyone else stays mention-curated.
					if dr, derr := ws.EnrichDealMentionedPages(p.Contacts, ourDomains); derr == nil && len(dr.Created) > 0 {
						s.logger.Info("deal-mentioned pages created", "count", len(dr.Created))
					}
					if er, eerr := ws.EnrichPersonEmails(p.Contacts); eerr == nil && len(er.Ambiguous) > 0 {
						s.logger.Info("person email backfill", "seeded", len(er.Updated), "homonyms_flagged", len(er.Ambiguous))
					}
				}
				return res, nil
			},
			WorkFeed: s.nativeWorkFeedStore(),
			// Upgrade a shared document's feed card to a proper doc_analysis
			// deliverable. Wrapped so s.proactiveRelay (built after the chat handler)
			// is resolved at call time.
			PublishDeliverable: func(text string) (bool, error) {
				return s.proactiveRelay.PublishDeliverable(text)
			},
			IngestEvent: phoneevents.New(s.phoneEventHandlerConfig()).IngestAsync,
			SteerNative: func(sessionKey, note string) bool {
				if s.chatHandler == nil {
					return false
				}
				return s.chatHandler.SteerNative(sessionKey, note)
			},
		}),
		handlersession.ExecMethods(handlersession.ExecDeps{
			Chat:       s.chatHandler,
			JobTracker: hub.JobTracker(),
		}),
		// --- Wiki knowledge base (feature-flagged, late-bound) ---
		handlerwiki.Methods(handlerwiki.Deps{
			Store: hub.Opt.WikiStore,
		}),

		// --- Native model picker (miniapp.models.*) ---
		// Late on purpose: the Controller snapshots the registry and chat
		// handler at construction, and both exist only after
		// registerSessionRPCMethods. Registered early (#3457) the snapshot
		// stayed nil forever — the native picker showed every role as 미설정
		// and models.set was rejected as "not ready".
		modelpicker.NewController(modelpicker.ControllerConfig{
			Registry:    s.modelRegistry,
			ChatHandler: s.chatHandler,
			Logger:      s.logger,
			RoleHealthVerdicts: func() map[string]string {
				if s.roleHealth == nil {
					return nil
				}
				return s.roleHealth.Verdicts()
			},
			RefreshCodingModelConsumers: s.refreshCodingModelConsumers,
			ProviderConfigs: func() map[string]chatport.ProviderConfig {
				return configresolve.LoadProviderConfigs(s.logger)
			},
			// 24h per-model usage for the picker rows (nil-safe on the writer).
			UsageStats: s.agentLogWriter.AggregateByModel,
		}).Methods(),

		// --- Skill genesis (depends on chatHandler for LLM client) ---
		handlerskill.GenesisMethods(handlerskill.GenesisDeps{
			Genesis:     s.genesisSvc,
			Evolver:     s.genesisEvolver,
			Tracker:     s.genesisTracker,
			Transcripts: s.genesisTranscripts,
		}),

		// --- Mini App email analysis (miniapp.mail.analyze) ---
		// Late-bound because the analyzer needs a configured LLM client
		// from the model registry, which is wired during memory subsystem
		// init right before this phase. Lazy factory still — operator
		// runs without any provider configured, the call returns
		// UNAVAILABLE rather than crashing the gateway.
		handlermail.GmailAnalyzeMethods(handlermail.GmailAnalyzeDeps{
			// Archive-first client — the same factory the native mail list/detail
			// surface uses. Mail now arrives via LMTP and lives in the on-box
			// archive keyed by RFC822 Message-ID. The old gmail.DefaultClient()
			// fetched by Gmail-API message id, so "🔄 다시 분석" on an archived mail
			// handed an archive id (…@amazonses.com) to the Gmail API → HTTP 400
			// "Invalid id value". The miniapp mail surface is now native-archive-only
			// (the Gmail fallback was removed — see server_mail_repository.go).
			Client: s.miniappMailClientFactory(s.denebDir),
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
				llmClient, model, localClient, localModel := s.mailAnalysisModels()
				if llmClient == nil {
					return nil, handlermail.ErrAnalyzeNoLLM
				}
				gmailClient, err := gmail.DefaultClient()
				if err != nil {
					return nil, err
				}
				return handlermail.PipelineFromMailAnalysis(gmailClient, llmClient, localClient, model, localModel, s.mailAnalysisPrompt(), s.projectCandidatesFn(), s.wikiSenderFacts, toolbind.ExtractAttachmentText, func(domain string) []string {
					return s.cpProjects.Lookup(s.wikiStore, domain)
				})
			},
			Cache:      handlermail.NewAnalysisStore(filepath.Join(s.denebDir, "cache", "mail_analysis")),
			WorkState:  mailwork.New(filepath.Join(s.denebDir, "mail_work_state.json")),
			SaveToWiki: makeMailAnalysisWikiSink(hub),
			WikiStore: func() (miniknowledge.MemorySearcher, error) {
				store := hub.Opt.WikiStore
				if store == nil {
					return nil, errWikiDisabled
				}
				return store, nil
			},
			Ask: s.makeMailQAAsk(),
		}),
	}

	for _, d := range domains {
		if d != nil {
			s.dispatcher.RegisterDomain(d)
		}
	}

	// Wire agent runner and subagent poller to cron service. Cron output is
	// delivered to the native client via the main-session handoff wired in
	// registerSessionRPCMethods (proactive relay), not Telegram.
	if s.cronService != nil {
		var morningDataFn func(ctx context.Context) (string, error)
		var morningRenderFn func(dataJSON, narrativeJSON string) (string, error)
		var morningOpts toolbind.MorningLetterOpts
		if s.wikiStore != nil {
			morningOpts = toolbind.MorningLetterOpts{
				DiaryDir: s.wikiStore.DiaryDir(),
				WikiDir:  s.wikiStore.Dir(),
			}
		}
		morningDataFn = func(ctx context.Context) (string, error) {
			return toolbind.CollectMorningLetterData(ctx, morningOpts, time.Now())
		}
		morningRenderFn = func(dataJSON, narrativeJSON string) (string, error) {
			return toolbind.RenderMorningLetterCard(dataJSON, narrativeJSON, time.Now())
		}
		// Pre-collect wiki weekly-report data for "/weekly" cron payloads so the
		// LLM writes inside a fixed 양식 (cronChatAdapter.resolveCronCommand), and
		// render the formal form image to post to the 업무 chat alongside the text.
		var weeklyDataFn func(ctx context.Context) (string, error)
		var weeklyFormFn func(ctx context.Context) error
		var weeklyTextFn func(ctx context.Context) (string, error)
		if s.wikiStore != nil {
			wikiDir := s.wikiStore.Dir()
			weeklyDataFn = func(ctx context.Context) (string, error) {
				return toolbind.CollectWeeklyReportData(ctx, toolbind.WeeklyReportOpts{WikiDir: wikiDir}, time.Now())
			}
			// Deterministic report body — a head line + server-assembled deneb-ui
			// card (RenderWeeklyReportCard), preferred over the LLM turn so the
			// format is identical every run and no model ever touches a figure.
			// The plain-text 양식 (RenderWeeklyReportText) remains the PDF path's
			// fallback composition.
			weeklyTextFn = func(_ context.Context) (string, error) {
				return toolbind.RenderWeeklyReportCard(toolbind.WeeklyReportOpts{WikiDir: wikiDir}, time.Now()), nil
			}
			weeklyFormFn = func(ctx context.Context) error {
				img, ok := toolbind.BuildWeeklyReportImage(ctx, toolbind.WeeklyReportOpts{WikiDir: wikiDir}, time.Now())
				if !ok {
					return nil // render unavailable (low memory/disk) → text report only
				}
				_, err := s.proactiveRelay.DeliverNativeImage("📋 주간업무보고 — 정식 양식", img)
				return err
			}
		}
		s.cronService.SetAgentRunner(cronrunner.New(cronrunner.Config{
			Chat:                s.chatHandler,
			Logger:              s.logger,
			MorningLetterData:   morningDataFn,
			MorningLetterRender: morningRenderFn,
			WeeklyReportData:    weeklyDataFn,
			WeeklyReportText:    weeklyTextFn,
			WeeklyFormDeliver:   weeklyFormFn,
		}))
		// Interactive /weekly (/주간보고) reuses the same deterministic generators
		// so a manually typed command matches the Saturday cron output (this path
		// was cron-only before — typed input fell through to the LLM).
		if s.chatHandler != nil {
			s.chatHandler.SetWeeklyReport(weeklyTextFn, weeklyFormFn)
		}
		if s.acpDeps != nil {
			s.cronService.SetSubagentPoller(cronrunner.NewSubagentPoller(
				s.acpDeps.Registry,
				s.sessions,
			))
		}
	}
}
