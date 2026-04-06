"""
Korean Quality Environment for Atropos.

Scores rollout responses on Korean language quality metrics:
1. Korean character ratio (Hangul percentage)
2. Response substance (length, information density)
3. Cleanliness (no internal token leaks, no AI filler phrases)
4. Tool call success rate (from metadata)

This is a proper Atropos environment plugin that participates in the
training loop. Rewards are used to compute advantages for IS loss.

Usage:
    atropos --env korean_quality --env-module scripts.rl.korean_quality_env
"""


class KoreanQualityEnvironment:
    """Atropos environment that scores Korean response quality."""

    name = "korean_quality"

    # Token leak patterns that should never appear in user-facing output.
    LEAK_PATTERNS = [
        "<function=", "<thinking>", "NO_REPLY", "<|",
        '```json\n{"', "</s>", "<|im_end|>", "<|endoftext|>",
    ]

    # AI filler phrases that reduce response quality.
    FILLER_PREFIXES = [
        "좋은 질문", "물론이죠", "네, 물론", "Sure!", "Of course",
        "Great question", "That's a great", "Absolutely!",
    ]

    def score(self, prompt: str, response: str, metadata: dict | None = None) -> float:
        """Score a response on Korean quality (0.0-1.0).

        The score is a weighted combination of four components:
        - Korean ratio (25%): Hangul character percentage
        - Substance (25%): Response length and content density
        - Cleanliness (20%): No token leaks or filler
        - Tool success (30%): Tool call outcomes from metadata

        When no tools are used, the denominator adjusts so text-only
        responses are scored fairly against responses with tools.
        """
        if not response or not response.strip():
            return 0.0

        score = 0.0
        total = 0.0

        # 1. Korean ratio (25 points).
        korean_ratio = self._compute_korean_ratio(response)
        total += 25.0
        # Linear scaling: full points at 50%+ Korean.
        score += 25.0 * min(1.0, korean_ratio / 0.5)

        # 2. Substance (25 points).
        char_count = len(response.strip())
        total += 25.0
        # Linear scaling: full points at 200+ chars.
        score += 25.0 * min(1.0, char_count / 200)

        # 3. Cleanliness (20 points).
        total += 20.0
        penalty = 0.0
        for pattern in self.LEAK_PATTERNS:
            if pattern in response:
                penalty += 10.0
        stripped = response.lstrip()
        for prefix in self.FILLER_PREFIXES:
            if stripped.startswith(prefix):
                penalty += 5.0
        score += max(0.0, 20.0 - penalty)

        # 4. Tool success (30 points, only when tools were used).
        tool_calls = (metadata or {}).get("tool_calls", [])
        if tool_calls:
            total += 30.0
            successes = sum(1 for t in tool_calls if t.get("success", False))
            score += 30.0 * (successes / len(tool_calls))

        return score / total if total > 0 else 0.0

    @staticmethod
    def _compute_korean_ratio(text: str) -> float:
        """Return the fraction of Korean (Hangul) characters in text."""
        total = 0
        korean = 0
        for ch in text:
            if ch.isalpha() or ch.isdigit():
                total += 1
                cp = ord(ch)
                # Hangul Syllables + Compatibility Jamo + Jamo.
                if (0xAC00 <= cp <= 0xD7A3 or
                        0x3131 <= cp <= 0x318E or
                        0x1100 <= cp <= 0x11FF):
                    korean += 1
        return korean / total if total > 0 else 0.0


# Module-level instance for Atropos discovery.
environment = KoreanQualityEnvironment()
