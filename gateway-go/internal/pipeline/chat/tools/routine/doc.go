// Package routine collects and renders recurring morning, evening, and weekly
// business briefings. Collection and deterministic rendering live together so
// cron, interactive commands, and deferred tools share one contract.
//
// The package does not import its parent tools package.
package routine
