package recall

import "testing"

// Fixtures mirror the 172-turn production replay that motivated this matcher:
// the true-positive shapes it must catch and the measured false-positive
// classes it must refuse.

const vaultBody = `Bitwarden 호환 셀프호스팅 금고. 접속은 Tailscale 네트워크로 제한.
MASTER 키는 운영자만 보관. 계정 astra7471@example.com 으로 로그인.`

const windBody = `## 사업 개요
거금도 풍력 42MW, 총사업비 336억. 미납 시 위약금 47.4억, 선급금 30.4억 반환.`

func neutralRarity(string) float64 { return 1 }

// The replay's true-positive shapes: an answer built from the page's figures
// or proper nouns, never naming the page.
func TestCiteContentMatchesRealUsageShapes(t *testing.T) {
	answer := "시나리오 트리로 정리하면, 42MW 기준 총 336억에서 위약금 47.4억을 상계하고…"
	if !citeContentMatches(answer, windBody, neutralRarity) {
		t.Error("figure-built answer must credit the page it was computed from")
	}
	credsAnswer := "계정은 astra7471@example.com 이고, 접속은 Tailscale로 제한돼 있어요."
	if !citeContentMatches(credsAnswer, vaultBody, neutralRarity) {
		t.Error("credentials answer must credit the vault page")
	}
}

// Measured false-positive class #1: round figures recur across unrelated
// documents — a finance answer full of round percentages must not credit an
// unrelated meeting page.
func TestCiteContentRefusesRoundNumbers(t *testing.T) {
	body := "계약 조건: 선급금 20%, 중도금 70%, 잔금 300억."
	answer := "중국 회사채 시장에서 발행사의 절대다수가 AA 이상이고, 상위 20%가 70%를 차지…"
	if citeContentMatches(answer, body, neutralRarity) {
		t.Error("round percentages/amounts must carry no attribution power")
	}
	for _, tok := range []string{"20%", "300억", "1,000만원"} {
		if !citeRoundNumber(tok) {
			t.Errorf("%q should classify as round", tok)
		}
	}
	for _, tok := range []string{"47.4억", "336억", "12.6억"} {
		if citeRoundNumber(tok) {
			t.Errorf("%q should NOT classify as round", tok)
		}
	}
}

// Measured false-positive class #2: a corpus-common token (a hostname the
// whole 시스템 category mentions) must not let one page claim an answer about
// another, even paired with a second common token.
func TestCiteContentRefusesCorpusCommonTokens(t *testing.T) {
	commonAware := func(tok string) float64 {
		if tok == "Tailscale" || tok == "Bitwarden" {
			return 0.1 // carried by much of the corpus
		}
		return 1
	}
	answer := "srv2의 vLLM 주소는 Tailscale 네트워크의 Bitwarden… 아니, 100.x 대역이에요."
	if citeContentMatches(answer, vaultBody, commonAware) {
		t.Error("two corpus-common tokens must not add up to a citation")
	}
}

// One matched token is never enough — the replay's single-hostname case.
func TestCiteContentRequiresTwoDistinctTokens(t *testing.T) {
	answer := "설정은 Tailscale 네트워크에서 확인하세요."
	if citeContentMatches(answer, vaultBody, neutralRarity) {
		t.Error("a single matched token credited a page")
	}
}

// Degraded inputs must stay silent — this runs post-delivery on every turn.
func TestCiteContentDegradesQuietly(t *testing.T) {
	if citeContentMatches("", windBody, neutralRarity) {
		t.Error("empty answer matched")
	}
	if citeContentMatches("답변", "", neutralRarity) {
		t.Error("empty body matched")
	}
	// nil rarity oracle = no corpus filter, matcher still functions.
	answer := "42MW에 336억 규모예요."
	if !citeContentMatches(answer, windBody, nil) {
		t.Error("nil rarity oracle must not disable the matcher")
	}
}
