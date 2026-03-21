import type { StreamFn } from "@mariozechner/pi-agent-core";
import { streamSimple } from "@mariozechner/pi-ai";

const HTML_ENTITY_RE = /&(?:amp|lt|gt|quot|apos|#39|#x[0-9a-f]+|#\d+);/i;

function decodeHtmlEntities(value: string): string {
  return value
    .replace(/&amp;/gi, "&")
    .replace(/&quot;/gi, '"')
    .replace(/&#39;/gi, "'")
    .replace(/&apos;/gi, "'")
    .replace(/&lt;/gi, "<")
    .replace(/&gt;/gi, ">")
    .replace(/&#x([0-9a-f]+);/gi, (_, hex) => String.fromCodePoint(Number.parseInt(hex, 16)))
    .replace(/&#(\d+);/gi, (_, dec) => String.fromCodePoint(Number.parseInt(dec, 10)));
}

export function decodeHtmlEntitiesInObject(obj: unknown): unknown {
  if (typeof obj === "string") {
    return HTML_ENTITY_RE.test(obj) ? decodeHtmlEntities(obj) : obj;
  }
  if (Array.isArray(obj)) {
    return obj.map(decodeHtmlEntitiesInObject);
  }
  if (obj && typeof obj === "object") {
    const result: Record<string, unknown> = {};
    for (const [key, val] of Object.entries(obj as Record<string, unknown>)) {
      result[key] = decodeHtmlEntitiesInObject(val);
    }
    return result;
  }
  return obj;
}

function decodeXaiToolCallArgumentsInMessage(message: unknown): void {
  if (!message || typeof message !== "object") {
    return;
  }
  const content = (message as { content?: unknown }).content;
  if (!Array.isArray(content)) {
    return;
  }
  for (const block of content) {
    if (!block || typeof block !== "object") {
      continue;
    }
    const typedBlock = block as { type?: unknown; arguments?: unknown };
    if (typedBlock.type !== "toolCall" || !typedBlock.arguments) {
      continue;
    }
    if (typeof typedBlock.arguments === "object") {
      typedBlock.arguments = decodeHtmlEntitiesInObject(typedBlock.arguments);
    }
  }
}

function wrapStreamDecodeXaiToolCallArguments(
  stream: ReturnType<typeof streamSimple>,
): ReturnType<typeof streamSimple> {
  const originalResult = stream.result.bind(stream);
  stream.result = async () => {
    const message = await originalResult();
    decodeXaiToolCallArgumentsInMessage(message);
    return message;
  };

  const originalAsyncIterator = stream[Symbol.asyncIterator].bind(stream);
  (stream as { [Symbol.asyncIterator]: typeof originalAsyncIterator })[Symbol.asyncIterator] =
    function () {
      const iterator = originalAsyncIterator();
      return {
        async next() {
          const result = await iterator.next();
          if (!result.done && result.value && typeof result.value === "object") {
            const event = result.value as { partial?: unknown; message?: unknown };
            decodeXaiToolCallArgumentsInMessage(event.partial);
            decodeXaiToolCallArgumentsInMessage(event.message);
          }
          return result;
        },
        async return(value?: unknown) {
          return iterator.return?.(value) ?? { done: true as const, value: undefined };
        },
        async throw(error?: unknown) {
          return iterator.throw?.(error) ?? { done: true as const, value: undefined };
        },
      };
    };
  return stream;
}

export function wrapStreamFnDecodeXaiToolCallArguments(baseFn: StreamFn): StreamFn {
  return (model, context, options) => {
    const maybeStream = baseFn(model, context, options);
    if (maybeStream && typeof maybeStream === "object" && "then" in maybeStream) {
      return Promise.resolve(maybeStream).then((stream) =>
        wrapStreamDecodeXaiToolCallArguments(stream),
      );
    }
    return wrapStreamDecodeXaiToolCallArguments(maybeStream);
  };
}
