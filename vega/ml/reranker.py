"""LocalReranker -- cross-encoder reranking via Qwen3-Reranker-4B."""

import logging

from .manager import ModelManager

_log = logging.getLogger(__name__)


# ──────────────────────────────────────────────
# 3. LocalReranker
# ──────────────────────────────────────────────

class LocalReranker:
    """cross-encoder 리랭킹 (Qwen3-Reranker-4B).

    Qwen3-Reranker 방식:
    1. 프롬프트: query + document → "yes"/"no" 판별
    2. "yes" 토큰의 logprob → sigmoid → 관련성 점수 (0~1)
    """

    # Qwen3-Reranker 공식 프롬프트 템플릿
    _PROMPT_TEMPLATE = (
        '<|im_start|>system\nJudge whether the document is relevant to the search query. '
        'Answer only "yes" or "no".<|im_end|>\n'
        '<|im_start|>user\n<query>{query}</query>\n<document>{document}</document><|im_end|>\n'
        '<|im_start|>assistant\n'
    )

    def __init__(self, manager=None):
        self.mgr = manager or ModelManager()

    def rerank(self, query, docs):
        """query에 대한 각 doc의 관련성 점수 리스트 반환 (0~1).

        Returns:
            list[float] — 정상 (빈 docs면 빈 리스트)
            None — 모델 로드 실패
        """
        if not docs:
            return []
        if not query:
            return [0.0] * len(docs)

        model = self.mgr.get_model('reranker')
        if model is None:
            return None

        import math
        scores = []
        for doc in docs:
            try:
                prompt = self._PROMPT_TEMPLATE.format(
                    query=str(query)[:1000],
                    document=str(doc)[:1500]  # 문서 길이 제한
                )
                output = model(
                    prompt,
                    max_tokens=1,
                    logprobs=1,  # top_logprobs 1개 요청 (True가 아닌 int)
                    temperature=0.0,
                )
                logprob = self._extract_yes_logprob(output)
                score = 1.0 / (1.0 + math.exp(-logprob))  # sigmoid
                scores.append(score)
            except Exception as e:
                _log.warning("리랭킹 실패 (단건): %s", e)
                scores.append(0.0)

        return scores

    @staticmethod
    def _extract_yes_logprob(output):
        """llama-cpp-python 출력에서 "yes" 토큰의 logprob 추출.

        llama-cpp-python의 completion 응답 형식:
        {
          'choices': [{
            'text': 'yes',
            'logprobs': {
              'tokens': ['yes'],
              'token_logprobs': [-0.5],
              'top_logprobs': [{'yes': -0.5, 'no': -1.2}],
              'text_offset': [0]
            }
          }]
        }
        """
        try:
            choices = output.get('choices', [])
            if not choices:
                return 0.0

            logprobs_data = choices[0].get('logprobs')
            if logprobs_data:
                # top_logprobs에서 "yes" 토큰 logprob 탐색
                top_list = logprobs_data.get('top_logprobs') or []
                if top_list:
                    top = top_list[0]  # 첫 번째 토큰의 top logprobs
                    if isinstance(top, dict):
                        for token, lp in top.items():
                            if token.strip().lower() == 'yes' and isinstance(lp, (int, float)):
                                return float(lp)

                # top_logprobs에 "yes" 없으면 token_logprobs 사용
                tokens = logprobs_data.get('tokens', [])
                token_logprobs = logprobs_data.get('token_logprobs', [])
                if tokens and token_logprobs and token_logprobs[0] is not None:
                    lp = token_logprobs[0]
                    if not isinstance(lp, (int, float)):
                        return 0.0
                    if tokens[0].strip().lower() == 'yes':
                        return float(lp)
                    # 생성된 토큰이 "no"면 logprob을 반전
                    if tokens[0].strip().lower() == 'no':
                        return -abs(float(lp)) - 1.0

            # logprobs 구조가 없으면 생성된 텍스트로 판별
            text = choices[0].get('text', '').strip().lower()
            if text.startswith('yes'):
                return 2.0
            return -2.0
        except Exception:
            return 0.0
