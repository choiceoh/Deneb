// TTS removed — stub file retained for plugin-sdk export compatibility.
export type SpeechVoiceOption = { id: string; name: string };
export function parseTtsDirectives(_text: string): { text: string; directives: Record<string, string> } {
  return { text: _text, directives: {} };
}
