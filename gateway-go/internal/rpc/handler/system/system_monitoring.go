// system_monitoring.go — monitoring.* and doctor.* RPC handlers.
package system

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/metrics"
	"github.com/choiceoh/deneb/gateway-go/internal/monitoring"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// MethodLister returns all registered RPC method names.
type MethodLister interface {
	Methods() []string
}

// MonitoringDeps holds the dependencies for monitoring RPC methods.
type MonitoringDeps struct {
	ChannelHealth *monitoring.ChannelHealthMonitor
	Activity      *monitoring.ActivityTracker
	Dispatcher    MethodLister // for rpc_zero_calls
}

// MonitoringMethods returns the monitoring.channel_health and
// monitoring.activity handlers.
func MonitoringMethods(deps MonitoringDeps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"monitoring.channel_health": func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
			if deps.ChannelHealth == nil {
				resp := protocol.MustResponseOK(req.ID, map[string]any{"channels": []any{}})
				return resp
			}
			snapshot := deps.ChannelHealth.HealthSnapshot()
			resp := protocol.MustResponseOK(req.ID, map[string]any{"channels": snapshot})
			return resp
		},

		"monitoring.activity": func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
			if deps.Activity == nil {
				resp := protocol.MustResponseOK(req.ID, map[string]any{"lastActivityMs": 0})
				return resp
			}
			resp := protocol.MustResponseOK(req.ID, map[string]any{
				"lastActivityMs": deps.Activity.LastActivityAt(),
			})
			return resp
		},

		"monitoring.rpc_zero_calls": func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
			if deps.Dispatcher == nil {
				return protocol.MustResponseOK(req.ID, map[string]any{
					"zeroCalls": []any{},
					"total":     0,
				})
			}

			// Get all registered methods and their call counts.
			methods := deps.Dispatcher.Methods()
			sort.Strings(methods)

			counts := metrics.RPCRequestsTotal.Snapshot()

			// Find methods with zero calls.
			var zeroCalls []string
			called := make([]map[string]any, 0)
			for _, m := range methods {
				okKey := m + "\x00" + "ok"
				errKey := m + "\x00" + "error"
				ok := counts[okKey]
				errs := counts[errKey]
				total := ok + errs
				if total == 0 {
					zeroCalls = append(zeroCalls, m)
				} else {
					called = append(called, map[string]any{
						"method": m,
						"ok":     ok,
						"error":  errs,
					})
				}
			}

			resp := protocol.MustResponseOK(req.ID, map[string]any{
				"zeroCalls":    zeroCalls,
				"zeroCount":    len(zeroCalls),
				"calledCount":  len(called),
				"called":       called,
				"totalMethods": len(methods),
			})
			return resp
		},
	}
}

// DoctorDeps holds dependencies for doctor RPC methods.
type DoctorDeps struct {
	// DefaultAgentID is the default agent identifier from config.
	DefaultAgentID string
	// EmbeddingProvider is the name of the configured embedding provider.
	EmbeddingProvider string
}

// DoctorMethods returns the doctor.memory.status handler.
func DoctorMethods(deps DoctorDeps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"doctor.memory.status": doctorMemoryStatus(deps),
	}
}

func doctorMemoryStatus(deps DoctorDeps) rpcutil.HandlerFunc {
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
// Returns (0, 0) if reading fails.
func readProcMeminfo() (total, available uint64) {
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
