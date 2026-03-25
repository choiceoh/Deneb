import type { WizardPrompter } from "./prompts.js";

/**
 * Stub: wizard implementation removed. Returns a no-op prompter.
 * Callers that need real interactive prompts should use their own prompter implementation.
 */
export function createClackPrompter(): WizardPrompter {
  const noop = async () => {};
  return {
    intro: noop,
    outro: noop,
    note: noop,
    select: async <T>(params: { options: Array<{ value: T }> }): Promise<T> => {
      return params.options[0].value;
    },
    multiselect: async () => [],
    text: async () => "",
    confirm: async () => false,
    progress: () => ({
      update: () => {},
      stop: () => {},
    }),
  };
}

export function tokenizedOptionFilter<T>(_search: string, _option: { value: T }): boolean {
  return true;
}
