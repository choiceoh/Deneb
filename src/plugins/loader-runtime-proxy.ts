import type { CreatePluginRuntimeOptions } from "./runtime/index.js";
import type { PluginRuntime } from "./runtime/types.js";

const LAZY_RUNTIME_REFLECTION_KEYS = [
  "version",
  "config",
  "agent",
  "subagent",
  "system",
  "media",
  "tts",
  "stt",
  "tools",
  "channel",
  "events",
  "logging",
  "state",
  "modelAuth",
] as const satisfies readonly (keyof PluginRuntime)[];

/**
 * Creates a lazy Proxy over PluginRuntime that defers initialization until
 * first property access. This avoids eagerly loading every channel/runtime
 * dependency tree during startup paths that discover/skip plugins.
 */
export function createLazyRuntimeProxy(
  resolveCreatePluginRuntime: () => (options?: CreatePluginRuntimeOptions) => PluginRuntime,
  runtimeOptions?: CreatePluginRuntimeOptions,
): PluginRuntime {
  let resolvedRuntime: PluginRuntime | null = null;
  const resolveRuntime = (): PluginRuntime => {
    resolvedRuntime ??= resolveCreatePluginRuntime()(runtimeOptions);
    return resolvedRuntime;
  };
  const lazyRuntimeReflectionKeySet = new Set<PropertyKey>(LAZY_RUNTIME_REFLECTION_KEYS);
  const resolveLazyRuntimeDescriptor = (prop: PropertyKey): PropertyDescriptor | undefined => {
    if (!lazyRuntimeReflectionKeySet.has(prop)) {
      return Reflect.getOwnPropertyDescriptor(resolveRuntime() as object, prop);
    }
    return {
      configurable: true,
      enumerable: true,
      get() {
        return Reflect.get(resolveRuntime() as object, prop);
      },
      set(value: unknown) {
        Reflect.set(resolveRuntime() as object, prop, value);
      },
    };
  };
  return new Proxy({} as PluginRuntime, {
    get(_target, prop, receiver) {
      return Reflect.get(resolveRuntime(), prop, receiver);
    },
    set(_target, prop, value, receiver) {
      return Reflect.set(resolveRuntime(), prop, value, receiver);
    },
    has(_target, prop) {
      return lazyRuntimeReflectionKeySet.has(prop) || Reflect.has(resolveRuntime(), prop);
    },
    ownKeys() {
      return [...LAZY_RUNTIME_REFLECTION_KEYS];
    },
    getOwnPropertyDescriptor(_target, prop) {
      return resolveLazyRuntimeDescriptor(prop);
    },
    defineProperty(_target, prop, attributes) {
      return Reflect.defineProperty(resolveRuntime() as object, prop, attributes);
    },
    deleteProperty(_target, prop) {
      return Reflect.deleteProperty(resolveRuntime() as object, prop);
    },
    getPrototypeOf() {
      return Reflect.getPrototypeOf(resolveRuntime() as object);
    },
  });
}
