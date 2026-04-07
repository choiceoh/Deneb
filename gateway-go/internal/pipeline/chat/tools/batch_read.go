package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// ToolBatchRead reads up to 40 files in a single call using parallel I/O.
// Each file supports the same options as the read tool (offset, limit, function).
// Per-file errors are reported inline without aborting the entire batch.
// Results are reassembled in the original request order.
func ToolBatchRead(defaultDir string) ToolFunc {
	readFn := ToolRead(defaultDir)

	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Files []struct {
				FilePath string `json:"file_path"`
				Offset   int    `json:"offset"`
				Limit    int    `json:"limit"`
				Function string `json:"function"`
			} `json:"files"`
		}
		if err := jsonutil.UnmarshalInto("batch_read params", input, &p); err != nil {
			return "", err
		}
		if len(p.Files) == 0 {
			return "", fmt.Errorf("files is required and must not be empty")
		}

		type fileResult struct {
			output string
			err    error
		}

		n := len(p.Files)
		results := make([]fileResult, n)

		// Read all files concurrently — file I/O benefits from parallelism.
		var wg sync.WaitGroup
		wg.Add(n)
		for i, f := range p.Files {
			go func(idx int, f struct {
				FilePath string `json:"file_path"`
				Offset   int    `json:"offset"`
				Limit    int    `json:"limit"`
				Function string `json:"function"`
			}) {
				defer wg.Done()
				fileInput, _ := json.Marshal(map[string]any{
					"file_path": f.FilePath,
					"offset":    f.Offset,
					"limit":     f.Limit,
					"function":  f.Function,
				})
				results[idx].output, results[idx].err = readFn(ctx, fileInput)
			}(i, f)
		}
		wg.Wait()

		// Reassemble in original order.
		var sb strings.Builder
		successCount := 0
		for i, r := range results {
			if i > 0 {
				sb.WriteString("\n---\n\n")
			}
			if r.err != nil {
				fmt.Fprintf(&sb, "[Error reading %s: %s]\n", p.Files[i].FilePath, r.err.Error())
				continue
			}
			sb.WriteString(r.output)
			successCount++
		}

		fmt.Fprintf(&sb, "\n---\n[batch_read: %d/%d files read successfully]\n", successCount, n)
		return TruncateForLLM(sb.String()), nil
	}
}
