// Package toolwire is the thin facade over tool registration subpackages.
//
// Heavy tool-package imports live behind toolwire/wire → {core,ops,domain,chrono},
// plus toolwire/bridge, toolwire/attach, and toolwire/recall (server-side
// polaris/knowledge). Schemas live in toolwire/schema. This parent stays within
// Health Bench fan-out limits (direct ≤8, two-hop ≤25).
//
// Dependency flow: toolwire -> subpackages -> toolport/tooldeps + tools/*;
// never imports chat/.
package toolwire
