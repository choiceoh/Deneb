package hanja

// Hangul syllable-block decomposition constants (U+AC00 … U+D7A3).
const (
	hangulBase  = 0xAC00
	hangulCount = 19 * 21 * 28 // 11172 composed syllables
	jungCount   = 21           // medials (vowels)
	jongCount   = 28           // finals (incl. none)
)

// Choseong (initial consonant) indices we care about.
const (
	choN = 2  // ㄴ
	choR = 5  // ㄹ
	choO = 11 // ㅇ (null onset)
)

// applyDueum applies South Korean 두음법칙 to a *word-initial* Sino-Korean reading
// syllable: a ㄹ or ㄴ onset shifts toward ㄴ or ㅇ depending on the vowel —
//
//	旅 려→여, 例 례→예, 利 리→이, 良 량→양   (ㄹ + ㅑㅕㅖㅛㅠㅣ → ㅇ)
//	來 래→내, 老 로→노, 樓 루→누, 雷 뢰→뇌   (ㄹ + 그 밖의 모음   → ㄴ)
//	女 녀→여, 年 년→연, 尿 뇨→요, 匿 닉→익   (ㄴ + ㅑㅕㅛㅠㅣ     → ㅇ)
//
// Syllables with other onsets (報 보, 告 고) are returned unchanged. Call this ONLY
// for the first Hanja of a consecutive run — mid-run readings keep their canonical
// onset (男女 남녀, not 남여; 金利 금리, not 금이).
func applyDueum(s rune) rune {
	if s < hangulBase || s >= hangulBase+hangulCount {
		return s
	}
	idx := int(s - hangulBase)
	cho := idx / (jungCount * jongCount)
	jung := (idx / jongCount) % jungCount
	rest := idx % (jungCount * jongCount) // medial+final, preserved as-is

	var newCho int
	switch cho {
	case choR: // ㄹ → ㅇ before a y-glide/ㅣ vowel, else → ㄴ
		if rToNull(jung) {
			newCho = choO
		} else {
			newCho = choN
		}
	case choN: // ㄴ → ㅇ before a y-glide/ㅣ vowel, else unchanged
		if nToNull(jung) {
			newCho = choO
		} else {
			return s
		}
	default:
		return s
	}
	return rune(hangulBase + newCho*(jungCount*jongCount) + rest)
}

// rToNull reports the medials that send a ㄹ onset to ㅇ: ㅑ ㅕ ㅖ ㅛ ㅠ ㅣ.
func rToNull(jung int) bool {
	switch jung {
	case 2, 6, 7, 12, 17, 20:
		return true
	}
	return false
}

// nToNull reports the medials that send a ㄴ onset to ㅇ: ㅑ ㅕ ㅛ ㅠ ㅣ.
func nToNull(jung int) bool {
	switch jung {
	case 2, 6, 12, 17, 20:
		return true
	}
	return false
}
