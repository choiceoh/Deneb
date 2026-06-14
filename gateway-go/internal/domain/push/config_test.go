package push

import "testing"

func TestConfigEnabled(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"no path", Config{}, false},
		{"blank path", Config{CredentialsFile: "   "}, false},
		{"path set", Config{CredentialsFile: "/etc/deneb/fcm.json"}, true},
		{"disabled override", Config{CredentialsFile: "/etc/deneb/fcm.json", Disabled: true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.Enabled(); got != tc.want {
				t.Errorf("Enabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("DENEB_FCM_CREDENTIALS_FILE", "/path/to/sa.json")
	t.Setenv("DENEB_FCM_DISABLE", "")
	cfg := ConfigFromEnv()
	if cfg.CredentialsFile != "/path/to/sa.json" || !cfg.Enabled() {
		t.Fatalf("cfg = %+v, want enabled with path", cfg)
	}

	t.Setenv("DENEB_FCM_DISABLE", "1")
	if ConfigFromEnv().Enabled() {
		t.Error("DENEB_FCM_DISABLE=1 should disable")
	}
}

func TestConfigFromEnv_UnsetIsDormant(t *testing.T) {
	t.Setenv("DENEB_FCM_CREDENTIALS_FILE", "")
	t.Setenv("DENEB_FCM_DISABLE", "")
	if ConfigFromEnv().Enabled() {
		t.Error("unset credentials must be dormant (disabled)")
	}
}
