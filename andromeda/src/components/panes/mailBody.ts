import type { Mail } from "@/types";
import { firstString } from "@/format";
import { stripMailChrome } from "@/mailChrome";

// The displayable mail body, falling back through the gateway's field aliases and
// finally a stripped HTML part. Exported so the pane can project it to the AI.
export function mailBody(mail?: Mail): string {
  if (!mail) return "";
  const body = firstString(mail, ["body", "plain", "plainText", "bodyText", "text", "message", "content"]);
  if (body) return stripMailChrome(body);
  if (mail.snippet) return mail.snippet;
  const html = firstString(mail, ["html"]);
  return html ? stripMailChrome(htmlToText(html)) : "";
}

function htmlToText(html: string): string {
  if (typeof DOMParser !== "undefined") {
    return new DOMParser().parseFromString(html, "text/html").body.textContent?.trim() ?? "";
  }
  return html
    .replace(/<[^>]*>/g, " ")
    .replace(/\s+/g, " ")
    .trim();
}
