// Package infrabind re-exports core/infra packages used by the server composition root.
// Type/var aliases only — no adapter logic.
package infrabind

import (
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/logging"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/metrics"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/middleware"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/process"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/shortid"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/sparkfleet"
)

// --- core/agentlog ---

type (
	ModelStat = agentlog.ModelStat
	ToolStat  = agentlog.ToolStat
	Writer    = agentlog.Writer
)

var NewWriter = agentlog.NewWriter

// --- core/observe ---

type (
	LogCapture = observe.LogCapture
	LogLine    = observe.LogLine
	QueryOpts  = observe.QueryOpts
)

var (
	DefaultRingSize = observe.DefaultRingSize
	NewCapture      = observe.NewCapture
	NewRing         = observe.NewRing
)

// --- infra/config ---

type (
	ConfigSnapshot       = config.ConfigSnapshot
	GatewayRuntimeConfig = config.GatewayRuntimeConfig
	TopicsConfig         = config.TopicsConfig
)

var (
	LoadConfigFromDefaultPath = config.LoadConfigFromDefaultPath
	ResolveStateDir           = config.ResolveStateDir
	DefaultStateDirname       = config.DefaultStateDirname
)

// --- infra/logging ---

var PrintShutdown = logging.PrintShutdown

// --- infra/metrics ---

var RPCInstrumentation = metrics.RPCInstrumentation

// --- infra/middleware ---

var Logging = middleware.Logging

// --- infra/process ---

type (
	ExecRequest = process.ExecRequest
	Manager     = process.Manager
)

var (
	NewManager    = process.NewManager
	StatusRunning = process.StatusRunning
)

// --- infra/shortid ---

var NewShortID = shortid.New

// --- infra/sparkfleet ---

type (
	SparkFleetClient = sparkfleet.Client
)

var NewSparkFleetClient = sparkfleet.New
