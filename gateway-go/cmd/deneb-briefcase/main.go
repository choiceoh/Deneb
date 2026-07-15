// Command deneb-briefcase validates, runs, and deterministically grades
// isolated Deneb-Briefcase casepacks.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/tokenest"
	casepack "github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase"
	briefcasefb "github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase/feedback"
	evalbriefcase "github.com/choiceoh/deneb/gateway-go/internal/eval/briefcase"
	closedloop "github.com/choiceoh/deneb/gateway-go/internal/eval/briefcase/closedloop"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	runtimebriefcase "github.com/choiceoh/deneb/gateway-go/internal/runtime/briefcase"
)

const maxJSONInput = 16 << 20

const (
	graderPlanSourceRef        = "briefcase:grader-plan"
	supervisorPlanSourceRef    = "briefcase:supervisor-plan"
	userSimulatorPlanSourceRef = "briefcase:user-simulator-plan"
	devicePlanSourceRef        = "briefcase:device-plan"
)

type partialRunOutput struct {
	SchemaVersion string                      `json:"schemaVersion"`
	Complete      bool                        `json:"complete"`
	Error         string                      `json:"error"`
	Run           *runtimebriefcase.RunResult `json:"run"`
}

type partialLoopOutput struct {
	SchemaVersion string             `json:"schemaVersion"`
	Complete      bool               `json:"complete"`
	Error         string             `json:"error"`
	Result        *closedloop.Result `json:"result"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "deneb-briefcase:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stderr)
		return errors.New("a command is required")
	}
	switch args[0] {
	case "validate":
		return runValidate(args[1:], stdout, stderr)
	case "manifest-digest":
		return runManifestDigest(args[1:], stdout, stderr)
	case "supervisor-plan-digest":
		return runSupervisorPlanDigest(args[1:], stdout, stderr)
	case "doctor":
		return runDoctor(args[1:], stdout, stderr)
	case "run":
		return runCase(args[1:], stdout, stderr)
	case "loop":
		return runClosedLoop(args[1:], stdout, stderr)
	case "score":
		return runScore(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printUsage(stdout)
		return nil
	default:
		printUsage(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: deneb-briefcase <validate|manifest-digest|supervisor-plan-digest|doctor|run|loop|score> [flags]")
}

func runValidate(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	caseDir := fs.String("case", "", "casepack directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pack, err := casepack.LoadDir(*caseDir)
	if err != nil {
		return err
	}
	return writeJSON(stdout, "", struct {
		Status   string `json:"status"`
		CaseID   string `json:"caseId"`
		Digest   string `json:"digest"`
		Sources  int    `json:"sources"`
		Episodes int    `json:"episodes"`
	}{Status: "VALID", CaseID: pack.Manifest.CaseID, Digest: pack.Digest, Sources: len(pack.Manifest.Sources), Episodes: len(pack.Manifest.Episodes)})
}

func runManifestDigest(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("manifest-digest", flag.ContinueOnError)
	fs.SetOutput(stderr)
	manifestPath := fs.String("manifest", "", "manifest.json path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var manifest casepack.Manifest
	if err := decodeJSONFile(*manifestPath, &manifest); err != nil {
		return err
	}
	digest, err := casepack.CanonicalDigest(manifest)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, digest)
	return nil
}

func runSupervisorPlanDigest(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("supervisor-plan-digest", flag.ContinueOnError)
	fs.SetOutput(stderr)
	planPath := fs.String("plan", "", "supervisor plan JSON path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var plan evalbriefcase.SupervisorPlan
	if err := decodeJSONFile(*planPath, &plan); err != nil {
		return err
	}
	digest, err := evalbriefcase.SupervisorPlanDigest(plan)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, digest)
	return nil
}

func runDoctor(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	parent := fs.String("parent", "", "optional parent for disposable run root")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := runtimebriefcase.NewRunRoot(*parent)
	if err != nil {
		return err
	}
	defer root.Close()
	policy, err := runtimebriefcase.NewPolicy(root, runtimebriefcase.PolicyOptions{})
	if err != nil {
		return err
	}
	if !errors.Is(policy.CheckNetwork(), runtimebriefcase.ErrPolicyDenied) || !errors.Is(policy.CheckExec(), runtimebriefcase.ErrPolicyDenied) {
		return errors.New("runtime policy did not deny network and exec")
	}
	env, err := root.IsolatedEnviron(os.Environ())
	if err != nil {
		return err
	}
	return writeJSON(stdout, "", struct {
		Status       string `json:"status"`
		Network      string `json:"network"`
		Exec         string `json:"exec"`
		Environment  int    `json:"isolatedEnvironmentVariables"`
		CleanupArmed bool   `json:"cleanupArmed"`
	}{Status: "READY", Network: "DENIED", Exec: "DENIED", Environment: len(env), CleanupArmed: true})
}

func runCase(args []string, stdout, stderr io.Writer) (returnErr error) {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	caseDir := fs.String("case", "", "casepack directory")
	baseURL := fs.String("base-url", os.Getenv("DENEB_BRIEFCASE_MODEL_BASE_URL"), "OpenAI/Anthropic compatible model base URL")
	model := fs.String("model", os.Getenv("DENEB_BRIEFCASE_MODEL"), "model ID")
	apiKeyEnv := fs.String("api-key-env", "DENEB_BRIEFCASE_MODEL_API_KEY", "environment variable containing the model API key")
	apiMode := fs.String("api-mode", llm.APIModeOpenAI, "openai or anthropic")
	allowRemote := fs.Bool("allow-remote-model", false, "allow Portable case data to reach a non-loopback model endpoint")
	keepRoot := fs.Bool("keep-run-root", false, "retain plaintext run root for local debugging")
	output := fs.String("output", "", "write run JSON to this file instead of stdout")
	artifactDir := fs.String("artifact-dir", "", "durable export directory for signed artifacts (default: <output>.artifacts or a temporary export)")
	devicePlan := fs.String("device-plan", "", "signed Device Twin plan JSON (auto-loaded from the casepack when declared)")
	arm := fs.String("arm", string(runtimebriefcase.ArmMemoryAssisted), "benchmark arm: memory-assisted or raw-primary")
	skipRecall := fs.Bool("skip-recall", false, "disable durable-memory recall preflight (raw-primary does this automatically)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pack, err := casepack.LoadDir(*caseDir)
	if err != nil {
		return err
	}
	if err := authorizeModelEndpoint(*baseURL, pack.Manifest.PrivacyMode, *allowRemote); err != nil {
		return err
	}
	if strings.TrimSpace(*model) == "" {
		return errors.New("--model is required")
	}
	mode := strings.ToLower(strings.TrimSpace(*apiMode))
	if mode != llm.APIModeOpenAI && mode != llm.APIModeAnthropic {
		return fmt.Errorf("--api-mode must be %q or %q", llm.APIModeOpenAI, llm.APIModeAnthropic)
	}
	key := ""
	if name := strings.TrimSpace(*apiKeyEnv); name != "" {
		key = os.Getenv(name)
	}
	client := llm.NewClient(*baseURL, key, llm.WithAPIMode(mode))
	devicePlanSource, devicePlanDigest, err := loadDevicePlanSource(pack, *devicePlan)
	if err != nil {
		return err
	}
	runRoot, err := runtimebriefcase.NewRunRoot("")
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			if err := runRoot.Close(); err != nil {
				returnErr = errors.Join(returnErr, fmt.Errorf("clean plaintext run root: %w", err))
			}
		}
	}()
	harness, err := runtimebriefcase.NewChatHarness(runtimebriefcase.ChatHarnessConfig{
		Pack: pack, Root: runRoot, Client: client, Model: *model,
		DevicePlanSource: devicePlanSource, DevicePlanSourceSHA256: devicePlanDigest,
		SkipRecall: *skipRecall,
		Arm:        runtimebriefcase.Arm(strings.TrimSpace(*arm)),
			TokenEstimate: tokenest.EstimateUncalibrated,
	})
	if err != nil {
		return err
	}
	defer func() {
		if err := harness.Close(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close Briefcase harness: %w", err))
		}
	}()
	if *keepRoot {
		paths, pathErr := runRoot.Paths()
		if pathErr != nil {
			return pathErr
		}
		cleanup = false
		fmt.Fprintln(stderr, "retained run root:", paths.Root)
	}
	result, runErr := harness.Run(context.Background())
	if result != nil {
		exported, exportErr := exportDirectArtifacts(context.Background(), pack, result, *output, *artifactDir)
		if exportErr != nil {
			combined := errors.Join(runErr, exportErr)
			if retainErr := retainFailedRunRoot(runRoot, &cleanup, stderr); retainErr != nil {
				combined = errors.Join(combined, retainErr)
			}
			if writeErr := writeJSON(stdout, *output, partialRunOutput{
				SchemaVersion: "deneb-briefcase-partial/v1", Complete: false, Error: combined.Error(), Run: result,
			}); writeErr != nil {
				combined = errors.Join(combined, writeErr)
			}
			return combined
		}
		result = exported
		makeRunArtifactRootPortable(*output, result)
	}
	if runErr != nil {
		if result != nil {
			if err := writeJSON(stdout, *output, partialRunOutput{
				SchemaVersion: "deneb-briefcase-partial/v1", Complete: false, Error: runErr.Error(), Run: result,
			}); err != nil {
				return errors.Join(runErr, fmt.Errorf("write partial run result: %w", err))
			}
		}
		return runErr
	}
	if err := writeJSON(stdout, *output, result); err != nil {
		return err
	}
	return nil
}

type closedLoopOptions struct {
	caseDir            string
	baseURL            string
	model              string
	apiKeyEnv          string
	apiMode            string
	allowRemote        bool
	keepRoot           bool
	output             string
	artifactDir        string
	supervisorPlanPath string
	userPlanPath       string
	devicePlanPath     string
	arm                string
	skipRecall         bool
}

func parseClosedLoopOptions(args []string, stderr io.Writer) (closedLoopOptions, error) {
	var options closedLoopOptions
	fs := flag.NewFlagSet("loop", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&options.caseDir, "case", "", "casepack directory")
	fs.StringVar(&options.baseURL, "base-url", os.Getenv("DENEB_BRIEFCASE_MODEL_BASE_URL"), "OpenAI/Anthropic compatible executor model base URL")
	fs.StringVar(&options.model, "model", os.Getenv("DENEB_BRIEFCASE_MODEL"), "executor model ID")
	fs.StringVar(&options.apiKeyEnv, "api-key-env", "DENEB_BRIEFCASE_MODEL_API_KEY", "environment variable containing the executor model API key")
	fs.StringVar(&options.apiMode, "api-mode", llm.APIModeOpenAI, "openai or anthropic")
	fs.BoolVar(&options.allowRemote, "allow-remote-model", false, "allow Portable case data to reach a non-loopback model endpoint")
	fs.BoolVar(&options.keepRoot, "keep-run-root", false, "retain plaintext run root for local debugging")
	fs.StringVar(&options.output, "output", "", "write closed-loop JSON to this file instead of stdout")
	fs.StringVar(&options.artifactDir, "artifact-dir", "", "durable export directory for current/best signed artifacts")
	fs.StringVar(&options.supervisorPlanPath, "supervisor-plan", "", "sealed checkpoint supervisor plan JSON")
	fs.StringVar(&options.userPlanPath, "user-plan", "", "sealed scripted user-simulator plan JSON")
	fs.StringVar(&options.devicePlanPath, "device-plan", "", "signed Device Twin plan JSON (auto-loaded from the casepack when declared)")
	fs.StringVar(&options.arm, "arm", string(runtimebriefcase.ArmMemoryAssisted), "benchmark arm: memory-assisted or raw-primary")
	fs.BoolVar(&options.skipRecall, "skip-recall", false, "disable durable-memory recall preflight (raw-primary does this automatically)")
	if err := fs.Parse(args); err != nil {
		return closedLoopOptions{}, err
	}
	return options, nil
}

func validateClosedLoopModel(pack *casepack.Pack, options closedLoopOptions) (string, error) {
	if err := authorizeModelEndpoint(options.baseURL, pack.Manifest.PrivacyMode, options.allowRemote); err != nil {
		return "", err
	}
	if strings.TrimSpace(options.model) == "" {
		return "", errors.New("--model is required")
	}
	mode := strings.ToLower(strings.TrimSpace(options.apiMode))
	if mode != llm.APIModeOpenAI && mode != llm.APIModeAnthropic {
		return "", fmt.Errorf("--api-mode must be %q or %q", llm.APIModeOpenAI, llm.APIModeAnthropic)
	}
	return mode, nil
}

type closedLoopPlans struct {
	supervisorData         []byte
	supervisorSourceDigest string
	userData               []byte
	userSourceDigest       string
}

func loadClosedLoopPlans(pack *casepack.Pack, options closedLoopOptions) (closedLoopPlans, error) {
	supervisorData, supervisorSourceDigest, err := readSealedSource(pack, options.supervisorPlanPath, supervisorPlanSourceRef)
	if err != nil {
		return closedLoopPlans{}, fmt.Errorf("supervisor plan: %w", err)
	}
	var supervisorPlan evalbriefcase.SupervisorPlan
	if err := decodeJSONBytes(supervisorData, &supervisorPlan); err != nil {
		return closedLoopPlans{}, fmt.Errorf("supervisor plan: %w", err)
	}

	userData, userSourceDigest, userPlan, err := loadClosedLoopUserPlan(pack, options.userPlanPath)
	if err != nil {
		return closedLoopPlans{}, err
	}
	if pack.Manifest.RunPolicy.MaxFollowUps > 0 {
		if err := validateClosedLoopFeedback(pack, supervisorPlan, userPlan, userSourceDigest, supervisorSourceDigest); err != nil {
			return closedLoopPlans{}, err
		}
	}
	return closedLoopPlans{
		supervisorData:         supervisorData,
		supervisorSourceDigest: supervisorSourceDigest,
		userData:               userData,
		userSourceDigest:       userSourceDigest,
	}, nil
}

func loadClosedLoopUserPlan(pack *casepack.Pack, path string) ([]byte, string, briefcasefb.UserSimulatorPlan, error) {
	if pack.Manifest.RunPolicy.MaxFollowUps == 0 {
		if strings.TrimSpace(path) != "" {
			return nil, "", briefcasefb.UserSimulatorPlan{}, errors.New("--user-plan is forbidden when the signed maxFollowUps is zero")
		}
		return nil, "", briefcasefb.UserSimulatorPlan{}, nil
	}
	data, digest, err := readSealedSource(pack, path, userSimulatorPlanSourceRef)
	if err != nil {
		return nil, "", briefcasefb.UserSimulatorPlan{}, fmt.Errorf("user simulator plan: %w", err)
	}
	var userPlan briefcasefb.UserSimulatorPlan
	if err := decodeJSONBytes(data, &userPlan); err != nil {
		return nil, "", briefcasefb.UserSimulatorPlan{}, fmt.Errorf("user simulator plan: %w", err)
	}
	if userPlan.CaseID != pack.Manifest.CaseID {
		return nil, "", briefcasefb.UserSimulatorPlan{}, errors.New("user simulator plan caseId does not match the signed case")
	}
	if _, err := briefcasefb.NewScriptedUserSimulator(userPlan, pack.Manifest.RunPolicy.MaxFollowUps); err != nil {
		return nil, "", briefcasefb.UserSimulatorPlan{}, err
	}
	return data, digest, userPlan, nil
}

func validateClosedLoopFeedback(pack *casepack.Pack, supervisorPlan evalbriefcase.SupervisorPlan, userPlan briefcasefb.UserSimulatorPlan, userSourceDigest, supervisorSourceDigest string) error {
	hidden, err := closedloop.HiddenFeedbackInputs(pack, supervisorPlan, userSourceDigest, supervisorSourceDigest)
	if err != nil {
		return fmt.Errorf("feedback firewall inputs: %w", err)
	}
	firewall, err := briefcasefb.NewFeedbackFirewall(hidden, briefcasefb.FeedbackLimits{})
	if err != nil {
		return fmt.Errorf("feedback firewall: %w", err)
	}
	// Reject privileged feedback before the executor spends a token. The
	// runtime repeats this check at the actual simulator boundary.
	for _, followUp := range userPlan.FollowUps {
		if _, err := firewall.SanitizeFollowUp(followUp.Message); err != nil {
			return fmt.Errorf("user simulator plan feedback: %w", err)
		}
	}
	return nil
}

func runClosedLoop(args []string, stdout, stderr io.Writer) (returnErr error) {
	options, err := parseClosedLoopOptions(args, stderr)
	if err != nil {
		return err
	}
	pack, err := casepack.LoadDir(options.caseDir)
	if err != nil {
		return err
	}
	mode, err := validateClosedLoopModel(pack, options)
	if err != nil {
		return err
	}
	plans, err := loadClosedLoopPlans(pack, options)
	if err != nil {
		return err
	}

	key := ""
	if name := strings.TrimSpace(options.apiKeyEnv); name != "" {
		key = os.Getenv(name)
	}
	client := llm.NewClient(options.baseURL, key, llm.WithAPIMode(mode))
	devicePlanSource, devicePlanDigest, err := loadDevicePlanSource(pack, options.devicePlanPath)
	if err != nil {
		return err
	}
	runRoot, err := runtimebriefcase.NewRunRoot("")
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			if err := runRoot.Close(); err != nil {
				returnErr = errors.Join(returnErr, fmt.Errorf("clean plaintext run root: %w", err))
			}
		}
	}()
	harness, err := runtimebriefcase.NewChatHarness(runtimebriefcase.ChatHarnessConfig{
		Pack: pack, Root: runRoot, Client: client, Model: options.model,
		DevicePlanSource: devicePlanSource, DevicePlanSourceSHA256: devicePlanDigest,
		SkipRecall: options.skipRecall,
		Arm:        runtimebriefcase.Arm(strings.TrimSpace(options.arm)),
			TokenEstimate: tokenest.EstimateUncalibrated,
	})
	if err != nil {
		return err
	}
	defer func() {
		if err := harness.Close(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close Briefcase harness: %w", err))
		}
	}()
	runner, err := closedloop.New(closedloop.Config{
		Pack: pack, Executor: harness,
		SupervisorPlanSource:          plans.supervisorData,
		SupervisorPlanSourceSHA256:    plans.supervisorSourceDigest,
		UserSimulatorPlanSource:       plans.userData,
		UserSimulatorPlanSourceSHA256: plans.userSourceDigest,
	})
	if err != nil {
		return err
	}
	if options.keepRoot {
		paths, pathErr := runRoot.Paths()
		if pathErr != nil {
			return pathErr
		}
		cleanup = false
		fmt.Fprintln(stderr, "retained run root:", paths.Root)
	}
	result, loopErr := runner.Run(context.Background())
	return finishClosedLoop(pack, runRoot, &cleanup, result, loopErr, options, stdout, stderr)
}

func finishClosedLoop(pack *casepack.Pack, runRoot *runtimebriefcase.RunRoot, cleanup *bool, result *closedloop.Result, loopErr error, options closedLoopOptions, stdout, stderr io.Writer) error {
	if result != nil {
		if err := exportLoopArtifacts(context.Background(), pack, result, options.output, options.artifactDir); err != nil {
			combined := errors.Join(loopErr, err)
			if retainErr := retainFailedRunRoot(runRoot, cleanup, stderr); retainErr != nil {
				combined = errors.Join(combined, retainErr)
			}
			if writeErr := writeJSON(stdout, options.output, partialLoopOutput{
				SchemaVersion: "deneb-briefcase-loop-partial/v1", Complete: false, Error: combined.Error(), Result: result,
			}); writeErr != nil {
				combined = errors.Join(combined, writeErr)
			}
			return combined
		}
		makeLoopArtifactRootsPortable(options.output, result)
		if err := writeJSON(stdout, options.output, result); err != nil {
			return errors.Join(loopErr, err)
		}
	}
	if loopErr != nil {
		return loopErr
	}
	if result == nil || result.Decision != evalbriefcase.SupervisorPass {
		if result == nil {
			return errors.New("closed-loop result is missing")
		}
		return fmt.Errorf("supervisor verdict is %s", result.Decision)
	}
	return nil
}

func exportDirectArtifacts(ctx context.Context, pack *casepack.Pack, result *runtimebriefcase.RunResult, output, explicit string) (*runtimebriefcase.RunResult, error) {
	destination, temporaryParent, err := artifactDestination(output, explicit)
	if err != nil {
		return nil, err
	}
	exported, exportErr := runtimebriefcase.ExportRunArtifacts(ctx, pack, result, destination)
	if exportErr != nil && temporaryParent != "" {
		exportErr = errors.Join(exportErr, os.RemoveAll(temporaryParent))
	}
	return exported, exportErr
}

func exportLoopArtifacts(ctx context.Context, pack *casepack.Pack, result *closedloop.Result, output, explicit string) error {
	if result == nil || (result.Run == nil && result.BestRun == nil) {
		return nil
	}
	base, temporaryParent, err := artifactDestination(output, explicit)
	if err != nil {
		return err
	}
	if _, statErr := os.Lstat(base); !errors.Is(statErr, os.ErrNotExist) {
		if temporaryParent != "" {
			_ = os.RemoveAll(temporaryParent)
		}
		if statErr == nil {
			return errors.New("closed-loop artifact export destination already exists")
		}
		return fmt.Errorf("inspect closed-loop artifact export destination: %w", statErr)
	}
	cleanupOnError := func(exportErr error) error {
		removeRoot := base
		if temporaryParent != "" {
			removeRoot = temporaryParent
		}
		return errors.Join(exportErr, os.RemoveAll(removeRoot))
	}
	var exportedRun, exportedBest *runtimebriefcase.RunResult
	if result.Run != nil {
		exported, exportErr := runtimebriefcase.ExportRunArtifacts(ctx, pack, result.Run, filepath.Join(base, "run"))
		if exportErr != nil {
			return cleanupOnError(exportErr)
		}
		exportedRun = exported
	}
	if result.BestRun != nil {
		exported, exportErr := runtimebriefcase.ExportRunArtifacts(ctx, pack, result.BestRun, filepath.Join(base, "best"))
		if exportErr != nil {
			return cleanupOnError(exportErr)
		}
		exportedBest = exported
	}
	if exportedRun != nil {
		result.Run = exportedRun
	}
	if exportedBest != nil {
		result.BestRun = exportedBest
	}
	return nil
}

func artifactDestination(output, explicit string) (destination, temporaryParent string, err error) {
	if value := strings.TrimSpace(explicit); value != "" {
		return value, "", nil
	}
	if value := strings.TrimSpace(output); value != "" {
		return value + ".artifacts", "", nil
	}
	parent, err := os.MkdirTemp("", "deneb-briefcase-artifacts-")
	if err != nil {
		return "", "", fmt.Errorf("create durable artifact export parent: %w", err)
	}
	return filepath.Join(parent, "artifacts"), parent, nil
}

func makeRunArtifactRootPortable(output string, result *runtimebriefcase.RunResult) {
	if result == nil {
		return
	}
	result.ArtifactRoot = portableArtifactRoot(output, result.ArtifactRoot)
}

func makeLoopArtifactRootsPortable(output string, result *closedloop.Result) {
	if result == nil {
		return
	}
	makeRunArtifactRootPortable(output, result.Run)
	makeRunArtifactRootPortable(output, result.BestRun)
}

func portableArtifactRoot(output, artifactRoot string) string {
	if strings.TrimSpace(output) == "" || strings.TrimSpace(artifactRoot) == "" {
		return artifactRoot
	}
	outputAbs, err := filepath.Abs(filepath.Clean(output))
	if err != nil {
		return artifactRoot
	}
	rootAbs, err := filepath.Abs(filepath.Clean(artifactRoot))
	if err != nil {
		return artifactRoot
	}
	relative, err := filepath.Rel(filepath.Dir(outputAbs), rootAbs)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return rootAbs
	}
	return filepath.ToSlash(relative)
}

func resolvePortableArtifactRoot(runPath string, result *runtimebriefcase.RunResult) error {
	if result == nil || strings.TrimSpace(result.ArtifactRoot) == "" || filepath.IsAbs(result.ArtifactRoot) {
		return nil
	}
	root := filepath.Clean(filepath.FromSlash(result.ArtifactRoot))
	if root == "." || root == ".." || strings.HasPrefix(root, ".."+string(filepath.Separator)) || filepath.ToSlash(root) != result.ArtifactRoot {
		return errors.New("run result relative artifactRoot must be a canonical path below the result directory")
	}
	runAbs, err := filepath.Abs(filepath.Clean(runPath))
	if err != nil {
		return fmt.Errorf("resolve run result path: %w", err)
	}
	result.ArtifactRoot = filepath.Join(filepath.Dir(runAbs), root)
	return nil
}

func retainFailedRunRoot(root *runtimebriefcase.RunRoot, cleanup *bool, stderr io.Writer) error {
	if root == nil || cleanup == nil || !*cleanup {
		return nil
	}
	paths, err := root.Paths()
	if err != nil {
		return fmt.Errorf("retain failed run root: %w", err)
	}
	*cleanup = false
	fmt.Fprintln(stderr, "retained failed run root:", paths.Root)
	return nil
}

func runScore(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("score", flag.ContinueOnError)
	fs.SetOutput(stderr)
	caseDir := fs.String("case", "", "signed casepack directory")
	planPath := fs.String("plan", "", "sealed grader plan JSON")
	runPath := fs.String("run", "", "run result JSON")
	output := fs.String("output", "", "write grader report to this file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pack, err := casepack.LoadDir(*caseDir)
	if err != nil {
		return fmt.Errorf("casepack: %w", err)
	}
	planData, err := readSealedPlan(pack, *planPath)
	if err != nil {
		return err
	}
	var plan evalbriefcase.Plan
	if err := decodeJSONBytes(planData, &plan); err != nil {
		return fmt.Errorf("grader plan: %w", err)
	}
	if err := validateGraderArtifactBindings(pack, plan); err != nil {
		return err
	}
	var result runtimebriefcase.RunResult
	if err := decodeJSONFile(*runPath, &result); err != nil {
		return fmt.Errorf("run result: %w", err)
	}
	if err := verifyRunBinding(pack, plan.Fingerprint, result); err != nil {
		return err
	}
	if err := resolvePortableArtifactRoot(*runPath, &result); err != nil {
		return err
	}
	plan.Fingerprint = evalbriefcase.FingerprintFromRun(&result)
	report := evalbriefcase.Grade(plan, evalbriefcase.Evidence{
		Text: runtimebriefcase.LatestExecutorText(&result), ArtifactRoot: result.ArtifactRoot, State: result.State,
		ArtifactMaxBytes: graderArtifactLimits(pack),
	})
	if err := writeJSON(stdout, *output, report); err != nil {
		return err
	}
	if report.Status != evalbriefcase.StatusPass {
		return fmt.Errorf("grader verdict is %s", report.Status)
	}
	return nil
}

func graderArtifactLimits(pack *casepack.Pack) map[string]int64 {
	limits := make(map[string]int64, len(pack.Manifest.Artifacts))
	for _, artifact := range pack.Manifest.Artifacts {
		limit := artifact.MaxBytes
		if limit <= 0 {
			limit = casepack.MaxArtifactBytesV1
		}
		limits[artifact.Path] = limit
	}
	return limits
}

func validateGraderArtifactBindings(pack *casepack.Pack, plan evalbriefcase.Plan) error {
	declared := make(map[string]struct{}, len(pack.Manifest.Artifacts))
	for _, artifact := range pack.Manifest.Artifacts {
		declared[artifact.Path] = struct{}{}
	}
	for _, check := range plan.Checks {
		if check.Type != evalbriefcase.CheckArtifact {
			continue
		}
		if _, ok := declared[check.ArtifactPath]; !ok {
			return fmt.Errorf("grader artifact check %q references undeclared path %q", check.ID, check.ArtifactPath)
		}
	}
	return nil
}

func readSealedPlan(pack *casepack.Pack, path string) ([]byte, error) {
	data, _, err := readSealedSource(pack, path, graderPlanSourceRef)
	return data, err
}

func readSealedSource(pack *casepack.Pack, path, sourceRef string) ([]byte, string, error) {
	if pack == nil || strings.TrimSpace(path) == "" {
		return nil, "", errors.New("signed case and sealed source path are required")
	}
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, "", fmt.Errorf("sealed source path: %w", err)
	}
	roleCount := 0
	var selected *casepack.Source
	for i := range pack.Manifest.Sources {
		source := &pack.Manifest.Sources[i]
		if source.Access != casepack.SourceAccessSealed {
			continue
		}
		if source.SourceRef == sourceRef {
			roleCount++
		}
		candidate := filepath.Join(pack.Root, filepath.FromSlash(source.Path))
		if abs != candidate {
			continue
		}
		selected = source
	}
	if roleCount != 1 || selected == nil || selected.SourceRef != sourceRef {
		return nil, "", fmt.Errorf("path must name the unique signed %s sealed source", sourceRef)
	}
	data, err := pack.ReadFile(selected.Path)
	if err != nil {
		return nil, "", fmt.Errorf("sealed source: %w", err)
	}
	return data, selected.SHA256, nil
}

func verifyRunBinding(pack *casepack.Pack, fingerprint evalbriefcase.Fingerprint, result runtimebriefcase.RunResult) error {
	if pack == nil {
		return errors.New("briefcase: signed casepack is required for scoring")
	}
	if err := validateRunResult(result); err != nil {
		return err
	}
	if result.CaseID != pack.Manifest.CaseID || result.CasepackSHA256 != pack.Digest || result.Seed != pack.Manifest.Seed {
		return errors.New("run result does not match the signed casepack caseId, digest, and seed")
	}
	if err := validateCompletedRun(pack, result); err != nil {
		return err
	}
	if result.Arm != runtimebriefcase.ArmRawPrimary && result.Arm != runtimebriefcase.ArmMemoryAssisted {
		return fmt.Errorf("run result has unsupported arm %q", result.Arm)
	}
	return verifyFingerprintBinding(fingerprint, result)
}

// verifyFingerprintBinding checks every populated grader fingerprint field
// against the run; caseId is mandatory, the rest bind only when non-zero.
func verifyFingerprintBinding(fingerprint evalbriefcase.Fingerprint, result runtimebriefcase.RunResult) error {
	if fingerprint.CaseID != result.CaseID {
		return errors.New("grader fingerprint caseId does not match the run")
	}
	bindings := []struct {
		name      string
		got, want string
	}{
		{"runId", fingerprint.RunID, result.RunID},
		{"casepackSha256", fingerprint.CasepackSHA256, result.CasepackSHA256},
		{"model", fingerprint.Model, result.Model},
		{"providerModel", fingerprint.ProviderModel, result.ProviderModel},
		{"arm", fingerprint.Arm, string(result.Arm)},
		{"apiMode", fingerprint.APIMode, result.APIMode},
		{"recallMode", fingerprint.RecallMode, result.RecallMode},
		{"devicePlanSha256", fingerprint.DevicePlanSHA256, result.DevicePlanSHA256},
		{"devicePlanSourceSha256", fingerprint.DevicePlanSourceSHA256, result.DevicePlanSourceSHA256},
		{"toolSchemaSha256", fingerprint.ToolSchemaSHA256, result.ToolSchemaSHA256},
		{"endpointSha256", fingerprint.EndpointSHA256, result.EndpointSHA256},
		{"buildSha256", fingerprint.BuildSHA256, result.BuildSHA256},
		{"executionProfileSha256", fingerprint.ExecutionProfileSHA256, result.ExecutionProfileSHA256},
		{"systemPromptSequenceSha256", fingerprint.SystemPromptSequenceSHA256, result.SystemPromptSequenceSHA256},
	}
	for _, binding := range bindings {
		if binding.got != "" && binding.got != binding.want {
			return fmt.Errorf("grader fingerprint %s does not match the run", binding.name)
		}
	}
	if fingerprint.Seed != 0 && fingerprint.Seed != result.Seed {
		return errors.New("grader fingerprint seed does not match the run")
	}
	return nil
}

func validateCompletedRun(pack *casepack.Pack, result runtimebriefcase.RunResult) error {
	if err := validateDevicePlanBinding(pack, result); err != nil {
		return err
	}
	manifest := pack.Manifest
	if err := validateCompletedRunLength(len(manifest.Episodes), len(result.Episodes), manifest.RunPolicy.MaxFollowUps); err != nil {
		return err
	}
	budget := completedRunBudget{turns: manifest.RunPolicy.MaxTurns, outputTokens: manifest.RunPolicy.MaxTokens}
	budget, err := validateTimelineEpisodes(pack, result, indexRunSources(manifest.Sources), budget)
	if err != nil {
		return err
	}
	return validateFollowUpEpisodes(pack, result, budget)
}

type completedRunBudget struct {
	turns        int
	outputTokens int64
}

func validateCompletedRunLength(timelineEpisodes, runEpisodes, maxFollowUps int) error {
	if runEpisodes < timelineEpisodes {
		return errors.New("run result is a partial timeline and cannot be scored")
	}
	if runEpisodes-timelineEpisodes > maxFollowUps {
		return errors.New("run result exceeds the signed follow-up budget")
	}
	return nil
}

func indexRunSources(sources []casepack.Source) map[string]casepack.Source {
	indexed := make(map[string]casepack.Source, len(sources))
	for _, source := range sources {
		indexed[source.ID] = source
	}
	return indexed
}

func consumeCompletedRunBudget(budget completedRunBudget, label string, episode runtimebriefcase.EpisodeResult) (completedRunBudget, error) {
	if episode.Turns <= 0 || episode.OutputTokens < 0 || ((episode.Text != "" || episode.AllText != "") && episode.OutputTokens == 0) {
		return budget, fmt.Errorf("run result %s has invalid budget accounting", label)
	}
	if episode.Turns > budget.turns || int64(episode.OutputTokens) > budget.outputTokens {
		return budget, errors.New("run result exceeds the signed cumulative model budget")
	}
	budget.turns -= episode.Turns
	budget.outputTokens -= int64(episode.OutputTokens)
	return budget, nil
}

func validateTimelineEpisodes(
	pack *casepack.Pack,
	result runtimebriefcase.RunResult,
	sources map[string]casepack.Source,
	budget completedRunBudget,
) (completedRunBudget, error) {
	for index, expected := range pack.Manifest.Episodes {
		actual := result.Episodes[index]
		if err := validateTimelineHeader(index, expected, actual); err != nil {
			return budget, err
		}
		released, withheld := expectedReleaseOutcome(expected, sources, result.Arm)
		if !sameStrings(actual.ReleasedSource, released) || !sameStrings(actual.WithheldSource, withheld) {
			return budget, fmt.Errorf("run result episode %q release outcome does not match the signed arm", expected.ID)
		}
		if expected.Kind == casepack.EpisodeEvent {
			if eventExecutorOutput(actual) != (eventOutput{}) {
				return budget, fmt.Errorf("run result event %q contains executor output", expected.ID)
			}
			continue
		}
		var err error
		budget, err = consumeCompletedRunBudget(budget, fmt.Sprintf("episode %q", expected.ID), actual)
		if err != nil {
			return budget, err
		}
		normalized, err := readNormalizedEpisodeInput(pack, expected)
		if err != nil {
			return budget, err
		}
		if normalized == "" || actual.InputSHA256 != casepack.DigestBytes([]byte(normalized)) {
			return budget, fmt.Errorf("run result episode %q normalized input digest does not match", expected.ID)
		}
		if !validExecutorProvenance(actual, result) {
			return budget, fmt.Errorf("run result episode %q executor provenance does not match the run", expected.ID)
		}
	}
	return budget, nil
}

func validateTimelineHeader(index int, expected casepack.Episode, actual runtimebriefcase.EpisodeResult) error {
	if actual.EpisodeID != expected.ID || actual.Phase != "timeline" {
		return fmt.Errorf("run result timeline episode %d does not match signed episode %q", index+1, expected.ID)
	}
	if actual.At != expected.At.UTC().Format(time.RFC3339Nano) {
		return fmt.Errorf("run result episode %q timestamp does not match the signed timeline", expected.ID)
	}
	return nil
}

func expectedReleaseOutcome(
	expected casepack.Episode,
	sources map[string]casepack.Source,
	arm runtimebriefcase.Arm,
) (released, withheld []string) {
	released = make([]string, 0, len(expected.ReleaseSourceIDs))
	withheld = make([]string, 0, len(expected.ReleaseSourceIDs))
	for _, sourceID := range expected.ReleaseSourceIDs {
		if source := sources[sourceID]; arm == runtimebriefcase.ArmRawPrimary && source.Memory {
			withheld = append(withheld, sourceID)
		} else {
			released = append(released, sourceID)
		}
	}
	return released, withheld
}

type eventOutput struct {
	inputSHA256        string
	systemPromptSHA256 string
	model              string
	providerModel      string
	text               string
	allText            string
	stopReason         string
	inputTokens        int
	outputTokens       int
}

func eventExecutorOutput(episode runtimebriefcase.EpisodeResult) eventOutput {
	return eventOutput{
		inputSHA256:        episode.InputSHA256,
		systemPromptSHA256: episode.SystemPromptSHA256,
		model:              episode.Model,
		providerModel:      episode.ProviderModel,
		text:               episode.Text,
		allText:            episode.AllText,
		stopReason:         episode.StopReason,
		inputTokens:        episode.InputTokens,
		outputTokens:       episode.OutputTokens,
	}
}

func readNormalizedEpisodeInput(pack *casepack.Pack, expected casepack.Episode) (string, error) {
	if expected.Input == nil {
		return "", fmt.Errorf("signed executable episode %q has no input", expected.ID)
	}
	input, err := pack.ReadFile(expected.Input.Path)
	if err != nil {
		return "", fmt.Errorf("read signed episode %q input: %w", expected.ID, err)
	}
	return chat.SanitizeInput(string(input)), nil
}

type executorProvenance struct {
	model         string
	providerModel string
	stopReason    string
}

func validExecutorProvenance(actual runtimebriefcase.EpisodeResult, result runtimebriefcase.RunResult) bool {
	got := executorProvenance{model: actual.Model, providerModel: actual.ProviderModel, stopReason: actual.StopReason}
	want := executorProvenance{model: result.Model, providerModel: result.ProviderModel, stopReason: "end_turn"}
	return got == want && lowerSHA256(actual.SystemPromptSHA256)
}

func validateFollowUpEpisodes(pack *casepack.Pack, result runtimebriefcase.RunResult, budget completedRunBudget) error {
	timelineCount := len(pack.Manifest.Episodes)
	followUps := result.Episodes[timelineCount:]
	if len(followUps) == 0 {
		return nil
	}
	plan, err := loadSignedUserSimulatorPlan(pack)
	if err != nil {
		return err
	}
	lastAt := pack.Manifest.FrozenNow
	if timelineCount > 0 {
		lastAt = pack.Manifest.Episodes[timelineCount-1].At
	}
	for index, actual := range followUps {
		cycle := index + 1
		if err := validateFollowUpHeader(cycle, lastAt, actual); err != nil {
			return err
		}
		wantInput := followUpInputDigest(plan.FollowUps[index].Message)
		if !validFollowUpProvenance(actual, result, wantInput) {
			return fmt.Errorf("run result follow-up %d provenance is invalid", cycle)
		}
		budget, err = consumeCompletedRunBudget(budget, fmt.Sprintf("follow-up %d", cycle), actual)
		if err != nil {
			return err
		}
	}
	return nil
}

func validateFollowUpHeader(cycle int, lastAt time.Time, actual runtimebriefcase.EpisodeResult) error {
	if actual.EpisodeID != fmt.Sprintf("simulator-followup-%d", cycle) || actual.Phase != "follow-up" || actual.Cycle != cycle {
		return fmt.Errorf("run result follow-up %d has invalid phase or cycle", cycle)
	}
	at, err := time.Parse(time.RFC3339Nano, actual.At)
	if err != nil || !at.Equal(lastAt) {
		return fmt.Errorf("run result follow-up %d has an invalid timestamp", cycle)
	}
	return nil
}

func followUpInputDigest(message string) string {
	message = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(message, "\r\n", "\n"), "\r", "\n"))
	return casepack.DigestBytes([]byte(chat.SanitizeInput(message)))
}

type followUpProvenance struct {
	model           string
	providerModel   string
	stopReason      string
	releasedSources int
	withheldSources int
}

func validFollowUpProvenance(actual runtimebriefcase.EpisodeResult, result runtimebriefcase.RunResult, wantInput string) bool {
	if actual.InputSHA256 != wantInput || !lowerSHA256(actual.SystemPromptSHA256) {
		return false
	}
	got := followUpProvenance{
		model: actual.Model, providerModel: actual.ProviderModel, stopReason: actual.StopReason,
		releasedSources: len(actual.ReleasedSource), withheldSources: len(actual.WithheldSource),
	}
	want := followUpProvenance{model: result.Model, providerModel: result.ProviderModel, stopReason: "end_turn"}
	return got == want
}

func validateDevicePlanBinding(pack *casepack.Pack, result runtimebriefcase.RunResult) error {
	var role *casepack.Source
	roleCount := 0
	for index := range pack.Manifest.Sources {
		candidate := &pack.Manifest.Sources[index]
		if candidate.Access == casepack.SourceAccessSealed && candidate.SourceRef == devicePlanSourceRef {
			roleCount++
			role = candidate
		}
	}
	switch roleCount {
	case 0:
		if result.DevicePlanSHA256 != "" || result.DevicePlanSourceSHA256 != "" {
			return errors.New("run result declares a device plan that is absent from the signed casepack")
		}
		return nil
	case 1:
		if result.DevicePlanSourceSHA256 != role.SHA256 {
			return errors.New("run result device plan source digest does not match the signed casepack")
		}
		data, err := pack.ReadFile(role.Path)
		if err != nil {
			return fmt.Errorf("read signed device plan: %w", err)
		}
		plans, err := runtimebriefcase.DecodeDevicePlanSource(data)
		if err != nil {
			return fmt.Errorf("decode signed device plan: %w", err)
		}
		digest, err := runtimebriefcase.DevicePlansDigest(plans)
		if err != nil {
			return fmt.Errorf("digest signed device plan: %w", err)
		}
		if result.DevicePlanSHA256 != digest {
			return errors.New("run result canonical device plan digest does not match the signed plan")
		}
		return nil
	default:
		return errors.New("signed casepack must declare at most one briefcase:device-plan role")
	}
}

func loadSignedUserSimulatorPlan(pack *casepack.Pack) (briefcasefb.UserSimulatorPlan, error) {
	var source *casepack.Source
	count := 0
	for index := range pack.Manifest.Sources {
		candidate := &pack.Manifest.Sources[index]
		if candidate.Access == casepack.SourceAccessSealed && candidate.SourceRef == userSimulatorPlanSourceRef {
			count++
			source = candidate
		}
	}
	if count != 1 || source == nil {
		return briefcasefb.UserSimulatorPlan{}, errors.New("run follow-ups require one signed user-simulator plan")
	}
	data, err := pack.ReadFile(source.Path)
	if err != nil {
		return briefcasefb.UserSimulatorPlan{}, fmt.Errorf("read signed user-simulator plan: %w", err)
	}
	var plan briefcasefb.UserSimulatorPlan
	if err := decodeJSONBytes(data, &plan); err != nil {
		return briefcasefb.UserSimulatorPlan{}, fmt.Errorf("decode signed user-simulator plan: %w", err)
	}
	if plan.CaseID != pack.Manifest.CaseID {
		return briefcasefb.UserSimulatorPlan{}, errors.New("signed user-simulator plan caseId does not match")
	}
	if _, err := briefcasefb.NewScriptedUserSimulator(plan, pack.Manifest.RunPolicy.MaxFollowUps); err != nil {
		return briefcasefb.UserSimulatorPlan{}, err
	}
	return plan, nil
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func validateRunResult(result runtimebriefcase.RunResult) error {
	if err := validateRunResultHeader(result); err != nil {
		return err
	}
	if err := validateRunResultDigests(result); err != nil {
		return err
	}
	if err := validateRunResultPayload(result); err != nil {
		return err
	}
	return validateRunEpisodes(result)
}

func validateRunResultHeader(result runtimebriefcase.RunResult) error {
	if result.SchemaVersion != runtimebriefcase.RunSchemaVersion {
		return fmt.Errorf("run result has unsupported schemaVersion %q", result.SchemaVersion)
	}
	if err := runtimebriefcase.ValidateRunProvenance(&result); err != nil {
		return err
	}
	if strings.TrimSpace(result.RunID) == "" || strings.TrimSpace(result.CaseID) == "" || strings.TrimSpace(result.Model) == "" {
		return errors.New("run result requires runId, caseId, and model")
	}
	if result.APIMode != llm.APIModeOpenAI && result.APIMode != llm.APIModeAnthropic {
		return fmt.Errorf("run result has unsupported apiMode %q", result.APIMode)
	}
	if result.SeedForwarded != (result.APIMode == llm.APIModeOpenAI) {
		return errors.New("run result seedForwarded is inconsistent with apiMode")
	}
	if result.RecallMode != "enabled" && result.RecallMode != "disabled" {
		return fmt.Errorf("run result has unsupported recallMode %q", result.RecallMode)
	}
	if result.Arm == runtimebriefcase.ArmRawPrimary && result.RecallMode != "disabled" {
		return errors.New("raw-primary run result must have recall disabled")
	}
	return nil
}

func validateRunResultDigests(result runtimebriefcase.RunResult) error {
	if !lowerSHA256(result.CasepackSHA256) || !lowerSHA256(result.ToolSchemaSHA256) {
		return errors.New("run result requires lowercase casepack and tool-schema SHA-256 values")
	}
	for name, digest := range map[string]string{
		"endpointSha256":             result.EndpointSHA256,
		"buildSha256":                result.BuildSHA256,
		"executionProfileSha256":     result.ExecutionProfileSHA256,
		"systemPromptSequenceSha256": result.SystemPromptSequenceSHA256,
	} {
		if !lowerSHA256(digest) {
			return fmt.Errorf("run result requires lowercase %s", name)
		}
	}
	if (result.DevicePlanSHA256 == "") != (result.DevicePlanSourceSHA256 == "") {
		return errors.New("run result device plan and source digests must be present together")
	}
	if result.DevicePlanSHA256 != "" && (!lowerSHA256(result.DevicePlanSHA256) || !lowerSHA256(result.DevicePlanSourceSHA256)) {
		return errors.New("run result device plan digests must be lowercase SHA-256 values")
	}
	return nil
}

func validateRunResultPayload(result runtimebriefcase.RunResult) error {
	if strings.TrimSpace(result.ArtifactRoot) == "" {
		return errors.New("run result artifactRoot is required")
	}
	if len(result.State) == 0 {
		return errors.New("run result state is required")
	}
	var state any
	if err := decodeJSONBytes(result.State, &state); err != nil {
		return fmt.Errorf("run result state: %w", err)
	}
	return nil
}

func validateRunEpisodes(result runtimebriefcase.RunResult) error {
	if len(result.Episodes) == 0 {
		return errors.New("run result has no episodes")
	}
	seen := make(map[string]struct{}, len(result.Episodes))
	executable := 0
	for _, episode := range result.Episodes {
		if strings.TrimSpace(episode.EpisodeID) == "" {
			return errors.New("run result episodeId is required")
		}
		if _, duplicate := seen[episode.EpisodeID]; duplicate {
			return fmt.Errorf("run result has duplicate episodeId %q", episode.EpisodeID)
		}
		seen[episode.EpisodeID] = struct{}{}
		if episode.Model == "" && episode.Text == "" && episode.AllText == "" {
			if episode.StopReason != "" {
				return fmt.Errorf("run result event %q has a stop reason without executor output", episode.EpisodeID)
			}
			continue
		}
		executable++
		if err := validateExecutorEpisode(episode, result.Model); err != nil {
			return err
		}
	}
	if executable == 0 {
		return errors.New("run result has no completed executor episode")
	}
	return nil
}

func validateExecutorEpisode(episode runtimebriefcase.EpisodeResult, runModel string) error {
	if strings.TrimSpace(episode.Model) == "" {
		return fmt.Errorf("run result executor episode %q has no model", episode.EpisodeID)
	}
	if episode.Model != runModel {
		return fmt.Errorf("run result executor episode %q model %q does not match run model %q", episode.EpisodeID, episode.Model, runModel)
	}
	if episode.StopReason != "end_turn" {
		return fmt.Errorf("run result executor episode %q has incomplete stop reason %q", episode.EpisodeID, episode.StopReason)
	}
	return nil
}

func lowerSHA256(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func authorizeModelEndpoint(raw string, mode casepack.PrivacyMode, allowRemote bool) error {
	if mode == casepack.PrivacyVault {
		return errors.New("Vault execution is not enabled; validate the casepack without running it")
	}
	if mode != casepack.PrivacyPortable {
		return fmt.Errorf("unsupported privacy mode %q", mode)
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return errors.New("--base-url must be an absolute http(s) URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("--base-url must use http or https")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("--base-url must not contain credentials, query, or fragment")
	}
	loopback := isLoopbackHost(parsed.Hostname())
	if !loopback && parsed.Scheme != "https" {
		return errors.New("remote model endpoint must use https")
	}
	if !loopback && !allowRemote {
		return errors.New("remote model endpoint requires --allow-remote-model and a Portable case")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func loadDevicePlanSource(pack *casepack.Pack, path string) ([]byte, string, error) {
	if strings.TrimSpace(path) == "" {
		var source *casepack.Source
		count := 0
		for index := range pack.Manifest.Sources {
			candidate := &pack.Manifest.Sources[index]
			if candidate.Access == casepack.SourceAccessSealed && candidate.SourceRef == devicePlanSourceRef {
				count++
				source = candidate
			}
		}
		if count == 0 {
			return nil, "", nil
		}
		if count != 1 || source == nil {
			return nil, "", errors.New("device plan: casepack must declare at most one signed briefcase:device-plan source")
		}
		data, err := pack.ReadFile(source.Path)
		if err != nil {
			return nil, "", fmt.Errorf("device plan: read signed source: %w", err)
		}
		if _, err := runtimebriefcase.DecodeDevicePlanSource(data); err != nil {
			return nil, "", fmt.Errorf("device plan: %w", err)
		}
		return data, source.SHA256, nil
	}
	data, digest, err := readSealedSource(pack, path, devicePlanSourceRef)
	if err != nil {
		return nil, "", fmt.Errorf("device plan: %w", err)
	}
	if _, err := runtimebriefcase.DecodeDevicePlanSource(data); err != nil {
		return nil, "", fmt.Errorf("device plan: %w", err)
	}
	return data, digest, nil
}

func decodeJSONFile(path string, target any) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("path is required")
	}
	clean := filepath.Clean(path)
	info, err := os.Lstat(clean)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("JSON input must be a regular non-symlink file")
	}
	if info.Size() > maxJSONInput {
		return fmt.Errorf("JSON input exceeds %d bytes", maxJSONInput)
	}
	file, err := os.Open(clean)
	if err != nil {
		return err
	}
	defer file.Close()
	return decodeJSONReader(io.LimitReader(file, maxJSONInput+1), target)
}

func decodeJSONBytes(data []byte, target any) error {
	if len(data) > maxJSONInput {
		return fmt.Errorf("JSON input exceeds %d bytes", maxJSONInput)
	}
	return decodeJSONReader(bytes.NewReader(data), target)
}

func decodeJSONReader(reader io.Reader, target any) error {
	data, err := io.ReadAll(io.LimitReader(reader, maxJSONInput+1))
	if err != nil {
		return err
	}
	if len(data) > maxJSONInput {
		return fmt.Errorf("JSON input exceeds %d bytes", maxJSONInput)
	}
	if err := casepack.RejectDuplicateJSONKeys(data); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are forbidden")
		}
		return err
	}
	return nil
}

func writeJSON(stdout io.Writer, path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if strings.TrimSpace(path) == "" {
		_, err = stdout.Write(data)
		return err
	}
	clean := filepath.Clean(path)
	if info, statErr := os.Lstat(clean); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("output path must not be a symlink")
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	if err := os.WriteFile(clean, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(clean, 0o600)
}
