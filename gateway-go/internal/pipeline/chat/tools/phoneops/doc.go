// Package phoneops owns the agent-facing phone_read / phone_write tools.
//
// Reads serve the cache the native app pushes (location, battery, usage,
// call log, message ledger). Writes dispatch P1 in-app actions through
// tooldeps.PhoneActionFunc. Registration stays in toolwire/ops.
package phoneops
