package localai

import (
	"context"
	"net/http"
	"time"
)

const (
	healthCheckInterval = 1 * time.Minute
	healthWarmupTTL     = 10 * time.Second
	healthWarmupPeriod  = 1 * time.Minute
	healthPingTimeout   = 3 * time.Second
)

// healthChecker runs periodic liveness pings against the local AI /models endpoint.
type healthChecker struct {
	hub     *Hub
	baseURL string
	started time.Time
}

func (hc *healthChecker) interval() time.Duration {
	if time.Since(hc.started) < healthWarmupPeriod {
		return healthWarmupTTL
	}
	return healthCheckInterval
}

// run is the background health check loop.
func (hc *healthChecker) run() {
	defer hc.hub.wg.Done()

	// Immediate first check.
	hc.check()

	for {
		select {
		case <-hc.hub.ctx.Done():
			return
		case <-time.After(hc.interval()):
			hc.check()
		}
	}
}

func (hc *healthChecker) check() {
	alive := hc.pingModels()
	hc.hub.healthy.Store(alive)
	hc.hub.lastHealthCheck.Store(time.Now().Unix())
}

func (hc *healthChecker) pingModels() bool {
	ctx, cancel := context.WithTimeout(hc.hub.ctx, healthPingTimeout)
	defer cancel()

	url := hc.baseURL + "/models"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

