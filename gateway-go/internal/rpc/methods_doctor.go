package rpc

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// DoctorDeps holds dependencies for doctor RPC methods.
type DoctorDeps struct {
	// DefaultAgentID is the default agent identifier from config.
	DefaultAgentID string
	// EmbeddingProvider is the name of the configured embedding provider.
	EmbeddingProvider string
}

// RegisterDoctorMethods registers the doctor.memory.status RPC method.
func RegisterDoctorMethods(d *Dispatcher, deps DoctorDeps) {
	d.Register("doctor.memory.status", doctorMemoryStatus(deps))
}

func doctorMemoryStatus(deps DoctorDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		// Collect Go runtime memory stats.
		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)

		// Read system memory from /proc/meminfo (Linux).
		sysMemTotal, sysMemAvail := readProcMeminfo()

		embeddingOK := deps.EmbeddingProvider != ""

		result := map[string]any{
			"agentId":  deps.DefaultAgentID,
			"provider": deps.EmbeddingProvider,
			"embedding": map[string]any{
				"ok": embeddingOK,
			},
			"system": map[string]any{
				"totalMB":     sysMemTotal / (1024 * 1024),
				"availableMB": sysMemAvail / (1024 * 1024),
			},
			"runtime": map[string]any{
				"allocMB":    memStats.Alloc / (1024 * 1024),
				"sysAllocMB": memStats.Sys / (1024 * 1024),
				"numGC":      memStats.NumGC,
			},
		}

		if !embeddingOK {
			result["embedding"] = map[string]any{
				"ok":    false,
				"error": "no embedding provider configured",
			}
		}

		resp, _ := protocol.NewResponseOK(req.ID, result)
		return resp
	}
}

// readProcMeminfo reads total and available memory from /proc/meminfo.
// Returns (0, 0) on non-Linux or if reading fails.
func readProcMeminfo() (total, available uint64) {
	if runtime.GOOS != "linux" {
		return 0, 0
	}

	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fmt.Sscanf(line, "MemTotal: %d kB", &total)
			total *= 1024 // Convert to bytes.
		} else if strings.HasPrefix(line, "MemAvailable:") {
			fmt.Sscanf(line, "MemAvailable: %d kB", &available)
			available *= 1024
		}
		if total > 0 && available > 0 {
			break
		}
	}
	return total, available
}
