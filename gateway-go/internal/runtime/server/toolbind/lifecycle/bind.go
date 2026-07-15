package lifecycle

import (
	chattools "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/lifecycletool"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/runtimeops"
)

type (
	HeartbeatShadowReplayResult        = chattools.HeartbeatShadowReplayResult
	HeartbeatShadowReplayFixtureResult = chattools.HeartbeatShadowReplayFixtureResult
	SkillLifecycleBackend              = chattools.SkillLifecycleBackend
	ToolFunc                           = chattools.ToolFunc
)

var (
	SkillLifecycleToolSchema  = chattools.SkillLifecycleToolSchema
	ToolSkillLifecycle        = chattools.ToolSkillLifecycle
	ErrPhoneActionUnconfirmed = runtimeops.ErrPhoneActionUnconfirmed
)
