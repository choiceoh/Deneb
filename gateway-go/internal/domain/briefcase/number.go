package briefcase

import (
	"math/big"
	"strings"
)

const (
	// V1 bounds prevent a short exponent from expanding into megabytes of
	// bigint state during grading or feedback leak detection.
	MaxRationalDigits      = 256
	MaxRationalExponentAbs = 4096
	MaxRationalTextBytes   = 1024
)

// ParseBoundedRational accepts a decimal/scientific number, an integer
// fraction, or either form followed by %. It rejects inputs whose mantissa or
// exponent would exceed the deterministic v1 resource bounds.
func ParseBoundedRational(text string) (*big.Rat, bool) {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > MaxRationalTextBytes {
		return nil, false
	}
	percent := strings.HasSuffix(text, "%")
	if percent {
		text = strings.TrimSpace(strings.TrimSuffix(text, "%"))
	}
	var value *big.Rat
	var ok bool
	if strings.Contains(text, "/") {
		value, ok = parseBoundedFraction(text)
	} else {
		value, ok = parseBoundedDecimal(text)
	}
	if !ok {
		return nil, false
	}
	if percent {
		value.Quo(value, big.NewRat(100, 1))
	}
	return value, true
}

func BoundedRationalKey(text string) (string, bool) {
	value, ok := ParseBoundedRational(text)
	if !ok {
		return "", false
	}
	return value.RatString(), true
}

func parseBoundedFraction(text string) (*big.Rat, bool) {
	if strings.Count(text, "/") != 1 {
		return nil, false
	}
	parts := strings.SplitN(text, "/", 2)
	numerator, ok := parseBoundedInteger(strings.TrimSpace(parts[0]))
	if !ok {
		return nil, false
	}
	denominator, ok := parseBoundedInteger(strings.TrimSpace(parts[1]))
	if !ok || denominator.Sign() == 0 {
		return nil, false
	}
	return new(big.Rat).SetFrac(numerator, denominator), true
}

func parseBoundedInteger(text string) (*big.Int, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, false
	}
	raw := text
	digits := text
	if digits[0] == '+' || digits[0] == '-' {
		digits = digits[1:]
	}
	if digits == "" || len(digits) > MaxRationalDigits {
		return nil, false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return nil, false
		}
	}
	value, ok := new(big.Int).SetString(raw, 10)
	return value, ok
}

func parseBoundedDecimal(text string) (*big.Rat, bool) {
	if text == "" {
		return nil, false
	}
	mantissa := text
	exponent := ""
	if index := strings.IndexAny(mantissa, "eE"); index >= 0 {
		if strings.ContainsAny(mantissa[index+1:], "eE") {
			return nil, false
		}
		exponent = mantissa[index+1:]
		mantissa = mantissa[:index]
		if !boundedExponent(exponent) {
			return nil, false
		}
	}
	if mantissa == "" {
		return nil, false
	}
	if mantissa[0] == '+' || mantissa[0] == '-' {
		mantissa = mantissa[1:]
	}
	if mantissa == "" || strings.Count(mantissa, ".") > 1 {
		return nil, false
	}
	digits := 0
	for _, r := range mantissa {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r == '.':
		default:
			return nil, false
		}
	}
	if digits == 0 || digits > MaxRationalDigits {
		return nil, false
	}
	value, ok := new(big.Rat).SetString(text)
	return value, ok
}

func boundedExponent(text string) bool {
	if text == "" {
		return false
	}
	sign := 1
	if text[0] == '+' || text[0] == '-' {
		if text[0] == '-' {
			sign = -1
		}
		text = text[1:]
	}
	if text == "" || len(text) > 5 {
		return false
	}
	value := 0
	for _, r := range text {
		if r < '0' || r > '9' {
			return false
		}
		value = value*10 + int(r-'0')
		if value > MaxRationalExponentAbs {
			return false
		}
	}
	value *= sign
	return value >= -MaxRationalExponentAbs && value <= MaxRationalExponentAbs
}
