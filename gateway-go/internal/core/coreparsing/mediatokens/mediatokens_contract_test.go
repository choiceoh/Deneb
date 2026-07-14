package mediatokens

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseFastPathsAndTrailingWhitespace(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want Result
	}{
		{name: "empty", in: "", want: Result{}},
		{name: "whitespace", in: " \t\r\n ", want: Result{}},
		{name: "plain", in: "plain text  \n\t", want: Result{Text: "plain text"}},
		{name: "brackets without directive", in: "single [ bracket", want: Result{Text: "single [ bracket"}},
		{name: "media word without colon", in: "MEDIA is useful", want: Result{Text: "MEDIA is useful"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Parse(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Parse(%q) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseMediaPrefixIsCaseInsensitive(t *testing.T) {
	for _, prefix := range []string{"MEDIA:", "media:", "Media:", "mEdIa:"} {
		got := Parse(prefix + " https://example.com/image.png")
		if len(got.MediaURLs) != 1 || got.MediaURLs[0] != "https://example.com/image.png" || got.Text != "" {
			t.Errorf("prefix %q = %#v", prefix, got)
		}
	}
}

func TestParseMediaSourceKindsAndWrapping(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
		want []string
	}{
		{name: "http", line: "MEDIA: http://example.com/a.jpg", want: []string{"http://example.com/a.jpg"}},
		{name: "uppercase https", line: "MEDIA: HTTPS://EXAMPLE.COM/A.JPG", want: []string{"HTTPS://EXAMPLE.COM/A.JPG"}},
		{name: "absolute unix", line: "MEDIA: /tmp/a.png", want: []string{"/tmp/a.png"}},
		{name: "relative dot", line: "MEDIA: ./a.png", want: []string{"./a.png"}},
		{name: "relative parent", line: "MEDIA: ../a.png", want: []string{"../a.png"}},
		{name: "home", line: "MEDIA: ~/a.png", want: []string{"~/a.png"}},
		{name: "windows backslash", line: `MEDIA: C:\temp\a.png`, want: []string{`C:\temp\a.png`}},
		{name: "windows slash", line: "MEDIA: D:/temp/a.png", want: []string{"D:/temp/a.png"}},
		{name: "unc", line: `MEDIA: \\server\share\a.png`, want: []string{`\\server\share\a.png`}},
		{name: "bare filename", line: "MEDIA: result.webp", want: []string{"result.webp"}},
		{name: "backticks", line: "MEDIA: `https://example.com/a.png`", want: []string{"https://example.com/a.png"}},
		{name: "brackets", line: "MEDIA: [https://example.com/a.png],", want: []string{"https://example.com/a.png"}},
		{name: "single quote path", line: `MEDIA: '/tmp/my audio.mp3'`, want: []string{"/tmp/my audio.mp3"}},
		{name: "uppercase file URI", line: "MEDIA: FILE:///tmp/a.png", want: []string{"/tmp/a.png"}},
		{name: "multiple", line: "MEDIA: /tmp/a.png https://example.com/b.jpg result.gif", want: []string{"/tmp/a.png", "https://example.com/b.jpg", "result.gif"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Parse(tc.line)
			if !reflect.DeepEqual(got.MediaURLs, tc.want) || got.Text != "" {
				t.Fatalf("Parse(%q) = %#v, want URLs %#v", tc.line, got, tc.want)
			}
		})
	}
}

func TestParseRejectsInvalidMediaButHandlesLikelyLocalPaths(t *testing.T) {
	tooLong := "/" + strings.Repeat("a", 4096)
	for _, tc := range []struct {
		name      string
		line      string
		wantText  string
		wantURLs  []string
		lineDrops bool
	}{
		{name: "empty payload kept", line: "MEDIA:   ", wantText: "MEDIA:"},
		{name: "ordinary prose kept", line: "MEDIA: not a media source", wantText: "MEDIA: not a media source"},
		{name: "invalid quoted URL with spaces kept", line: `MEDIA: "https://example.com/not valid.png"`, wantText: `MEDIA: "https://example.com/not valid.png"`},
		{name: "likely missing local path dropped", line: "MEDIA: /", lineDrops: true},
		{name: "too long local path dropped", line: "MEDIA: " + tooLong, lineDrops: true},
		{name: "bare without extension kept", line: "MEDIA: output", wantText: "MEDIA: output"},
		{name: "extension too long kept", line: "MEDIA: output.abcdefghijk", wantText: "MEDIA: output.abcdefghijk"},
		{name: "directory filename kept", line: "MEDIA: dir/output.png", wantText: "MEDIA: dir/output.png"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Parse(tc.line)
			if !reflect.DeepEqual(got.MediaURLs, tc.wantURLs) {
				t.Fatalf("URLs = %#v, want %#v", got.MediaURLs, tc.wantURLs)
			}
			if tc.lineDrops {
				if got.Text != "" {
					t.Fatalf("likely local line was retained: %q", got.Text)
				}
			} else if got.Text != tc.wantText {
				t.Fatalf("Text = %q, want %q", got.Text, tc.wantText)
			}
		})
	}
}

func TestParseFenceBoundariesAndOffsets(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want []string
		kept string
	}{
		{
			name: "backtick language fence",
			in:   "before\n```text\nMEDIA: /inside.png\n```\nMEDIA: /outside.png",
			want: []string{"/outside.png"}, kept: "MEDIA: /inside.png",
		},
		{
			name: "tilde fence",
			in:   "~~~\nMEDIA: /inside.png\n~~~\nMEDIA: /outside.png",
			want: []string{"/outside.png"}, kept: "MEDIA: /inside.png",
		},
		{
			name: "unclosed fence",
			in:   "```\nMEDIA: /inside.png",
			want: nil, kept: "MEDIA: /inside.png",
		},
		{
			name: "media before fence",
			in:   "MEDIA: /first.png\n```\nMEDIA: /inside.png\n```",
			want: []string{"/first.png"}, kept: "MEDIA: /inside.png",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Parse(tc.in)
			if !reflect.DeepEqual(got.MediaURLs, tc.want) || !strings.Contains(got.Text, tc.kept) {
				t.Fatalf("Parse = %#v", got)
			}
		})
	}
}

func TestStripInlineDirectivesParsesAudioAndVoiceKeys(t *testing.T) {
	for _, tc := range []struct {
		name      string
		in        string
		wantText  string
		wantKeys  []string
		wantVoice bool
	}{
		{name: "none", in: "plain", wantText: "plain"},
		{name: "audio", in: "before [[audio_as_voice]] after", wantText: "before  after", wantKeys: []string{"audio_as_voice"}, wantVoice: true},
		{name: "voice alias", in: "[[voice]]hello", wantText: "hello", wantKeys: []string{"voice"}, wantVoice: true},
		{name: "key value", in: "a[[format=wav]]b", wantText: "ab", wantKeys: []string{"format"}},
		{name: "trimmed key", in: "a[[ voice = yes ]]b", wantText: "ab", wantKeys: []string{"voice"}, wantVoice: true},
		{name: "unknown removed", in: "a[[unknown]]b", wantText: "ab", wantKeys: []string{"unknown"}},
		{name: "empty directive", in: "a[[]]b", wantText: "ab", wantKeys: []string{""}},
		{name: "unclosed kept", in: "a[[voice", wantText: "a[[voice"},
		{name: "multiple", in: "[[one]]x[[audio_as_voice]][[two=v]]", wantText: "x", wantKeys: []string{"one", "audio_as_voice", "two"}, wantVoice: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleaned, keys := stripInlineDirectives(tc.in)
			if cleaned != tc.wantText || !reflect.DeepEqual(keys, tc.wantKeys) {
				t.Fatalf("stripInlineDirectives = %q,%#v; want %q,%#v", cleaned, keys, tc.wantText, tc.wantKeys)
			}
			voiceText, voice := stripAudioTag(tc.in)
			if voiceText != tc.wantText || voice != tc.wantVoice {
				t.Fatalf("stripAudioTag = %q,%v; want %q,%v", voiceText, voice, tc.wantText, tc.wantVoice)
			}
		})
	}
}

func TestCollapseWhitespaceContract(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{in: "", want: ""},
		{in: "a  \t b", want: "a b"},
		{in: "a   \n", want: "a\n"},
		{in: "a\n\n\n b", want: "a\n b"},
		{in: "  leading", want: " leading"},
		{in: "trailing   ", want: "trailing"},
		{in: "한글\t\t공백", want: "한글 공백"},
		{in: "a\r\nb", want: "a\r\nb"},
	} {
		if got := collapseWhitespace(tc.in); got != tc.want {
			t.Errorf("collapseWhitespace(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMediaValidationHelpersClassifyValidAndInvalidCandidates(t *testing.T) {
	for _, tc := range []struct {
		candidate   string
		valid       bool
		allowSpaces bool
		bare        bool
		local       bool
	}{
		{candidate: "https://example.com/a.png", valid: true, allowSpaces: true},
		{candidate: "HTTPS://example.com/a.png", valid: true, allowSpaces: true},
		{candidate: "https://example.com/a b.png", valid: false, allowSpaces: false},
		{candidate: "/tmp/a b.png", valid: false, allowSpaces: true, local: true},
		{candidate: "result.png", valid: true, bare: true},
		{candidate: "result.", valid: false},
		{candidate: ".png", valid: false},
		{candidate: "result.abcdefghijk", valid: false},
		{candidate: "dir/result.png", valid: false},
		{candidate: "file:///tmp/a.png", local: true},
		{candidate: `\\server\share`, valid: true, allowSpaces: true, local: true},
		{candidate: "", valid: false},
	} {
		if got := isValidMedia(tc.candidate); got != tc.valid {
			t.Errorf("isValidMedia(%q) = %v, want %v", tc.candidate, got, tc.valid)
		}
		if got := isValidMediaAllowSpaces(tc.candidate); got != tc.allowSpaces {
			t.Errorf("isValidMediaAllowSpaces(%q) = %v, want %v", tc.candidate, got, tc.allowSpaces)
		}
		if got := isBareFilename(tc.candidate); got != tc.bare {
			t.Errorf("isBareFilename(%q) = %v, want %v", tc.candidate, got, tc.bare)
		}
		if got := isLikelyLocalPath(tc.candidate); got != tc.local {
			t.Errorf("isLikelyLocalPath(%q) = %v, want %v", tc.candidate, got, tc.local)
		}
	}
}

func TestCleanCandidateUnwrapsQuotesAndNormalizesSource(t *testing.T) {
	for _, tc := range []struct {
		in      string
		cleaned string
		quoted  string
		ok      bool
		norm    string
	}{
		{in: `"/tmp/a b.png"`, cleaned: "/tmp/a b.png", quoted: "/tmp/a b.png", ok: true, norm: `"/tmp/a b.png"`},
		{in: "'/tmp/a.png'", cleaned: "/tmp/a.png", quoted: "/tmp/a.png", ok: true, norm: "'/tmp/a.png'"},
		{in: "`[https://x/a.png],`", cleaned: "https://x/a.png", norm: "`[https://x/a.png],`"},
		{in: " file:///tmp/a.png ", cleaned: " file:///tmp/a.png ", norm: " file:///tmp/a.png "},
		{in: "FILE:///tmp/a.png", cleaned: "FILE:///tmp/a.png", norm: "/tmp/a.png"},
		{in: "plain", cleaned: "plain", norm: "plain"},
	} {
		if got := cleanCandidate(tc.in); got != tc.cleaned {
			t.Errorf("cleanCandidate(%q) = %q, want %q", tc.in, got, tc.cleaned)
		}
		quoted, ok := tryUnwrapQuoted(tc.in)
		if quoted != tc.quoted || ok != tc.ok {
			t.Errorf("tryUnwrapQuoted(%q) = %q,%v; want %q,%v", tc.in, quoted, ok, tc.quoted, tc.ok)
		}
		if got := normalizeMediaSource(tc.in); got != tc.norm {
			t.Errorf("normalizeMediaSource(%q) = %q, want %q", tc.in, got, tc.norm)
		}
	}
}

func TestContainsIgnoreCaseASCIIAndFenceLookup(t *testing.T) {
	for _, tc := range []struct {
		s, sub string
		want   bool
	}{
		{"MEDIA:", "media:", true},
		{"prefix Media: suffix", "MEDIA:", true},
		{"short", "longer", false},
		{"한글MEDIA:", "media:", true},
		{"nothing", "media:", false},
		{"anything", "", true},
	} {
		if got := containsIgnoreCase(tc.s, tc.sub); got != tc.want {
			t.Errorf("containsIgnoreCase(%q,%q) = %v, want %v", tc.s, tc.sub, got, tc.want)
		}
	}
	if !isASCIIAlpha('a') || !isASCIIAlpha('Z') || isASCIIAlpha('1') || isASCIIAlpha(0xff) {
		t.Fatal("isASCIIAlpha boundary failure")
	}
}
