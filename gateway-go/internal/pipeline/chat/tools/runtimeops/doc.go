// Package runtimeops owns command execution and managed-process control
// (exec / process). Host wrappers (browser, fleet, solarflow, workstation)
// live in sibling hostops; gateway self-management lives in gatewayops;
// sessions live in sessionops; phone tools live in phoneops.
//
// Registration stays in toolreg; this package does not depend on its parent
// tools package.
package runtimeops
