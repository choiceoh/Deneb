import { type DependencyList } from "react";

// Object.is over two dependency tuples — the comparison React itself uses for
// effect deps. Shared by the render-adjust pattern (reset state when inputs
// change, during render instead of a sync effect write — see
// https://react.dev/learn/you-might-not-need-an-effect).
export function depsEqual(a: DependencyList, b: DependencyList): boolean {
  return a.length === b.length && a.every((v, i) => Object.is(v, b[i]));
}
