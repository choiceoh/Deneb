// Hand-written constants. Previously generated from YAML.

package process

// blockedEnvKeys lists environment variable names that must be stripped
// from subprocess environments.
var blockedEnvKeys = map[string]struct{}{
	"LD_PRELOAD":           {},
	"LD_LIBRARY_PATH":      {},
	"BASH_ENV":             {},
	"ENV":                  {},
	"ZDOTDIR":              {},
	"MAVEN_OPTS":           {},
	"SBT_OPTS":             {},
	"GRADLE_OPTS":          {},
	"_JAVA_OPTIONS":        {},
	"JAVA_TOOL_OPTIONS":    {},
	"PYTHONSTARTUP":        {},
	"PERL5OPT":             {},
	"RUBYOPT":              {},
	"DOTNET_STARTUP_HOOKS": {},
	"DOTNET_ROOT":          {},
	"GLIBC_TUNABLES":       {},
}

// blockedEnvPrefixes lists environment variable prefixes that should be blocked.
var blockedEnvPrefixes = []string{
	"DYLD_",
	"LD_AUDIT",
}
