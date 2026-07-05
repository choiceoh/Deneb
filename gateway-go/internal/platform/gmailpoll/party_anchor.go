// party_anchor.go builds the deterministic party-anchor block injected into
// mail-analysis prompts: who sent the mail, who received it, and which side
// each address is on ("우리 측" vs 외부), derived purely from headers plus an
// our-domains set. No LLM involved (결정적 포맷 도그마).
//
// Why: shadow evaluation on real inbox mail (2026-07-05, scripts/dev/
// mail-bench.py shadow --anchor) showed this block eliminates the analysis
// models' party-confusion failures — reading a counterparty's reply as our own
// outgoing mail, mislabeling our staff as the counterparty's, and inverting
// who owes whom the next action. The effect was model-independent: it fixed
// dsv4-nothink's fatal inversions AND the incumbent glm-5.2's perspective
// wobbles, so the anchor is wired regardless of which model serves analysis.
package gmailpoll

import (
	"net/mail"
	"os"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

// defaultOurDomain is the operator organization's mail domain. Single-operator
// deployment: override or extend with DENEB_MAIL_OUR_DOMAINS (comma-separated)
// when group affiliates use additional domains.
const defaultOurDomain = "topsolar.kr"

// anchorRules is the fixed reading rule appended to every anchor block. The
// wording is exactly what the shadow evaluation validated — keep it in sync
// with scripts/dev/mail-bench.py ANCHOR_RULES if either changes.
const anchorRules = "판독 규칙: '우리 측'은 아래에 우리 측으로 표시된 도메인의 조직이다. 분석은 항상 우리 측 관점에서 쓴다. " +
	"보낸사람이 외부면 이 메일은 상대의 발화이고, 보낸사람이 우리 측이면 우리가 보낸 메일의 사본이다. " +
	"누가 무엇을 요구했는지는 반드시 이 앵커의 소속과 일치시켜라."

// ourAnchorDomains returns the lowercase our-side domain set, from
// DENEB_MAIL_OUR_DOMAINS when set, else the built-in default.
func ourAnchorDomains() map[string]bool {
	raw := strings.TrimSpace(os.Getenv("DENEB_MAIL_OUR_DOMAINS"))
	if raw == "" {
		raw = defaultOurDomain
	}
	out := map[string]bool{}
	for _, d := range strings.Split(raw, ",") {
		if d = strings.ToLower(strings.TrimSpace(d)); d != "" {
			out[d] = true
		}
	}
	return out
}

// anchorParty is one address with a display name, as parsed from a header.
type anchorParty struct {
	name string
	addr string
}

// parseAnchorParties parses an address-list header tolerantly. Korean business
// mail routinely violates RFC 5322 (unquoted names with '/', bare Hangul), so
// when net/mail rejects the list we fall back to comma-splitting and pulling
// the address out of each fragment.
func parseAnchorParties(header string) []anchorParty {
	header = strings.TrimSpace(header)
	if header == "" {
		return nil
	}
	if list, err := mail.ParseAddressList(header); err == nil {
		out := make([]anchorParty, 0, len(list))
		for _, a := range list {
			out = append(out, anchorParty{name: strings.TrimSpace(a.Name), addr: strings.ToLower(a.Address)})
		}
		return out
	}
	var out []anchorParty
	for _, frag := range strings.Split(header, ",") {
		frag = strings.TrimSpace(frag)
		if frag == "" {
			continue
		}
		addr := strings.ToLower(extractAddrForAnchor(frag))
		name := frag
		if i := strings.IndexByte(frag, '<'); i >= 0 {
			name = strings.TrimSpace(frag[:i])
		} else if addr != "" {
			name = ""
		}
		if addr == "" && name == "" {
			continue
		}
		out = append(out, anchorParty{name: strings.Trim(name, `"`), addr: addr})
	}
	return out
}

// extractAddrForAnchor pulls a bare email address out of a header fragment.
func extractAddrForAnchor(s string) string {
	if i := strings.IndexByte(s, '<'); i >= 0 {
		if j := strings.IndexByte(s[i:], '>'); j > 0 {
			return strings.TrimSpace(s[i+1 : i+j])
		}
	}
	for _, tok := range strings.Fields(s) {
		if strings.Contains(tok, "@") {
			return strings.Trim(tok, "<>;,")
		}
	}
	return ""
}

// anchorSideLabel renders the side tag for one address.
func anchorSideLabel(addr string, ourDomains map[string]bool) string {
	dom := ""
	if i := strings.LastIndexByte(addr, '@'); i >= 0 {
		dom = addr[i+1:]
	}
	if dom == "" {
		return "외부(주소 불명)"
	}
	if ourDomains[dom] {
		return "우리 측(" + dom + ")"
	}
	return "외부(" + dom + ")"
}

// buildPartyAnchor renders the anchor block for one message, or "" when no
// address parses out of any header (nothing deterministic to anchor on).
func buildPartyAnchor(msg *gmail.MessageDetail, ourDomains map[string]bool) string {
	if msg == nil {
		return ""
	}
	var sb strings.Builder
	wrote := false
	writeParties := func(label, header string) {
		for _, p := range parseAnchorParties(header) {
			disp := p.name
			if disp == "" {
				disp = p.addr
			}
			sb.WriteString("- " + label + ": " + disp)
			if p.addr != "" {
				sb.WriteString(" <" + p.addr + ">")
			}
			sb.WriteString(" — " + anchorSideLabel(p.addr, ourDomains) + "\n")
			wrote = true
		}
	}
	writeParties("보낸사람", msg.From)
	writeParties("받는사람", msg.To)
	writeParties("참조", msg.CC)
	if !wrote {
		return ""
	}
	sb.WriteString("- " + anchorRules)
	return "## 당사자 앵커 (헤더 기반 결정적 조립)\n" + sb.String()
}
