// Exec command summarization for tool display.
// Extracted from tool-display-common.ts for clarity — this module handles
// shell parsing, command recognition, and human-readable exec summaries.

function stripOuterQuotes(value: string | undefined): string | undefined {
  if (!value) {
    return value;
  }
  const trimmed = value.trim();
  if (
    trimmed.length >= 2 &&
    ((trimmed.startsWith('"') && trimmed.endsWith('"')) ||
      (trimmed.startsWith("'") && trimmed.endsWith("'")))
  ) {
    return trimmed.slice(1, -1).trim();
  }
  return trimmed;
}

function splitShellWords(input: string | undefined, maxWords = 48): string[] {
  if (!input) {
    return [];
  }

  const words: string[] = [];
  let current = "";
  let quote: '"' | "'" | undefined;
  let escaped = false;

  for (let i = 0; i < input.length; i += 1) {
    const char = input[i];

    if (escaped) {
      current += char;
      escaped = false;
      continue;
    }
    if (char === "\\") {
      escaped = true;
      continue;
    }

    if (quote) {
      if (char === quote) {
        quote = undefined;
      } else {
        current += char;
      }
      continue;
    }

    if (char === '"' || char === "'") {
      quote = char;
      continue;
    }

    if (/\s/.test(char)) {
      if (!current) {
        continue;
      }
      words.push(current);
      if (words.length >= maxWords) {
        return words;
      }
      current = "";
      continue;
    }

    current += char;
  }

  if (current) {
    words.push(current);
  }
  return words;
}

function binaryName(token: string | undefined): string | undefined {
  if (!token) {
    return undefined;
  }
  const cleaned = stripOuterQuotes(token) ?? token;
  const segment = cleaned.split(/[/]/).at(-1) ?? cleaned;
  return segment.trim().toLowerCase();
}

function optionValue(words: string[], names: string[]): string | undefined {
  const lookup = new Set(names);

  for (let i = 0; i < words.length; i += 1) {
    const token = words[i];
    if (!token) {
      continue;
    }

    if (lookup.has(token)) {
      const value = words[i + 1];
      if (value && !value.startsWith("-")) {
        return value;
      }
      continue;
    }

    for (const name of names) {
      if (name.startsWith("--") && token.startsWith(`${name}=`)) {
        return token.slice(name.length + 1);
      }
    }
  }

  return undefined;
}

function positionalArgs(words: string[], from = 1, optionsWithValue: string[] = []): string[] {
  const args: string[] = [];
  const takesValue = new Set(optionsWithValue);

  for (let i = from; i < words.length; i += 1) {
    const token = words[i];
    if (!token) {
      continue;
    }

    if (token === "--") {
      for (let j = i + 1; j < words.length; j += 1) {
        const candidate = words[j];
        if (candidate) {
          args.push(candidate);
        }
      }
      break;
    }

    if (token.startsWith("--")) {
      if (token.includes("=")) {
        continue;
      }
      if (takesValue.has(token)) {
        i += 1;
      }
      continue;
    }

    if (token.startsWith("-")) {
      if (takesValue.has(token)) {
        i += 1;
      }
      continue;
    }

    args.push(token);
  }

  return args;
}

function firstPositional(
  words: string[],
  from = 1,
  optionsWithValue: string[] = [],
): string | undefined {
  return positionalArgs(words, from, optionsWithValue)[0];
}

function trimLeadingEnv(words: string[]): string[] {
  if (words.length === 0) {
    return words;
  }

  let index = 0;
  if (binaryName(words[0]) === "env") {
    index = 1;
    while (index < words.length) {
      const token = words[index];
      if (!token) {
        break;
      }
      if (token.startsWith("-")) {
        index += 1;
        continue;
      }
      if (/^[A-Za-z_][A-Za-z0-9_]*=/.test(token)) {
        index += 1;
        continue;
      }
      break;
    }
    return words.slice(index);
  }

  while (index < words.length && /^[A-Za-z_][A-Za-z0-9_]*=/.test(words[index])) {
    index += 1;
  }
  return words.slice(index);
}

function unwrapShellWrapper(command: string): string {
  const words = splitShellWords(command, 10);
  if (words.length < 3) {
    return command;
  }

  const bin = binaryName(words[0]);
  if (!(bin === "bash" || bin === "sh" || bin === "zsh" || bin === "fish")) {
    return command;
  }

  const flagIndex = words.findIndex(
    (token, index) => index > 0 && (token === "-c" || token === "-lc" || token === "-ic"),
  );
  if (flagIndex === -1) {
    return command;
  }

  const inner = words
    .slice(flagIndex + 1)
    .join(" ")
    .trim();
  return inner ? (stripOuterQuotes(inner) ?? command) : command;
}

function scanTopLevelChars(
  command: string,
  visit: (char: string, index: number) => boolean | void,
): void {
  let quote: '"' | "'" | undefined;
  let escaped = false;

  for (let i = 0; i < command.length; i += 1) {
    const char = command[i];

    if (escaped) {
      escaped = false;
      continue;
    }
    if (char === "\\") {
      escaped = true;
      continue;
    }

    if (quote) {
      if (char === quote) {
        quote = undefined;
      }
      continue;
    }

    if (char === '"' || char === "'") {
      quote = char;
      continue;
    }

    if (visit(char, i) === false) {
      return;
    }
  }
}

function splitTopLevelStages(command: string): string[] {
  const parts: string[] = [];
  let start = 0;

  scanTopLevelChars(command, (char, index) => {
    if (char === ";") {
      parts.push(command.slice(start, index));
      start = index + 1;
      return true;
    }
    if ((char === "&" || char === "|") && command[index + 1] === char) {
      parts.push(command.slice(start, index));
      start = index + 2;
      return true;
    }
    return true;
  });

  parts.push(command.slice(start));
  return parts.map((part) => part.trim()).filter((part) => part.length > 0);
}

function splitTopLevelPipes(command: string): string[] {
  const parts: string[] = [];
  let start = 0;

  scanTopLevelChars(command, (char, index) => {
    if (char === "|" && command[index - 1] !== "|" && command[index + 1] !== "|") {
      parts.push(command.slice(start, index));
      start = index + 1;
    }
    return true;
  });

  parts.push(command.slice(start));
  return parts.map((part) => part.trim()).filter((part) => part.length > 0);
}

function parseChdirTarget(head: string): string | undefined {
  const words = splitShellWords(head, 3);
  const bin = binaryName(words[0]);
  if (bin === "cd" || bin === "pushd") {
    return words[1] || undefined;
  }
  return undefined;
}

function isChdirCommand(head: string): boolean {
  const bin = binaryName(splitShellWords(head, 2)[0]);
  return bin === "cd" || bin === "pushd" || bin === "popd";
}

function isPopdCommand(head: string): boolean {
  return binaryName(splitShellWords(head, 2)[0]) === "popd";
}

type PreambleResult = {
  command: string;
  chdirPath?: string;
};

function stripShellPreamble(command: string): PreambleResult {
  let rest = command.trim();
  let chdirPath: string | undefined;

  for (let i = 0; i < 4; i += 1) {
    // Find the first top-level separator (&&, ||, ;, \n) respecting quotes/escaping.
    let first: { index: number; length: number; isOr?: boolean } | undefined;
    scanTopLevelChars(rest, (char, idx) => {
      if (char === "&" && rest[idx + 1] === "&") {
        first = { index: idx, length: 2 };
        return false;
      }
      if (char === "|" && rest[idx + 1] === "|") {
        first = { index: idx, length: 2, isOr: true };
        return false;
      }
      if (char === ";" || char === "\n") {
        first = { index: idx, length: 1 };
        return false;
      }
    });
    const head = (first ? rest.slice(0, first.index) : rest).trim();
    // cd/pushd/popd is preamble when followed by && / ; / \n, or when we already
    // stripped at least one preamble segment (handles chained cd's like `cd /tmp && cd /app`).
    // NOT for || — `cd /app || npm install` means npm runs when cd *fails*, so (in /app) is wrong.
    const isChdir = (first ? !first.isOr : i > 0) && isChdirCommand(head);
    const isPreamble =
      head.startsWith("set ") || head.startsWith("export ") || head.startsWith("unset ") || isChdir;

    if (!isPreamble) {
      break;
    }

    if (isChdir) {
      // popd returns to the previous directory, so inferred cwd from earlier
      // preamble steps is no longer reliable.
      if (isPopdCommand(head)) {
        chdirPath = undefined;
      } else {
        chdirPath = parseChdirTarget(head) ?? chdirPath;
      }
    }

    rest = first ? rest.slice(first.index + first.length).trimStart() : "";
    if (!rest) {
      break;
    }
  }

  return { command: rest.trim(), chdirPath };
}

function summarizeKnownExec(words: string[]): string {
  if (words.length === 0) {
    return "run command";
  }

  const bin = binaryName(words[0]) ?? "command";

  if (bin === "git") {
    const globalWithValue = new Set([
      "-C",
      "-c",
      "--git-dir",
      "--work-tree",
      "--namespace",
      "--config-env",
    ]);

    const gitCwd = optionValue(words, ["-C"]);

    let sub: string | undefined;
    for (let i = 1; i < words.length; i += 1) {
      const token = words[i];
      if (!token) {
        continue;
      }
      if (token === "--") {
        sub = firstPositional(words, i + 1);
        break;
      }
      if (token.startsWith("--")) {
        if (token.includes("=")) {
          continue;
        }
        if (globalWithValue.has(token)) {
          i += 1;
        }
        continue;
      }
      if (token.startsWith("-")) {
        if (globalWithValue.has(token)) {
          i += 1;
        }
        continue;
      }
      sub = token;
      break;
    }

    const map: Record<string, string> = {
      status: "check git status",
      diff: "check git diff",
      log: "view git history",
      show: "show git object",
      branch: "list git branches",
      checkout: "switch git branch",
      switch: "switch git branch",
      commit: "create git commit",
      pull: "pull git changes",
      push: "push git changes",
      fetch: "fetch git changes",
      merge: "merge git changes",
      rebase: "rebase git branch",
      add: "stage git changes",
      restore: "restore git files",
      reset: "reset git state",
      stash: "stash git changes",
    };

    if (sub && map[sub]) {
      return map[sub];
    }
    if (!sub || sub.startsWith("/") || sub.startsWith("~") || sub.includes("/")) {
      return gitCwd ? `run git command in ${gitCwd}` : "run git command";
    }
    return `run git ${sub}`;
  }

  if (bin === "grep" || bin === "rg" || bin === "ripgrep") {
    const positional = positionalArgs(words, 1, [
      "-e",
      "--regexp",
      "-f",
      "--file",
      "-m",
      "--max-count",
      "-A",
      "--after-context",
      "-B",
      "--before-context",
      "-C",
      "--context",
    ]);
    const pattern = optionValue(words, ["-e", "--regexp"]) ?? positional[0];
    const target = positional.length > 1 ? positional.at(-1) : undefined;
    if (pattern) {
      return target ? `search "${pattern}" in ${target}` : `search "${pattern}"`;
    }
    return "search text";
  }

  if (bin === "find") {
    const path = words[1] && !words[1].startsWith("-") ? words[1] : ".";
    const name = optionValue(words, ["-name", "-iname"]);
    return name ? `find files named "${name}" in ${path}` : `find files in ${path}`;
  }

  if (bin === "ls") {
    const target = firstPositional(words, 1);
    return target ? `list files in ${target}` : "list files";
  }

  if (bin === "head" || bin === "tail") {
    const lines =
      optionValue(words, ["-n", "--lines"]) ??
      words
        .slice(1)
        .find((token) => /^-\d+$/.test(token))
        ?.slice(1);
    const positional = positionalArgs(words, 1, ["-n", "--lines"]);
    let target = positional.at(-1);
    if (target && /^\d+$/.test(target) && positional.length === 1) {
      target = undefined;
    }
    const side = bin === "head" ? "first" : "last";
    const unit = lines === "1" ? "line" : "lines";
    if (lines && target) {
      return `show ${side} ${lines} ${unit} of ${target}`;
    }
    if (lines) {
      return `show ${side} ${lines} ${unit}`;
    }
    if (target) {
      return `show ${target}`;
    }
    return `show ${bin} output`;
  }

  if (bin === "cat") {
    const target = firstPositional(words, 1);
    return target ? `show ${target}` : "show output";
  }

  if (bin === "sed") {
    const expression = optionValue(words, ["-e", "--expression"]);
    const positional = positionalArgs(words, 1, ["-e", "--expression", "-f", "--file"]);
    const script = expression ?? positional[0];
    const target = expression ? positional[0] : positional[1];

    if (script) {
      const compact = (stripOuterQuotes(script) ?? script).replace(/\s+/g, "");
      const range = compact.match(/^([0-9]+),([0-9]+)p$/);
      if (range) {
        return target
          ? `print lines ${range[1]}-${range[2]} from ${target}`
          : `print lines ${range[1]}-${range[2]}`;
      }
      const single = compact.match(/^([0-9]+)p$/);
      if (single) {
        return target ? `print line ${single[1]} from ${target}` : `print line ${single[1]}`;
      }
    }

    return target ? `run sed on ${target}` : "run sed transform";
  }

  if (bin === "printf" || bin === "echo") {
    return "print text";
  }

  if (bin === "cp" || bin === "mv") {
    const positional = positionalArgs(words, 1, ["-t", "--target-directory", "-S", "--suffix"]);
    const src = positional[0];
    const dst = positional[1];
    const action = bin === "cp" ? "copy" : "move";
    if (src && dst) {
      return `${action} ${src} to ${dst}`;
    }
    if (src) {
      return `${action} ${src}`;
    }
    return `${action} files`;
  }

  if (bin === "rm") {
    const target = firstPositional(words, 1);
    return target ? `remove ${target}` : "remove files";
  }

  if (bin === "mkdir") {
    const target = firstPositional(words, 1);
    return target ? `create folder ${target}` : "create folder";
  }

  if (bin === "touch") {
    const target = firstPositional(words, 1);
    return target ? `create file ${target}` : "create file";
  }

  if (bin === "curl" || bin === "wget") {
    const url = words.find((token) => /^https?:\/\//i.test(token));
    return url ? `fetch ${url}` : "fetch url";
  }

  if (bin === "npm" || bin === "pnpm" || bin === "yarn" || bin === "bun") {
    const positional = positionalArgs(words, 1, ["--prefix", "-C", "--cwd", "--config"]);
    const sub = positional[0] ?? "command";
    const map: Record<string, string> = {
      install: "install dependencies",
      test: "run tests",
      build: "run build",
      start: "start app",
      lint: "run lint",
      run: positional[1] ? `run ${positional[1]}` : "run script",
    };
    return map[sub] ?? `run ${bin} ${sub}`;
  }

  if (bin === "node" || bin === "python" || bin === "python3" || bin === "ruby" || bin === "php") {
    const heredoc = words.slice(1).find((token) => token.startsWith("<<"));
    if (heredoc) {
      return `run ${bin} inline script (heredoc)`;
    }

    const inline =
      bin === "node"
        ? optionValue(words, ["-e", "--eval"])
        : bin === "python" || bin === "python3"
          ? optionValue(words, ["-c"])
          : undefined;
    if (inline !== undefined) {
      return `run ${bin} inline script`;
    }

    const nodeOptsWithValue = ["-e", "--eval", "-m"];
    const otherOptsWithValue = ["-c", "-e", "--eval", "-m"];
    const script = firstPositional(
      words,
      1,
      bin === "node" ? nodeOptsWithValue : otherOptsWithValue,
    );
    if (!script) {
      return `run ${bin}`;
    }

    if (bin === "node") {
      const mode =
        words.includes("--check") || words.includes("-c")
          ? "check js syntax for"
          : "run node script";
      return `${mode} ${script}`;
    }

    return `run ${bin} ${script}`;
  }

  if (bin === "deneb") {
    const sub = firstPositional(words, 1);
    return sub ? `run deneb ${sub}` : "run deneb";
  }

  const arg = firstPositional(words, 1);
  if (!arg || arg.length > 48) {
    return `run ${bin}`;
  }
  return /^[A-Za-z0-9._/-]+$/.test(arg) ? `run ${bin} ${arg}` : `run ${bin}`;
}

function summarizePipeline(stage: string): string {
  const pipeline = splitTopLevelPipes(stage);
  if (pipeline.length > 1) {
    const first = summarizeKnownExec(trimLeadingEnv(splitShellWords(pipeline[0])));
    const last = summarizeKnownExec(trimLeadingEnv(splitShellWords(pipeline[pipeline.length - 1])));
    const extra = pipeline.length > 2 ? ` (+${pipeline.length - 2} steps)` : "";
    return `${first} -> ${last}${extra}`;
  }
  return summarizeKnownExec(trimLeadingEnv(splitShellWords(stage)));
}

export type ExecSummary = {
  text: string;
  chdirPath?: string;
  allGeneric?: boolean;
};

export function summarizeExecCommand(command: string): ExecSummary | undefined {
  const { command: cleaned, chdirPath } = stripShellPreamble(command);
  if (!cleaned) {
    // All segments were preamble (e.g. `cd /tmp && cd /app`) — preserve chdirPath for context.
    return chdirPath ? { text: "", chdirPath } : undefined;
  }

  const stages = splitTopLevelStages(cleaned);
  if (stages.length === 0) {
    return undefined;
  }

  const summaries = stages.map((stage) => summarizePipeline(stage));
  const text = summaries.length === 1 ? summaries[0] : summaries.join(" → ");
  const allGeneric = summaries.every((s) => isGenericSummary(s));

  return { text, chdirPath, allGeneric };
}

/** Known summarizer prefixes that indicate a recognized command with useful context. */
const KNOWN_SUMMARY_PREFIXES = [
  "check git",
  "view git",
  "show git",
  "list git",
  "switch git",
  "create git",
  "pull git",
  "push git",
  "fetch git",
  "merge git",
  "rebase git",
  "stage git",
  "restore git",
  "reset git",
  "stash git",
  "search ",
  "find files",
  "list files",
  "show first",
  "show last",
  "print line",
  "print text",
  "copy ",
  "move ",
  "remove ",
  "create folder",
  "create file",
  "fetch http",
  "install dependencies",
  "run tests",
  "run build",
  "start app",
  "run lint",
  "run deneb",
  "run node script",
  "run node ",
  "run python",
  "run ruby",
  "run php",
  "run sed",
  "run git ",
  "run npm ",
  "run pnpm ",
  "run yarn ",
  "run bun ",
  "check js syntax",
];

/** True when the summary is generic and the raw command would be more informative. */
export function isGenericSummary(summary: string): boolean {
  if (summary === "run command") {
    return true;
  }
  // "run <binary>" or "run <binary> <arg>" without useful context
  if (summary.startsWith("run ")) {
    return !KNOWN_SUMMARY_PREFIXES.some((prefix) => summary.startsWith(prefix));
  }
  return false;
}

/** Compact the raw command for display: collapse whitespace, trim long strings. */
export function compactRawCommand(raw: string, maxLength = 120): string {
  const oneLine = raw
    .replace(/\s*\n\s*/g, " ")
    .replace(/\s{2,}/g, " ")
    .trim();
  if (oneLine.length <= maxLength) {
    return oneLine;
  }
  return `${oneLine.slice(0, Math.max(0, maxLength - 1))}…`;
}

/** Unwrap a shell wrapper (e.g. `bash -c '...'`) and return the inner command. */
export { unwrapShellWrapper };
