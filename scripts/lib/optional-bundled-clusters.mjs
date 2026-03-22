export const optionalBundledClusters = [
  "acpx",
  "diagnostics-otel",
  "diffs",
  "memory-lancedb",
  "ui",
];

export const optionalBundledClusterSet = new Set(optionalBundledClusters);

export const OPTIONAL_BUNDLED_BUILD_ENV = "DENEB_INCLUDE_OPTIONAL_BUNDLED";

export function isOptionalBundledCluster(cluster) {
  return optionalBundledClusterSet.has(cluster);
}

export function shouldIncludeOptionalBundledClusters(env = process.env) {
  return env[OPTIONAL_BUNDLED_BUILD_ENV] === "1";
}

export function shouldBuildBundledCluster(cluster, env = process.env) {
  return shouldIncludeOptionalBundledClusters(env) || !isOptionalBundledCluster(cluster);
}
