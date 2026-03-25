import { MEDIA_AUDIO_FIELD_HELP } from "./media-audio-field-metadata.js";
import { AGENTS_HELP } from "./schema-help/agents.js";
import { CHANNELS_MESSAGES_HELP } from "./schema-help/channels-messages.js";
import { GATEWAY_HELP } from "./schema-help/gateway.js";
import { HOOKS_HELP } from "./schema-help/hooks.js";
import { MEMORY_HELP } from "./schema-help/memory.js";
import { MISC_HELP } from "./schema-help/misc.js";
import { MODELS_AUTH_HELP } from "./schema-help/models-auth.js";
import { PLUGINS_HELP } from "./schema-help/plugins.js";
import { SESSION_HELP } from "./schema-help/session.js";
import { TOOLS_HELP } from "./schema-help/tools.js";

export const FIELD_HELP: Record<string, string> = {
  ...MISC_HELP,
  ...GATEWAY_HELP,
  ...AGENTS_HELP,
  ...TOOLS_HELP,
  ...MODELS_AUTH_HELP,
  ...MEMORY_HELP,
  ...PLUGINS_HELP,
  ...SESSION_HELP,
  ...HOOKS_HELP,
  ...CHANNELS_MESSAGES_HELP,
  ...MEDIA_AUDIO_FIELD_HELP,
};
