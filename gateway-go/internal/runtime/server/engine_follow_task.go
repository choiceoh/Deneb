package server

import (
	"os"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginecontrol"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginespeed"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/configresolve"
)

// engineFollowEnv switches the routing follow off ("off").
const engineFollowEnv = "DENEB_ENGINE_ROUTING_FOLLOW"

// registerEngineFollowWorkflow starts the routing follow (enginecontrol.FollowTask)
// beside the liveness watcher: the roles on the local engine move to the model
// its production serves. Starts nothing without an engine, a model picker, or
// with the follow switched off.
func (s *Server) registerEngineFollowWorkflow() {
	if s.autonomousSvc == nil || s.modelPicker == nil {
		return
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv(engineFollowEnv)), "off") {
		s.logger.Info("engine routing follow: off (" + engineFollowEnv + ")")
		return
	}
	endpoints := enginespeed.Endpoints()
	if len(endpoints) == 0 {
		return
	}
	endpoint := endpoints[0]
	s.autonomousSvc.RegisterTask(&enginecontrol.FollowTask{
		Endpoint: endpoint,
		Picker:   s.modelPicker,
		// Re-read per pass: the router hot-reloads its config, and an entry added
		// for a new model is followable the moment it is there.
		Entries: func() []enginecontrol.Entry {
			var out []enginecontrol.Entry
			for _, e := range configresolve.EngineEntries(endpoint) {
				out = append(out, enginecontrol.Entry{Name: e.Name, UpstreamModel: e.UpstreamModel, ThinkingMode: e.ThinkingMode, Vision: e.Vision})
			}
			return out
		},
		Liveness: s.engineLiveness,
		Log:      s.engineFollowLog,
		Logger:   s.logger,
	})
	s.logger.Info("engine routing follow registered", "endpoint", endpoint)
}
