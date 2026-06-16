package hanja

import (
	"strings"
	"testing"
)

func TestTable_LoadedAndSane(t *testing.T) {
	// The embedded Unihan table should load thousands of single-syllable readings.
	if len(readings) < 8000 {
		t.Fatalf("readings table too small: %d (embed/parse broken?)", len(readings))
	}
	// Spot-check a few canonical business-vocab readings.
	for han, want := range map[rune]rune{
		'報': '보', '告': '고', '書': '서', '契': '계', '約': '약', '見': '견', '積': '적',
		'稅': '세', '金': '금', '計': '계', '算': '산', '株': '주', '式': '식', '會': '회', '社': '사',
	} {
		if got := readings[han]; got != want {
			t.Errorf("readings[%q] = %q, want %q", han, got, want)
		}
	}
}

func TestApplyDueum(t *testing.T) {
	// Word-initial 두음법칙 cases (the reading as stored → its word-initial form).
	cases := map[rune]rune{
		'려': '여', '례': '예', '료': '요', '류': '유', '리': '이', '량': '양', // ㄹ + y/ㅣ → ㅇ
		'래': '내', '로': '노', '루': '누', '뢰': '뇌', '락': '낙', '름': '늠', // ㄹ + 기타 → ㄴ
		'녀': '여', '년': '연', '뇨': '요', '뉴': '유', '니': '이', // ㄴ + y/ㅣ → ㅇ
	}
	for in, want := range cases {
		if got := applyDueum(in); got != want {
			t.Errorf("applyDueum(%q) = %q, want %q", in, got, want)
		}
	}
	// Onsets/vowels not subject to 두음 stay put.
	for _, r := range []rune{'보', '고', '서', '남', '노', '나', '무', '강'} {
		if got := applyDueum(r); got != r {
			t.Errorf("applyDueum(%q) = %q, want unchanged", r, got)
		}
	}
}

func TestTransliterate_Words(t *testing.T) {
	cases := []struct{ in, want string }{
		{"報告書 검토 부탁드립니다.", "보고서 검토 부탁드립니다."},
		{"見積書와 契約書를 보냅니다.", "견적서와 계약서를 보냅니다."},
		{"稅金計算書 첨부", "세금계산서 첨부"},
		{"株式會社 데네브", "주식회사 데네브"},
		// 두음법칙: run-initial gets 두음, mid-run keeps canonical.
		{"旅行 일정", "여행 일정"}, // 旅 려→여 (initial), 行 행 (mid)
		{"料金 안내", "요금 안내"}, // 料 료→요
		{"利用 방법", "이용 방법"}, // 利 리→이
		{"男女 구분", "남녀 구분"}, // 女 녀 stays (mid-run), 男 남
		{"金利 인상", "금리 인상"}, // 利 리 stays (mid-run, 두음 X)
		{"來年 계획", "내년 계획"}, // 來 래→내 (initial), 年 년 (mid)
		// No Han → untouched (and identity-fast-pathed).
		{"순수 한글 문장입니다.", "순수 한글 문장입니다."},
		{"", ""},
	}
	for _, c := range cases {
		if got := Transliterate(c.in); got != c.want {
			t.Errorf("Transliterate(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTransliterate_PreservesCode(t *testing.T) {
	// Han inside a fenced block stays as-is; prose outside is converted.
	in := "報告:\n```go\nx := \"報告\" // 中\n```\n結論은 報告 완료."
	got := Transliterate(in)
	if !strings.Contains(got, "```go\nx := \"報告\" // 中\n```") {
		t.Errorf("fenced Han must be preserved, got:\n%s", got)
	}
	if !strings.HasPrefix(got, "보고:") {
		t.Errorf("prose before fence must convert, got:\n%s", got)
	}
	if !strings.HasSuffix(got, "결론은 보고 완료.") {
		t.Errorf("prose after fence must convert, got:\n%s", got)
	}
	// Inline code preserved.
	if got := Transliterate("값 `報告` 참고"); got != "값 `報告` 참고" {
		t.Errorf("inline code must be preserved, got %q", got)
	}
}

func TestStreamer_MatchesWholeString(t *testing.T) {
	// Streaming any chunking of the input must equal the whole-string transform —
	// the property the live (delta) path and the final-text path both rely on.
	inputs := []string{
		"報告書 검토 後 契約 진행. 旅行 일정도 料金 포함.",
		"코드:\n```\n報告 = 1\n```\n結果 報告 완료, 利用 가능.",
		"백틱 경계 `報告` 와 ```\n中\n``` 혼합 報告.",
		// Simplified (时间→시간) + a dict connector (所以) split across deltas.
		"时间 분석 끝. 없어.所以 報告書 송부, 即时发生 확인.",
	}
	chunkings := [][]int{{1}, {2}, {3}, {5}, {7}} // split every N runes
	for _, in := range inputs {
		want := Transliterate(in)
		runes := []rune(in)
		for _, step := range chunkings {
			n := step[0]
			st := NewStreamer()
			var b strings.Builder
			for i := 0; i < len(runes); i += n {
				end := min(i+n, len(runes))
				b.WriteString(st.Write(string(runes[i:end])))
			}
			b.WriteString(st.Flush())
			if got := b.String(); got != want {
				t.Errorf("streamed (step=%d) = %q, want %q (in=%q)", n, got, want, in)
			}
		}
	}
}

func TestTransliterate_SimplifiedAndChinese(t *testing.T) {
	cases := []struct{ in, want string }{
		// (1) Simplified Chinese reads via kTraditionalVariant: 时→時→시 etc., so
		// Chinese Sino-vocabulary surfaces as the equivalent Korean word.
		{"时间 발생", "시간 발생"},
		{"发生 보고", "발생 보고"},
		{"问题 해결", "문제 해결"},
		{"经济 상황", "경제 상황"},
		{"即时发生", "즉시발생"}, // a Sino-compound reads as Korean (즉시 발생)
		// (2) Pure-Chinese connectors (gibberish reading) → Korean via the dict.
		{"없어.所以 진행", "없어.그래서 진행"},
		{"但是 문제가 있다", "하지만 문제가 있다"},
		{"这个 항목", "이것 항목"},
		// Valid Korean-Hanja words NOT in the dict fall through to their reading.
		{"不過 3개월", "불과 3개월"},
		{"目前 상황", "목전 상황"},
		{"然後 진행", "연후 진행"},
		// (3) All-or-nothing guard: a run with a no-reading char (U+20000) stays
		// whole rather than half-converting.
		{"報\U00020000告", "報\U00020000告"},
	}
	for _, c := range cases {
		if got := Transliterate(c.in); got != c.want {
			t.Errorf("Transliterate(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStreamer_SplitFenceMarker(t *testing.T) {
	// A ``` fence marker split across deltas must still toggle the block, so the
	// Han inside stays verbatim.
	st := NewStreamer()
	var b strings.Builder
	for _, d := range []string{"報告 ", "`", "`", "`", "\n報告\n", "`", "``", "\n結論"} {
		b.WriteString(st.Write(d))
	}
	b.WriteString(st.Flush())
	got := b.String()
	if !strings.Contains(got, "```\n報告\n```") {
		t.Errorf("split fence must preserve inner Han, got: %q", got)
	}
	if !strings.HasPrefix(got, "보고 ") || !strings.HasSuffix(got, "결론") {
		t.Errorf("prose around split fence must convert, got: %q", got)
	}
}
