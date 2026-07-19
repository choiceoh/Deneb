package embedindex

import (
	"os"
	"strconv"
	"strings"
)

// SemanticSurface names a retrieval decision whose cosine admission floor was
// measured against a specific embedding model. Keeping the values beside the
// embedder identity prevents a sidecar swap from silently reusing an
// incompatible score scale.
type SemanticSurface string

const (
	SemanticSurfaceMail        SemanticSurface = "mail"
	SemanticSurfaceFetchTools  SemanticSurface = "fetch_tools"
	SemanticSurfaceRSIExemplar SemanticSurface = "rsi_exemplar"
	SemanticSurfaceWorkFeed    SemanticSurface = "workfeed"
)

const uncalibratedSemanticFloor = 1.01 // cosine cannot exceed 1

var nemotronSemanticFloors = map[SemanticSurface]float64{
	SemanticSurfaceMail:        0.33,
	SemanticSurfaceFetchTools:  0.30,
	SemanticSurfaceRSIExemplar: 0.32,
	SemanticSurfaceWorkFeed:    0.47,
}

var semanticFloorEnv = map[SemanticSurface]string{
	SemanticSurfaceMail:        "DENEB_MAIL_SEM_FLOOR",
	SemanticSurfaceFetchTools:  "DENEB_FETCH_TOOLS_SEM_FLOOR",
	SemanticSurfaceRSIExemplar: "DENEB_RSI_EXEMPLAR_SEM_FLOOR",
	SemanticSurfaceWorkFeed:    "DENEB_WORKFEED_SEM_FLOOR",
}

// Calibration describes which score profile controls one semantic-only
// admission decision. Calibrated=false means dense ranks may still reinforce a
// lexical hit, but a dense-only hit is held back until the model is measured or
// an operator explicitly supplies an override.
type Calibration struct {
	Surface     SemanticSurface `json:"surface"`
	Fingerprint string          `json:"fingerprint,omitempty"`
	Profile     string          `json:"profile"`
	Floor       float64         `json:"floor"`
	Calibrated  bool            `json:"calibrated"`
}

// CalibrationFor returns the model-specific semantic admission profile. Test
// doubles and legacy embedders without an identity retain the shipped Nemotron
// values for compatibility; a real, non-empty unknown fingerprint fails closed
// for semantic-only admission. A valid per-surface environment override is the
// explicit operator escape hatch for newly measured models.
func CalibrationFor(embedder any, surface SemanticSurface) Calibration {
	identity := IdentityOf(embedder)
	calibration := Calibration{
		Surface:     surface,
		Fingerprint: identity.Fingerprint,
		Profile:     "uncalibrated",
		Floor:       uncalibratedSemanticFloor,
	}
	if floor, ok := semanticFloorOverride(surface); ok {
		calibration.Profile = "operator_override"
		calibration.Floor = floor
		calibration.Calibrated = true
		return calibration
	}

	floor, knownSurface := nemotronSemanticFloors[surface]
	if !knownSurface {
		return calibration
	}
	fingerprint := strings.ToLower(strings.TrimSpace(identity.Fingerprint))
	if fingerprint == "" {
		calibration.Profile = "identity_unavailable"
		calibration.Floor = floor
		return calibration
	}
	if strings.Contains(fingerprint, "nemotron-3-embed-1b") {
		calibration.Profile = "nemotron_3_embed_1b"
		calibration.Floor = floor
		calibration.Calibrated = true
	}
	return calibration
}

func semanticFloorOverride(surface SemanticSurface) (float64, bool) {
	key := semanticFloorEnv[surface]
	if key == "" {
		return 0, false
	}
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, false
	}
	floor, err := strconv.ParseFloat(raw, 64)
	if err != nil || floor < 0 || floor > 1 {
		return 0, false
	}
	return floor, true
}
