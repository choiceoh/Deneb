// adblock-probe.mjs — offline unit probe for the sidecar's ad-host matcher.
//
// The blocklist is the one place a typo turns into either a silent hole (an ad
// network keeps loading) or, worse, a blocked first-party site the operator
// needs. Substring matching would do the latter constantly — "myads.co.kr" and
// "doubleclick.net.phishing.example" must NOT match — so the matcher is label
// exact, and these cases pin that.
//
//   cd scripts/browser && npm run probe:adblock
// Exit 0 = matcher behaves · 1 = a case regressed. No network, no browser.
import { isAdHost } from "./server.mjs";

const CASES = [
  // Blocked: the host itself and any subdomain of it.
  ["doubleclick.net", true],
  ["securepubads.g.doubleclick.net", true],
  ["pagead2.googlesyndication.com", true],
  ["static.criteo.net", true],
  ["yellow.contentsfeed.com", true],
  ["ad.daum.net", true],
  ["DOUBLECLICK.NET", true], // case-insensitive
  ["doubleclick.net.", true], // trailing root dot
  // Not blocked: first-party sites that merely contain an ad-ish substring, and
  // lookalike hosts that only END with the label as a prefix of another label.
  ["myads.co.kr", false],
  ["notdoubleclick.net", false],
  ["doubleclick.net.evil.example", false],
  ["adop.cc.example.com", false],
  ["news.naver.com", false],
  ["ko.wikipedia.org", false],
  ["", false],
];

let failed = 0;
for (const [host, want] of CASES) {
  const got = isAdHost(host);
  if (got !== want) {
    failed++;
    console.log(`[FAIL] isAdHost(${JSON.stringify(host)}) = ${got}, want ${want}`);
  }
}
console.log(`PROBE_RESULT passed=${CASES.length - failed}/${CASES.length}`);
process.exit(failed > 0 ? 1 : 0);
