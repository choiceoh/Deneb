package embedindex

import "testing"

type identifiedCalibrationEmbedder struct {
	fingerprint string
	dimensions  int
}

func (e identifiedCalibrationEmbedder) EmbeddingFingerprint() string { return e.fingerprint }
func (e identifiedCalibrationEmbedder) EmbeddingDimensions() int     { return e.dimensions }

func TestCalibrationForUsesNemotronSurfaceProfile(t *testing.T) {
	embedder := identifiedCalibrationEmbedder{
		fingerprint: "Nemotron-3-Embed-1B-NVFP4:2048",
		dimensions:  2048,
	}
	wants := map[SemanticSurface]float64{
		SemanticSurfaceMail:        0.33,
		SemanticSurfaceFetchTools:  0.30,
		SemanticSurfaceRSIExemplar: 0.32,
		SemanticSurfaceWorkFeed:    0.47,
	}
	for surface, want := range wants {
		got := CalibrationFor(embedder, surface)
		if !got.Calibrated || got.Profile != "nemotron_3_embed_1b" || got.Floor != want {
			t.Errorf("%s calibration = %+v, want floor %.2f", surface, got, want)
		}
	}
}

func TestCalibrationForUnknownFingerprintBlocksSemanticOnlyAdmission(t *testing.T) {
	got := CalibrationFor(identifiedCalibrationEmbedder{fingerprint: "future-model:768", dimensions: 768}, SemanticSurfaceMail)
	if got.Calibrated || got.Profile != "uncalibrated" || got.Floor <= 1 {
		t.Fatalf("unknown-model calibration = %+v", got)
	}
}

func TestCalibrationForIdentitylessTestDoubleKeepsCompatibilityFloor(t *testing.T) {
	got := CalibrationFor(struct{}{}, SemanticSurfaceFetchTools)
	if got.Calibrated || got.Profile != "identity_unavailable" || got.Floor != 0.30 {
		t.Fatalf("identityless calibration = %+v", got)
	}
}

func TestCalibrationForOperatorOverrideAdmitsMeasuredReplacement(t *testing.T) {
	t.Setenv("DENEB_WORKFEED_SEM_FLOOR", "0.61")
	got := CalibrationFor(identifiedCalibrationEmbedder{fingerprint: "replacement:768", dimensions: 768}, SemanticSurfaceWorkFeed)
	if !got.Calibrated || got.Profile != "operator_override" || got.Floor != 0.61 {
		t.Fatalf("override calibration = %+v", got)
	}
}

func TestCalibrationForRejectsMalformedOverride(t *testing.T) {
	t.Setenv("DENEB_MAIL_SEM_FLOOR", "1.5")
	got := CalibrationFor(identifiedCalibrationEmbedder{fingerprint: "replacement:768", dimensions: 768}, SemanticSurfaceMail)
	if got.Calibrated || got.Floor <= 1 {
		t.Fatalf("malformed override calibration = %+v", got)
	}
}
