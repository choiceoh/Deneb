// pipeline_compat.go — temporary shims for symbols extracted to autoreply/pipeline.
package autoreply

import "github.com/choiceoh/deneb/gateway-go/internal/autoreply/pipeline"

func splitProviderModel(ref string) [2]string {
	return pipeline.SplitProviderModel(ref)
}
