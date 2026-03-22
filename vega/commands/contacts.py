import config
from core import register_command


@register_command('contacts',
    summary_fn=lambda d: f"연락처 {d.get('count',0)}명")
def _exec_contacts(params):
    from addons import Contacts, Ctx
    ctx = Ctx(config.DB_PATH)
    c = Contacts()

    query = params.get('query', '')
    sub_args = params.get('sub_args', [])

    # 명시적 서브명령 처리
    if sub_args and sub_args[0] in ('search', 'all', 'project'):
        if sub_args[0] == 'project' and len(sub_args) > 1:
            return c.safe_api('project', sub_args[1:], ctx)
        search_term = ' '.join(sub_args[1:]) if len(sub_args) > 1 else ''
    else:
        search_term = ' '.join(sub_args) if sub_args else query

    # 검색어 정제 (자연어 노이즈 제거)
    if search_term:
        noise = {'연락처', '전화번호', '이메일', '담당자', '알려', '검색', '찾아'}
        q_words = [w for w in search_term.split() if w not in noise]
        search_term = ' '.join(q_words)

    # addon의 search 서브커맨드 활용
    if search_term:
        return c.safe_api('search', [search_term], ctx)
    return c.safe_api('', [], ctx)
