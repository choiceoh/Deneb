package propus

// Config holds Propus channel configuration from deneb.json.
type Config struct {
	// Enabled controls whether the Propus channel starts.
	Enabled bool `json:"enabled"`
	// Port is the WebSocket listen port for Propus clients (default: 3710).
	Port int `json:"port"`
	// Bind is the listen address: "loopback" (127.0.0.1) or "all" (0.0.0.0).
	Bind string `json:"bind"`
	// Tools selects the tool set: "coding" (default) restricts to code tools.
	Tools string `json:"tools"`
}

// DefaultConfig returns sensible defaults for local-only coding use.
func DefaultConfig() *Config {
	return &Config{
		Enabled: false,
		Port:    3710,
		Bind:    "loopback",
		Tools:   "coding",
	}
}

// ListenAddr returns the resolved listen address.
func (c *Config) ListenAddr() string {
	host := "127.0.0.1"
	if c.Bind == "all" {
		host = "0.0.0.0"
	}
	port := c.Port
	if port == 0 {
		port = 3710
	}
	return host + ":" + itoa(port)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
