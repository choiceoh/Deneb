package lifecycle

import (
	chattools "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/lifecycletool"
)

type (
	HeartbeatShadowReplayResult        = chattools.HeartbeatShadowReplayResult
	HeartbeatShadowReplayFixtureResult = chattools.HeartbeatShadowReplayFixtureResult
	SkillLifecycleBackend              = chattools.SkillLifecycleBackend
	ToolFunc                           = chattools.ToolFunc
)

var (
	SkillLifecycleToolDescription = chattools.SkillLifecycleToolDescription
	SkillLifecycleToolSchema      = chattools.SkillLifecycleToolSchema
	ToolSkillLifecycle            = chattools.ToolSkillLifecycle
)
