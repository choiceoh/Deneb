"""interrupt 명령 — AI의 현재 작업을 중단하고 유저 입력에 집중하게 함."""
from datetime import datetime
from core import register_command, _load_session, _save_session


def _interrupt_summary(d):
    return d.get('message', '작업 중단됨 — 유저 입력 대기 중')


@register_command('interrupt', needs_db=False, read_only=True, category='system',
                  summary_fn=_interrupt_summary)
def _exec_interrupt(params):
    """현재 AI 작업을 중단하고 유저의 새 입력에 집중하도록 신호를 보냄."""

    user_message = ' '.join(params.get('sub_args', [])) or params.get('query', '')

    # 세션에 인터럽트 기록
    session = _load_session()
    session['interrupted'] = True
    session['interrupted_at'] = datetime.now().isoformat()
    session['interrupted_task'] = session.get('last_command')
    _save_session(session)

    result = {
        'interrupted': True,
        'timestamp': datetime.now().isoformat(),
        'previous_task': session.get('last_command'),
        'message': '현재 작업을 중단합니다. 유저 입력에 집중합니다.',
    }

    # 유저가 인터럽트와 함께 새 메시지를 보낸 경우
    if user_message:
        result['user_message'] = user_message
        result['message'] = f'작업 중단 — 새 요청: "{user_message}"'

    # AI 행동 가이드
    result['_ai_hint'] = [
        {
            'situation': 'user_interrupt',
            'tone': 'attentive',
            'priority': 'highest',
            'guide': (
                '⚠️ 유저가 현재 작업 중단을 요청했습니다. '
                '진행 중이던 모든 작업(검색, 분석, 보고서 작성 등)을 즉시 멈추세요. '
                '유저의 새 입력에 100% 집중하세요. '
                '중단된 작업에 대해 길게 설명하지 말고, '
                '"네, 말씀하세요" 또는 "중단했습니다. 무엇을 도와드릴까요?" 정도로 짧게 응답하세요.'
            ),
        },
    ]

    if user_message:
        result['_ai_hint'].append({
            'situation': 'interrupt_with_new_request',
            'guide': (
                f'유저가 중단과 함께 새 요청을 보냈습니다: "{user_message}". '
                f'이전 작업은 무시하고 이 새 요청을 바로 처리하세요.'
            ),
        })

    return result
