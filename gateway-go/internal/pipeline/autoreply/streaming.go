// streaming.go — Message preprocessing hooks.
package autoreply

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
)

// MessagePreprocessHook is called before agent processing to modify the message.
type MessagePreprocessHook func(msg *types.MsgContext) error

// RunPreprocessHooks executes all preprocess hooks on a message.
func RunPreprocessHooks(msg *types.MsgContext, hooks []MessagePreprocessHook) error {
	for _, hook := range hooks {
		if err := hook(msg); err != nil {
			return err
		}
	}
	return nil
}
