package nativeapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

func (s *Handler) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Server", "deneb-gateway")
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	if err := json.NewEncoder(w).Encode(value); err != nil && s.logger != nil {
		httputil.LogEncodeError(s.logger, "native api: json encode error", err)
	}
}

func disableWriteDeadline(w http.ResponseWriter) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
}
