import type { ImageGenerationProvider } from "../image-generation/types.js";
import type { MediaUnderstandingProvider } from "../media-understanding/types.js";
import type {
  SpeechProviderConfiguredContext,
  SpeechListVoicesRequest,
  SpeechProviderId,
  SpeechSynthesisRequest,
  SpeechSynthesisResult,
  SpeechTelephonySynthesisRequest,
  SpeechTelephonySynthesisResult,
  SpeechVoiceOption,
} from "../tts/provider-types.js";

export type SpeechProviderPlugin = {
  id: SpeechProviderId;
  label: string;
  aliases?: string[];
  models?: readonly string[];
  voices?: readonly string[];
  isConfigured: (ctx: SpeechProviderConfiguredContext) => boolean;
  synthesize: (req: SpeechSynthesisRequest) => Promise<SpeechSynthesisResult>;
  synthesizeTelephony?: (
    req: SpeechTelephonySynthesisRequest,
  ) => Promise<SpeechTelephonySynthesisResult>;
  listVoices?: (req: SpeechListVoicesRequest) => Promise<SpeechVoiceOption[]>;
};

export type PluginSpeechProviderEntry = SpeechProviderPlugin & {
  pluginId: string;
};

export type MediaUnderstandingProviderPlugin = MediaUnderstandingProvider;
export type ImageGenerationProviderPlugin = ImageGenerationProvider;
