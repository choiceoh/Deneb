"""LocalExpander -- query expansion via Qwen3.5-9B."""

import logging

from .manager import ModelManager

_log = logging.getLogger(__name__)


# ──────────────────────────────────────────────
# 4. LocalExpander
# ──────────────────────────────────────────────

class LocalExpander:
    """쿼리 확장 (Qwen3.5-9B) — 동의어/관련 키워드 생성."""

    _PROMPT_TEMPLATE = (
        '<|im_start|>system\n'
        '당신은 검색 쿼리 확장 전문가입니다. '
        '주어진 검색어에 대해 관련 동의어, 유의어, 관련 키워드를 쉼표로 구분하여 나열하세요. '
        '한국어와 영어를 모두 포함하세요. 설명 없이 키워드만 출력하세요.'
        '<|im_end|>\n'
        '<|im_start|>user\n검색어: {query}<|im_end|>\n'
        '<|im_start|>assistant\n'
    )

    def __init__(self, manager=None):
        self.mgr = manager or ModelManager()

    def expand(self, query):
        """검색어 확장 키워드 리스트 반환.

        실패 시 빈 리스트 반환.
        """
        if not query or not str(query).strip():
            return []

        model = self.mgr.get_model('expander')
        if model is None:
            return []

        try:
            # 쿼리 길이 제한 (프롬프트 오버플로우 방지)
            safe_query = str(query).strip()[:500]
            prompt = self._PROMPT_TEMPLATE.format(query=safe_query)
            output = model(
                prompt,
                max_tokens=64,
                temperature=0.3,
                stop=['<|im_end|>', '\n\n'],
            )
            text = output['choices'][0]['text'].strip()
            return self._parse_keywords(text, safe_query)
        except Exception as e:
            _log.warning("쿼리 확장 실패: %s", e)
            return []

    @staticmethod
    def _parse_keywords(text, original_query):
        """LLM 출력에서 키워드 파싱. 중복 제거, 원본 쿼리 제외."""
        keywords = []
        seen = set()
        original_lower = original_query.lower().strip()
        for part in text.replace('\n', ',').replace(';', ',').replace('/', ',').split(','):
            kw = part.strip().strip('-').strip('•').strip('*').strip('"').strip("'").strip()
            kw_lower = kw.lower()
            if (kw
                    and len(kw) >= 2
                    and kw_lower != original_lower
                    and kw_lower not in seen
                    and not kw_lower.startswith('검색어')
                    and not kw_lower.startswith('keyword')):
                keywords.append(kw)
                seen.add(kw_lower)
        return keywords[:10]  # 최대 10개
