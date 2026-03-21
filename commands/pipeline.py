import config
from core import register_command


@register_command('pipeline',
    summary_fn=lambda d: f"파이프라인 총 {d.get('total_amount',0):,.1f}억원 ({d.get('identified_count',0)}/{d.get('total_count',0)}개 금액 파악)")
def _exec_pipeline(params):
    from addons import Pipeline, Ctx
    ctx = Ctx(config.DB_PATH)
    p = Pipeline()

    sub_args = params.get('sub_args', [])
    cmd = sub_args[0] if sub_args else ''

    result_raw = p.safe_api(cmd, sub_args[1:] if sub_args else [], ctx)
    if 'error' in result_raw:
        return result_raw

    items = result_raw.get('items', [])
    result = {
        'total_amount': sum(i['amount'] or 0 for i in items),
        'identified_count': sum(1 for i in items if i['amount']),
        'total_count': len(items),
        'projects': items,
    }

    from collections import defaultdict
    by_stage = defaultdict(lambda: {'count': 0, 'amount': 0, 'projects': []})
    for i in items:
        by_stage[i['stage']]['count'] += 1
        by_stage[i['stage']]['amount'] += i['amount'] or 0
        by_stage[i['stage']]['projects'].append(i['name'])
    result['by_stage'] = dict(by_stage)

    if cmd == 'by-person':
        by_person = defaultdict(lambda: {'total': 0, 'projects': []})
        for i in items:
            for person in (i['persons'] or ['(미지정)']):
                by_person[person]['total'] += i['amount'] or 0
                by_person[person]['projects'].append({'id': i['id'], 'name': i['name'], 'amount': i['amount']})
        result['by_person'] = dict(by_person)

    return result
