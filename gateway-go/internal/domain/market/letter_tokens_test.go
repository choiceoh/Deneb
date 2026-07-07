package market

import (
	"testing"
	"time"
)

func resetLetterTokens() {
	letterTokens.Lock()
	letterTokens.values = nil
	letterTokens.asOf = time.Time{}
	letterTokens.Unlock()
}

func TestSubstituteLetterTokens(t *testing.T) {
	resetLetterTokens()
	RecordLetterTokens(map[string]string{
		LetterTokenUSDKRW: "1,531",
		LetterTokenEURKRW: "1,751",
	})
	RecordLetterTokens(map[string]string{LetterTokenCopper: "13,786"}) // second collector merges

	in := "💱 환율 {{market:usd_krw}}원/$ (유로 {{market:eur_krw}}원) · 구리 ${{market:copper}}/t"
	want := "💱 환율 1,531원/$ (유로 1,751원) · 구리 $13,786/t"
	if got := SubstituteLetterTokens(in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSubstituteLetterTokensUnknownBecomesDash(t *testing.T) {
	resetLetterTokens()
	RecordLetterTokens(map[string]string{LetterTokenUSDKRW: "1,531"})
	// An unrecorded token (fetch failed, or a token the model invented) must
	// degrade to a visible placeholder, never leak template syntax.
	if got := SubstituteLetterTokens("구리 {{market:copper}}"); got != "구리 —" {
		t.Errorf("got %q, want 구리 —", got)
	}
}

func TestSubstituteLetterTokensExpired(t *testing.T) {
	resetLetterTokens()
	RecordLetterTokens(map[string]string{LetterTokenUSDKRW: "1,531"})
	letterTokens.Lock()
	letterTokens.asOf = time.Now().Add(-letterTokenTTL - time.Minute)
	letterTokens.Unlock()
	// Yesterday's rates must not leak into a letter whose fetch failed today.
	if got := SubstituteLetterTokens("{{market:usd_krw}}"); got != "—" {
		t.Errorf("got %q, want —", got)
	}
}

func TestSubstituteLetterTokensFastPath(t *testing.T) {
	resetLetterTokens()
	const s = "시세 토큰이 없는 평범한 본문"
	if got := SubstituteLetterTokens(s); got != s {
		t.Errorf("no-token text must pass through unchanged, got %q", got)
	}
}
