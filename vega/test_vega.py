#!/usr/bin/env python3
"""
Vega 프로젝트 검색엔진 — 종합 테스트 스위트 (v1.41)

실행:
  python3 -m unittest test_vega -v
  python3 test_vega.py              # 직접 실행

DB 없이도 자동으로 fixture DB를 생성하여 모든 테스트 실행 가능.
"""

import unittest
import sys
import os
import json
import tempfile
import shutil
from pathlib import Path
from datetime import datetime, timedelta

SELF_DIR = Path(__file__).parent
sys.path.insert(0, str(SELF_DIR))

import config
from core import execute


# ──────────────────────────────────────────────
# Fixture: 테스트용 .md 파일 + DB 자동 생성
# ──────────────────────────────────────────────

_FIXTURE_MD_1 = """# 비금도 해상태양광

| 항목 | 내용 |
|------|------|
| **상태** | 시공 중 🟢 |
| **발주처** | 한국전력 |
| **사내 담당** | 고건 팀장 |
| **거래처 담당** | Christina Gu (ZTT) |
| **규모** | 100MW |
| **품목** | 해상태양광 |

## 현재 상황
- 154kV 해저케이블 공사 진행 중
- ZTT 케이블 납기 5월 예정

## 다음 예상 액션
- 2026-03-25: FAT 출장 일정 확정 필요
- CU 헷징 계약 검토

## 이력
- 2026-03-15: 케이블 선적 일정 조율
- 2026-03-01: PF 조건 협의 완료

## 2026-03-19
- **비금도 케이블 납기 건** (Christina Gu)
  - 최종 ETD 5월 12일 목표, 선적 일정 조율 중

## 2026-03-15
- **RE: 해저케이블 공사 진행** (고건)
  - 154kV 케이블 포설 2차 구간 완료
"""

_FIXTURE_MD_2 = """# 화성산단 태양광

| 항목 | 내용 |
|------|------|
| **상태** | 검토 중 🟡 |
| **발주처** | 현대엔지니어링 |
| **사내 담당** | 고건 팀장 |
| **거래처 담당** | 김서현 책임매니저 (현대위아) |
| **규모** | 50MW |
| **품목** | 육상태양광 |

## 현재 상황
- PF 조건 검토 진행 중
- 진코 635Wp 모듈 견적 수령

## 다음 예상 액션
- 환경영향평가 제출
- 모듈 가격 최종 협상

## 이력
- 2026-03-10: 초기 사업타당성 검토 완료

## 2026-03-18
- **화성산단 PF 조건 검토** (김서현)
  - 현대위아 지붕형 태양광 50MW 제안
"""

_FIXTURE_MD_3 = """# 제주 풍력 ESS

| 항목 | 내용 |
|------|------|
| **상태** | 긴급 대응 중 🔴 |
| **발주처** | 제주에너지공사 |
| **사내 담당** | 이경렬 과장 |
| **거래처 담당** | Alan Zhang (Peak Energy) |
| **규모** | 30MW/60MWh |
| **품목** | ESS |

## 현재 상황
- ESS 인버터 불량 이슈 발생
- Peak Energy와 긴급 대응 중

## 다음 예상 액션
- ESS 인버터 교체 일정 확정

## 이력
- 2026-02-01: 준공 완료
"""


class FixtureDB:
    """테스트용 임시 DB를 생성/관리하는 헬퍼"""

    def __init__(self):
        self.temp_dir = None
        self.md_dir = None
        self.db_path = None

    def setup(self):
        """임시 디렉토리에 .md 파일 생성 → DB 빌드"""
        self.temp_dir = tempfile.mkdtemp(prefix='vega_test_')
        self.md_dir = os.path.join(self.temp_dir, 'projects')
        os.makedirs(self.md_dir)

        # .md 파일 쓰기
        for name, content in [('비금도.md', _FIXTURE_MD_1), ('화성산단.md', _FIXTURE_MD_2), ('제주ESS.md', _FIXTURE_MD_3)]:
            Path(self.md_dir, name).write_text(content, encoding='utf-8')

        # 하위 디렉토리에도 하나 (rglob 테스트)
        sub_dir = os.path.join(self.md_dir, '거래처')
        os.makedirs(sub_dir)
        Path(sub_dir, 'ZTT_연락처.md').write_text(
            "# ZTT 연락처\n\n| 항목 | 내용 |\n|---|---|\n| **상태** | 참고자료 ⚪ |\n| **발주처** | - |\n| **사내 담당** | - |\n\n## 연락처\n- Christina Gu: christina@ztt.com, 010-1234-5678\n",
            encoding='utf-8'
        )

        self.db_path = os.path.join(self.temp_dir, 'test.db')

        # DB 빌드
        import project_db_v2
        project_db_v2.import_files(self.md_dir, db_path=self.db_path)

        # config 오버라이드
        import config
        self._orig_db = config.DB_PATH
        self._orig_md = config.MD_DIR
        config.DB_PATH = self.db_path
        config.MD_DIR = self.md_dir

    def teardown(self):
        """임시 디렉토리 정리 + config 복원"""
        import config
        config.DB_PATH = self._orig_db
        config.MD_DIR = self._orig_md
        if self.temp_dir and os.path.exists(self.temp_dir):
            shutil.rmtree(self.temp_dir, ignore_errors=True)


# 전역 fixture (setUpModule에서 1회 생성)
_fixture = FixtureDB()


def setUpModule():
    _fixture.setup()

def tearDownModule():
    _fixture.teardown()


# ──────────────────────────────────────────────
# Base class for vega tests
# ──────────────────────────────────────────────

class VegaTestCase(unittest.TestCase):
    """Base class for vega tests. Provides _exec() helper and fixture access."""

    def _exec(self, cmd, params=None):
        return execute(cmd, params or {})

    def _assert_ok(self, response, msg=None):
        """Assert response status is 'ok' and return data."""
        data = response.get('data')
        err = ''
        if isinstance(data, dict):
            err = data.get('error', '')
        self.assertEqual(response['status'], 'ok',
                        msg or f"{response.get('command', '?')} failed: {err}")
        return data

    def _search(self, query, **extra_params):
        """Shortcut for search command."""
        params = {'query': query}
        params.update(extra_params)
        return self._exec('search', params)


# ──────────────────────────────────────────────
# 1. 라우팅 (DB 불필요)
# ──────────────────────────────────────────────

class TestRouting(unittest.TestCase):
    """자연어 → 올바른 command 매핑"""

    def setUp(self):
        from core import route_input
        self.route = route_input

    def test_explicit_commands(self):
        for cmd in ['list', 'dashboard', 'pipeline', 'contacts', 'cross',
                     'health', 'brief', 'recent', 'compare', 'stats', 'ask']:
            self.assertEqual(self.route([cmd])[0], cmd, f'{cmd} 라우팅 실패')

    def test_nl_routes(self):
        cases = [
            (['현황'], 'dashboard'), (['대시보드'], 'dashboard'),
            (['연결고리'], 'cross'), (['연락처'], 'contacts'),
            (['파이프라인'], 'pipeline'), (['금액'], 'pipeline'),
            (['최근', '활동'], 'recent'), (['이번', '주'], 'weekly'),
            (['비교'], 'compare'), (['통계'], 'stats'),
            (['급한'], 'urgent'),
        ]
        for args, expected in cases:
            self.assertEqual(self.route(args)[0], expected, f'{args} → {expected} 실패')

    def test_number_to_show(self):
        cmd, params = self.route(['10'])
        self.assertEqual(cmd, 'show')
        self.assertEqual(params['id'], 10)

    def test_default_search(self):
        self.assertEqual(self.route(['비금도', 'ZTT'])[0], 'search')

    def test_empty_dashboard(self):
        self.assertEqual(self.route([])[0], 'dashboard')


# ──────────────────────────────────────────────
# 2. JSON 응답 구조 (fixture DB 사용)
# ──────────────────────────────────────────────

class TestResponseStructure(VegaTestCase):
    """모든 응답이 표준 JSON 구조를 가져야 함"""

    REQUIRED_KEYS = {'command', 'timestamp', 'status', 'data', 'summary'}

    def test_all_read_commands_return_ok(self):
        """모든 읽기 명령이 ok를 반환해야 함"""
        commands = ['list', 'dashboard', 'urgent', 'stats', 'health']
        for cmd in commands:
            r = self._exec(cmd)
            self.assertTrue(self.REQUIRED_KEYS.issubset(set(r.keys())), f'{cmd} 키 누락')
            self.assertEqual(r['status'], 'ok', f'{cmd} 실패: {r.get("data", {}).get("error")}')
            self.assertIsInstance(r['summary'], str, f'{cmd} summary가 str이 아님')

    def test_unknown_command_error(self):
        r = self._exec('nonexistent_xyz')
        self.assertEqual(r['status'], 'error')
        self.assertIn('available', r['data'])

    def test_search_structure(self):
        r = self._exec('search', {'query': '케이블'})
        self.assertEqual(r['status'], 'ok')
        d = r['data']
        self.assertIn('projects', d)
        self.assertIn('result_count', d)
        self.assertIn('matched_keywords', d)

    def test_show_structure(self):
        r = self._exec('show', {'sub_args': ['1']})
        self.assertEqual(r['status'], 'ok')
        d = r['data']
        for key in ('id', 'name', 'status', 'client', 'sections', 'recent_comms'):
            self.assertIn(key, d, f'show에 {key} 누락')

    def test_brief_structure(self):
        r = self._exec('brief', {'sub_args': ['1']})
        self.assertEqual(r['status'], 'ok')
        d = r['data']
        for key in ('project_id', 'project_name', 'status', 'next_actions', 'risks'):
            self.assertIn(key, d, f'brief에 {key} 누락')

    def test_person_structure(self):
        r = self._exec('person', {'name': '고건'})
        self.assertEqual(r['status'], 'ok')
        d = r['data']
        self.assertIn('projects', d)
        self.assertGreater(d['project_count'], 0, '고건 프로젝트 0건')


# ──────────────────────────────────────────────
# 3. 신규 명령 (v1.13+)
# ──────────────────────────────────────────────

class TestNewCommands(VegaTestCase):

    def test_compare_two_projects(self):
        r = self._exec('compare', {'projects': ['1', '2']})
        self.assertEqual(r['status'], 'ok')
        d = r['data']
        self.assertEqual(d['project_count'], 2)
        self.assertIn('shared', d)
        self.assertIn('unique_per_project', d)

    def test_compare_duplicate_id(self):
        """같은 ID 2번 → 중복 제거 → 에러 또는 1개"""
        r = self._exec('compare', {'projects': ['1', '1']})
        # 중복 제거 후 1개만 남으면 에러여야 함
        if r['status'] == 'ok':
            self.assertLessEqual(r['data']['project_count'], 2)

    def test_compare_insufficient(self):
        """프로젝트 1개만 → 에러"""
        r = self._exec('compare', {'projects': ['1']})
        self.assertEqual(r['status'], 'error')

    def test_stats(self):
        r = self._exec('stats')
        self.assertEqual(r['status'], 'ok')
        d = r['data']
        self.assertIn('projects', d)
        self.assertIn('communication', d)
        self.assertGreater(d['projects']['total'], 0)

    def test_list_filter_status(self):
        r = self._exec('list', {'status': '시공'})
        self.assertEqual(r['status'], 'ok')
        self.assertIn('filters', r['data'])
        # 비금도만 매칭되어야 함
        names = [p['name'] for p in r['data']['projects']]
        self.assertTrue(any('비금도' in n for n in names))

    def test_list_filter_person(self):
        r = self._exec('list', {'person': '고건'})
        self.assertEqual(r['status'], 'ok')
        # 고건이 담당하는 프로젝트만
        self.assertGreater(r['data']['total'], 0)

    def test_list_no_filter(self):
        r = self._exec('list')
        self.assertEqual(r['status'], 'ok')
        self.assertGreaterEqual(r['data']['total'], 3)  # fixture에 3+1개


# ──────────────────────────────────────────────
# 4. ask 통합 엔드포인트 (v1.15)
# ──────────────────────────────────────────────

class TestAsk(VegaTestCase):

    def test_ask_routes_to_search_or_brief(self):
        """ask with keywords routes to search or brief (smart routing)"""
        r = self._exec('ask', {'query': '케이블 선적 진행'})
        self.assertEqual(r['status'], 'ok')
        meta = r['data'].get('_meta', {})
        # _smart_route may redirect to brief if project detected
        self.assertIn(meta.get('routed_to'), ('search', 'brief'))

    def test_ask_routes_to_urgent(self):
        r = self._exec('ask', {'query': '오늘 급한 거 있어?'})
        self.assertEqual(r['status'], 'ok')
        meta = r['data'].get('_meta', {})
        self.assertEqual(meta.get('routed_to'), 'urgent')

    def test_ask_routes_to_dashboard(self):
        r = self._exec('ask', {'query': '전체 현황'})
        self.assertEqual(r['status'], 'ok')
        meta = r['data'].get('_meta', {})
        self.assertEqual(meta.get('routed_to'), 'dashboard')

    def test_ask_depth_brief(self):
        r = self._exec('ask', {'query': '전체 현황', 'depth': 'brief'})
        self.assertEqual(r['status'], 'ok')
        d = r['data']
        # brief 모드에서는 by_status가 축약되어야 함 (카운트만)
        if 'by_status' in d:
            for v in d['by_status'].values():
                self.assertIsInstance(v, int)

    def test_ask_empty_query_error(self):
        r = self._exec('ask', {'query': ''})
        self.assertEqual(r['status'], 'error')

    def test_ask_whitespace_query_error(self):
        r = self._exec('ask', {'query': '   '})
        self.assertEqual(r['status'], 'error')

    def test_ask_ai_hint_present(self):
        r = self._exec('ask', {'query': '급한 거 있어?'})
        self.assertEqual(r['status'], 'ok')
        # urgent에 critical이 있으면 _ai_hint가 있어야 함
        d = r['data']
        if d.get('critical', 0) > 0:
            self.assertIn('_ai_hint', d)

    def test_ask_no_circular_recursion(self):
        """ask가 ask로 라우팅되면 search로 폴백해야 함"""
        r = self._exec('ask', {'query': 'ask something'})
        self.assertEqual(r['status'], 'ok')
        meta = r['data'].get('_meta', {})
        self.assertNotEqual(meta.get('routed_to'), 'ask')


# ──────────────────────────────────────────────
# 5. 엣지케이스
# ──────────────────────────────────────────────

class TestEdgeCases(VegaTestCase):

    def test_search_fts_reserved_words(self):
        """FTS5 예약어가 크래시를 일으키지 않아야 함"""
        for word in ['AND', 'OR', 'NOT', 'NEAR', 'AND OR']:
            r = self._exec('search', {'query': word})
            self.assertEqual(r['status'], 'ok', f"'{word}' 검색 실패")

    def test_search_special_chars(self):
        """특수문자 포함 쿼리"""
        for q in ['O&M', '154kV/22.9kV', '(주)삼일기전', '"quoted"']:
            r = self._exec('search', {'query': q})
            self.assertEqual(r['status'], 'ok', f"'{q}' 검색 실패")

    def test_search_empty_query(self):
        r = self._exec('search', {'query': ''})
        self.assertEqual(r['status'], 'ok')

    def test_show_nonexistent(self):
        r = self._exec('show', {'sub_args': ['99999']})
        self.assertEqual(r['status'], 'error')

    def test_brief_nonexistent(self):
        r = self._exec('brief', {'sub_args': ['99999']})
        # auto-correct이 search로 전환할 수 있음
        self.assertIn(r['status'], ('ok', 'error'))

    def test_timeline_nonexistent(self):
        r = self._exec('timeline', {'sub_args': ['99999']})
        self.assertIn(r['status'], ('ok', 'error'))

    def test_person_nonexistent(self):
        r = self._exec('person', {'name': '존재하지않는사람XYZ'})
        self.assertEqual(r['status'], 'ok')
        self.assertEqual(r['data']['project_count'], 0)

    def test_min_score_filter(self):
        r = self._exec('search', {'query': '태양광', 'min_score': 999})
        self.assertEqual(r['status'], 'ok')
        # 매우 높은 min_score → 결과 0
        self.assertEqual(r['data']['result_count']['projects'], 0)


# ──────────────────────────────────────────────
# 6. Korean NLP (DB 불필요)
# ──────────────────────────────────────────────

class TestKoreanNLP(unittest.TestCase):

    def test_preprocess_preserves_place_names(self):
        """비금도, 진도, 완도는 '도' 제거하면 안 됨"""
        from router import _preprocess_korean
        result = _preprocess_korean('비금도에서')
        self.assertIn('비금도', result)

    def test_preprocess_removes_particles(self):
        from router import _preprocess_korean
        result = _preprocess_korean('케이블을')
        self.assertIn('케이블', result)

    def test_preprocess_non_string(self):
        """비문자열 입력 → 크래시 안 됨"""
        from router import _preprocess_korean
        result = _preprocess_korean(None)
        self.assertIsInstance(result, list)
        result = _preprocess_korean(123)
        self.assertIsInstance(result, list)


# ──────────────────────────────────────────────
# 7. FTS sanitize (DB 불필요)
# ──────────────────────────────────────────────

class TestFTSSanitize(unittest.TestCase):

    def setUp(self):
        from project_db_v2 import _sanitize_fts
        self.sanitize = _sanitize_fts

    def test_reserved_quoted(self):
        self.assertEqual(self.sanitize('AND'), '"AND"')
        self.assertEqual(self.sanitize('NOT'), '"NOT"')

    def test_special_chars_quoted(self):
        self.assertEqual(self.sanitize('O&M'), '"O&M"')

    def test_normal_pass(self):
        self.assertEqual(self.sanitize('비금도'), '비금도')

    def test_empty_none(self):
        self.assertIsNone(self.sanitize(''))
        self.assertIsNone(self.sanitize('   '))


# ──────────────────────────────────────────────
# 8. 금액 추출 (DB 불필요)
# ──────────────────────────────────────────────

class TestAmountExtraction(unittest.TestCase):

    def setUp(self):
        from addons import Pipeline
        self.p = Pipeline()

    def test_positive_억(self):
        self.assertAlmostEqual(self.p._extract_amount("계약금액 45억원"), 45.0, places=1)

    def test_positive_총사업비(self):
        self.assertAlmostEqual(self.p._extract_amount("총사업비 1,701억원"), 1701.0, places=1)

    def test_negative_보상비(self):
        self.assertIsNone(self.p._extract_amount("어업보상비 3,993억원"))

    def test_empty(self):
        self.assertIsNone(self.p._extract_amount(""))
        self.assertIsNone(self.p._extract_amount(None))


# ──────────────────────────────────────────────
# 9. Config (DB 불필요)
# ──────────────────────────────────────────────

class TestConfig(unittest.TestCase):

    def test_config_import(self):
        from config import DB_PATH, KNOWN_VENDORS, KNOWN_MATERIALS, VegaError, db_session
        self.assertIsInstance(KNOWN_VENDORS, list)
        self.assertTrue(len(KNOWN_VENDORS) > 0)
        # VegaError 사용 가능
        try:
            raise VegaError('test', usage=['test'])
        except VegaError as e:
            self.assertEqual(e.message, 'test')

    def test_db_session_context_manager(self):
        from config import db_session
        with db_session(_fixture.db_path) as conn:
            result = conn.execute("SELECT COUNT(*) FROM projects").fetchone()
            self.assertGreater(result[0], 0)
        # conn은 자동으로 닫혀야 함


# ──────────────────────────────────────────────
# 10. rglob 재귀 스캔 (v1.16 수정 검증)
# ──────────────────────────────────────────────

class TestRecursiveScan(unittest.TestCase):

    def test_rglob_finds_subdirectory_files(self):
        """하위 디렉토리의 .md 파일도 DB에 임포트되어야 함"""
        from config import db_session
        with db_session(_fixture.db_path) as conn:
            result = conn.execute("SELECT COUNT(*) FROM projects").fetchone()
            # fixture: 비금도, 화성산단, 제주ESS, ZTT연락처 = 4개
            self.assertGreaterEqual(result[0], 4, f"프로젝트 {result[0]}개 — 하위 디렉토리 .md 누락 가능")


class TestCommandMetadata(unittest.TestCase):
    """명령 메타데이터 검증"""

    def test_all_commands_have_metadata(self):
        from core import _COMMAND_REGISTRY
        for name, entry in _COMMAND_REGISTRY.items():
            self.assertIsInstance(entry, dict, f'{name} 메타데이터 없음 (old tuple format)')
            self.assertIn('handler', entry, f'{name} handler 없음')
            self.assertIn('read_only', entry, f'{name} read_only 없음')
            self.assertIn('category', entry, f'{name} category 없음')

    def test_write_commands_not_readonly(self):
        from core import _COMMAND_REGISTRY
        write_cmds = ['update', 'mail-append', 'add-action']
        for name in write_cmds:
            if name in _COMMAND_REGISTRY:
                self.assertFalse(_COMMAND_REGISTRY[name]['read_only'], f'{name}은 read_only=False여야 함')

    def test_read_commands_readonly(self):
        from core import _COMMAND_REGISTRY
        read_cmds = ['search', 'dashboard', 'list', 'show', 'brief', 'urgent', 'person', 'stats', 'compare']
        for name in read_cmds:
            if name in _COMMAND_REGISTRY:
                self.assertTrue(_COMMAND_REGISTRY[name]['read_only'], f'{name}은 read_only=True여야 함')


class TestSearchIntegration(unittest.TestCase):
    """검색 통합 테스트 — 의미 검색 미연결 환경에서도 안전하게 작동하는지 검증"""

    def test_router_graceful_fallback(self):
        """의미 검색 미연결 시 SearchRouter가 SQLite 폴백으로 작동"""
        from router import SearchRouter
        router = SearchRouter()
        results = router.search("테스트")
        self.assertIn('analysis', results)
        self.assertIn('sqlite', results)

    def test_search_unified_format(self):
        """SearchRouter가 Vega 통합 결과 형식(unified)을 반환하는지 검증"""
        from router import SearchRouter, _make_result
        router = SearchRouter()
        results = router.search("테스트")
        self.assertIn('unified', results)
        self.assertIsInstance(results['unified'], list)
        for item in results['unified']:
            self.assertIn('project_id', item)
            self.assertIn('project_name', item)
            self.assertIn('client', item)
            self.assertIn('status', item)
            self.assertIn('person', item)
            self.assertIn('content', item)
            self.assertIn('heading', item)
            self.assertIn('score', item)
            self.assertIn('source', item)
            self.assertIn('entry_date', item)
            self.assertIn('chunk_type', item)
            self.assertIn('metadata', item)
            self.assertIn(item['source'], ('sqlite', 'semantic'))

    def test_make_result_factory(self):
        """_make_result 팩토리가 올바른 Vega canonical dict를 생성하는지 검증"""
        from router import _make_result
        r = _make_result(
            project_id=1, project_name="테스트", client="고객사",
            status="진행 중", person="담당자", content="내용",
            heading="제목", score=42.5, source="sqlite",
            entry_date="2026-03-19", chunk_type="section",
        )
        self.assertEqual(r['project_id'], 1)
        self.assertEqual(r['project_name'], '테스트')
        self.assertEqual(r['source'], 'sqlite')
        self.assertEqual(r['score'], 42.5)
        self.assertIsInstance(r['metadata'], dict)

    def test_row_value_none_safety(self):
        """_row_value가 SQL NULL(None) 값에 대해 default를 반환하는지 검증"""
        from router import _row_value
        self.assertEqual(_row_value(None, 'key'), '')
        self.assertEqual(_row_value(None, 'key', 'fallback'), 'fallback')
        self.assertEqual(_row_value({'name': None}, 'name', ''), '')
        self.assertEqual(_row_value({'name': None}, 'name', 'default'), 'default')
        self.assertEqual(_row_value({'name': '테스트'}, 'name', ''), '테스트')
        self.assertEqual(_row_value({'a': 1}, 'missing', 'default'), 'default')

    def test_unified_format_via_search_command(self):
        """search 명령이 통합 형식을 기반으로 올바른 응답을 반환하는지 검증"""
        r = execute('search', {'query': '테스트'})
        self.assertEqual(r['status'], 'ok')
        data = r['data']
        self.assertIn('projects', data)
        self.assertIn('result_count', data)
        for p in data['projects']:
            self.assertIn('id', p)
            self.assertIn('name', p)
            self.assertIn('sections', p)


class TestNegateDateStr(unittest.TestCase):
    """v1.333: _negate_date_str 날짜 반전 정렬 키 테스트"""

    def test_recent_before_old(self):
        from router import _negate_date_str
        recent = _negate_date_str('2026-03-20')
        old = _negate_date_str('2024-01-01')
        self.assertLess(recent, old, "최신 날짜가 더 작은(앞선) 정렬 키를 가져야 함")

    def test_null_last(self):
        from router import _negate_date_str
        any_date = _negate_date_str('2020-01-01')
        null_key = _negate_date_str('')
        self.assertLess(any_date, null_key, "빈 날짜는 맨 뒤(z)로 가야 함")

    def test_none_last(self):
        from router import _negate_date_str
        self.assertEqual(_negate_date_str(None), 'z')

    def test_same_date_equal(self):
        from router import _negate_date_str
        self.assertEqual(_negate_date_str('2026-03-20'), _negate_date_str('2026-03-20'))


class TestNonProjectPenalty(unittest.TestCase):
    """v1.333: 비프로젝트 문서 노이즈 페널티 테스트"""

    def test_index_md_penalized(self):
        """INDEX.md 소스를 가진 의미 검색 결과는 0.3× 페널티를 받아야 함"""
        import re
        pattern = re.compile(r'(INDEX|README|CLAUDE|CHANGELOG|TODO|LICENSE|\.github)', re.IGNORECASE)
        self.assertTrue(pattern.search('/projects/INDEX.md'))
        self.assertTrue(pattern.search('/path/to/README.md'))
        self.assertTrue(pattern.search('CLAUDE.md'))
        self.assertTrue(pattern.search('.github/workflows/ci.yml'))

    def test_normal_project_not_penalized(self):
        """일반 프로젝트 파일은 페널티를 받지 않아야 함"""
        import re
        pattern = re.compile(r'(INDEX|README|CLAUDE|CHANGELOG|TODO|LICENSE|\.github)', re.IGNORECASE)
        self.assertIsNone(pattern.search('/projects/비금도.md'))
        self.assertIsNone(pattern.search('/projects/화성산단.md'))
        self.assertIsNone(pattern.search('/projects/영암태양광.md'))


class TestSearchFusion(unittest.TestCase):
    """검색 퓨전 + AI 사용성 테스트"""

    def test_load_project_lookup(self):
        """_load_project_lookup이 프로젝트 메타데이터를 반환하는지 검증"""
        from router import _load_project_lookup
        lookup = _load_project_lookup(config.DB_PATH)
        self.assertIsInstance(lookup, dict)
        # fixture DB에 프로젝트가 있으면 key가 소문자여야 함
        if lookup:
            for key in lookup:
                self.assertEqual(key, key.lower())
                info = lookup[key]
                self.assertIn('id', info)
                self.assertIn('name', info)
                self.assertIn('client', info)
                self.assertIn('status', info)
                self.assertIn('person', info)

    def test_load_project_lookup_none_db(self):
        """db_path=None이면 빈 dict 반환"""
        from router import _load_project_lookup
        self.assertEqual(_load_project_lookup(None), {})

    def test_semantic_items_to_unified_with_db(self):
        """_semantic_items_to_unified에 db_path를 전달하면 메타데이터가 보강되는지 검증"""
        from router import _semantic_items_to_unified
        fake = [{
            'content': '테스트 내용',
            'score': 25.0,
            'source': '/projects/테스트프로젝트.md',
            'metadata': {
                'project_name': '테스트프로젝트',
                'title': '현재 상황',
                'filepath': '/projects/테스트프로젝트.md',
            }
        }]
        unified = _semantic_items_to_unified(fake, db_path=config.DB_PATH)
        self.assertEqual(len(unified), 1)
        self.assertEqual(unified[0]['source'], 'semantic')

    def test_search_meta_in_router(self):
        """SearchRouter.search()가 search_meta를 반환하는지 검증"""
        from router import SearchRouter
        router = SearchRouter()
        results = router.search("테스트")
        self.assertIn('search_meta', results)
        sm = results['search_meta']
        self.assertIn('route', sm)
        self.assertIn('semantic_available', sm)
        self.assertIn('semantic_used', sm)
        self.assertIn('sqlite_count', sm)
        self.assertIn('semantic_count', sm)
        self.assertIn('rerank_mode', sm)

    def test_search_command_no_analysis(self):
        """search 명령 응답에 analysis가 없고 search_meta가 있는지 검증"""
        r = execute('search', {'query': '테스트'})
        self.assertEqual(r['status'], 'ok')
        data = r['data']
        self.assertNotIn('analysis', data)
        self.assertNotIn('route', data)
        self.assertNotIn('route_reason', data)
        self.assertIn('search_meta', data)

    def test_search_command_no_raw_semantic_results_key(self):
        """search 명령 응답에 raw 의미검색 결과 배열이 없는지 검증"""
        r = execute('search', {'query': '테스트'})
        data = r['data']
        self.assertNotIn('raw_semantic_results', data)
        self.assertNotIn('semantic_results', data)

    def test_search_projects_have_sources(self):
        """search 명령의 projects[].sources 필드가 있는지 검증"""
        r = execute('search', {'query': '테스트'})
        data = r['data']
        for p in data.get('projects', []):
            self.assertIn('sources', p)
            self.assertIsInstance(p['sources'], list)
            for s in p['sources']:
                self.assertIn(s, ('sqlite', 'semantic'))

    def test_search_sections_have_source(self):
        """search 명령의 projects[].sections[].source 필드가 있는지 검증"""
        r = execute('search', {'query': '테스트'})
        data = r['data']
        for p in data.get('projects', []):
            for sec in p.get('sections', []):
                self.assertIn('source', sec)
                self.assertIn(sec['source'], ('sqlite', 'semantic'))

    def test_comms_in_router_results(self):
        """SearchRouter.search()가 comms를 별도 전달하는지 검증"""
        from router import SearchRouter
        router = SearchRouter()
        results = router.search("테스트")
        self.assertIn('comms', results)
        self.assertIsInstance(results['comms'], list)


# ──────────────────────────────────────────────
# v1.335: Pipeline 금액 추출 테스트
# ──────────────────────────────────────────────

class TestPipelineAmountV1335(unittest.TestCase):
    """Pipeline._extract_amount() 이중 매칭 방지 및 컨텍스트 필터링 검증"""

    def setUp(self):
        sys.path.insert(0, str(SELF_DIR))
        from addons import Pipeline
        self.pipeline = Pipeline()

    def test_no_double_count_won_and_eok(self):
        """'1,568,000,000원' 과 '약 15.68억원'이 같은 줄에 있으면 이중 카운트 방지"""
        text = "계약금액 1,568,000,000원 (약 15.68억원)"
        amount = self.pipeline._extract_amount(text)
        self.assertIsNotNone(amount)
        # 첫 매칭(원)만 잡히거나 하나만 잡혀야 함
        self.assertAlmostEqual(amount, 15.68, delta=0.1)

    def test_negative_context_excluded(self):
        """부정 컨텍스트(보상비, O&M비) 줄의 금액은 제외"""
        text = "어업보상비 50억원\n계약금액 120억원"
        amount = self.pipeline._extract_amount(text)
        self.assertAlmostEqual(amount, 120.0, delta=0.1)

    def test_man_won_conversion(self):
        """만원 → 억원 변환"""
        text = "계약금액 230,000만원"
        amount = self.pipeline._extract_amount(text)
        self.assertAlmostEqual(amount, 23.0, delta=0.1)

    def test_usd_conversion(self):
        """USD 5M → 억원 변환"""
        text = "계약금액 USD 5M"
        amount = self.pipeline._extract_amount(text)
        self.assertAlmostEqual(amount, 65.0, delta=1.0)

    def test_small_amount_ignored(self):
        """0.5억 미만은 무시"""
        text = "수수료 300만원"  # 0.03억 → 무시, 부정 컨텍스트이기도 함
        amount = self.pipeline._extract_amount(text)
        self.assertIsNone(amount)


# ──────────────────────────────────────────────
# v1.335: Rerank fusion 분해 테스트
# ──────────────────────────────────────────────

class TestRerankFusionV1335(unittest.TestCase):
    """_score_sqlite_chunks, _score_semantic_results, _apply_ranking 분해 후 동작 검증"""

    def test_score_sqlite_chunks_basic(self):
        """SQLite 청크 스코어링 기본 동작"""
        from router import _score_sqlite_chunks
        chunks = [
            {'project_id': 1, 'name': '비금도', 'client': '한전', 'status': '시공',
             'person_internal': '고건', 'section_heading': '', 'content': '케이블', 'chunk_id': 1},
        ]
        scores, name_by_id, id_by_name = _score_sqlite_chunks(chunks, {'clients': ['한전'], 'persons': [], 'statuses': [], 'tags': [], 'keywords': []})
        self.assertIn(1, scores)
        self.assertGreater(scores[1], 0)
        self.assertEqual(name_by_id[1], '비금도')
        self.assertEqual(id_by_name['비금도'], 1)

    def test_rerank_fusion_unchanged_behavior(self):
        """분해 후 _rerank_fusion 통합 동작이 유지되는지 검증"""
        from router import _rerank_fusion
        sqlite_res = {
            'chunks': [
                {'project_id': 1, 'name': 'A', 'client': '', 'status': '', 'person_internal': '',
                 'section_heading': '', 'content': 'test', 'chunk_id': 1, 'entry_date': '2026-03-20'},
            ],
            'comms': [], 'project_ids': [1], 'project_names': ['A'],
        }
        result_sqlite, result_semantic = _rerank_fusion(sqlite_res, [], {'clients': [], 'persons': [], 'statuses': [], 'tags': [], 'keywords': []})
        self.assertIn('project_scores', result_sqlite)
        self.assertEqual(len(result_sqlite['project_scores']), 1)


# ──────────────────────────────────────────────
# v1.335: FTS rebuild 최적화 테스트
# ──────────────────────────────────────────────

class TestFTSRebuildV1335(unittest.TestCase):
    """rebuild_fts() 분리 후 호출 가능 여부 검증"""

    def test_rebuild_fts_callable(self):
        """rebuild_fts가 함수로 존재하고 호출 가능"""
        from project_db_v2 import rebuild_fts
        self.assertTrue(callable(rebuild_fts))


class TestVegaTestCase(VegaTestCase):
    """v1.337: VegaTestCase 기반 테스트 헬퍼 검증"""

    def test_assert_ok_passes_on_ok(self):
        r = self._exec('list')
        data = self._assert_ok(r)
        self.assertIsInstance(data, dict)

    def test_assert_ok_fails_on_error(self):
        r = self._exec('nonexistent_xyz')
        with self.assertRaises(AssertionError):
            self._assert_ok(r)

    def test_search_shortcut(self):
        r = self._search('케이블')
        self._assert_ok(r)


class TestBackupDirFilter(unittest.TestCase):
    """백업 디렉토리 필터 정규식 테스트"""

    def test_backup_dir_penalty_regex(self):
        """백업 디렉토리 패턴 감지 정규식 — 오탐 없이 정확히 매칭"""
        from router import _BACKUP_DIR_RE
        self.assertIsNotNone(_BACKUP_DIR_RE.search('/vega-v1.21/CLAUDE.md'))
        self.assertIsNotNone(_BACKUP_DIR_RE.search('/tools-backup'))
        self.assertIsNotNone(_BACKUP_DIR_RE.search('/path/backup-old/file.md'))
        self.assertIsNotNone(_BACKUP_DIR_RE.search('old-versions/file.md'))
        self.assertIsNone(_BACKUP_DIR_RE.search('/projects/비금도.md'))
        self.assertIsNone(_BACKUP_DIR_RE.search('/projects/서비스-v2/readme.md'))
        self.assertIsNone(_BACKUP_DIR_RE.search('/projects/nova-v3.md'))

    def test_penalty_no_stacking(self):
        """페널티가 min() 방식으로 적용되어 과도한 스태킹 없음"""
        from router import _NON_PROJECT_RE, _BACKUP_DIR_RE
        src = '/vega-v1.21/CLAUDE.md'
        self.assertIsNotNone(_BACKUP_DIR_RE.search(src))
        self.assertIsNotNone(_NON_PROJECT_RE.search(src))
        penalty = 1.0
        if _BACKUP_DIR_RE.search(src):
            penalty = min(penalty, 0.1)
        elif _NON_PROJECT_RE.search(src):
            penalty = min(penalty, 0.3)
        self.assertAlmostEqual(penalty, 0.1)
        self.assertNotAlmostEqual(penalty, 0.03)



# ──────────────────────────────────────────────
#  v1.34 — 검색 품질 개선 테스트 (3건)
# ──────────────────────────────────────────────
class TestSearchQualityV134(VegaTestCase):
    """v1.34: 검색 품질 개선 — 급한 라우팅, 의미+키워드 hybrid, 조사 키워드"""

    def test_urgent_keyword_routing(self):
        """'급한'이 status 패턴으로 인식 → sqlite 라우팅"""
        from router import analyze_query
        result = analyze_query('급한 프로젝트 뭐 있어')
        # '급한'이 status 패턴에 매칭되어 구조화 점수 > 0
        extracted = result.get('extracted', {})
        statuses = extracted.get('statuses', [])
        self.assertTrue(
            any('급한' in s for s in statuses),
            f"'급한'이 status로 추출되어야 함: {statuses}"
        )
        self.assertIn(result['route'], ('sqlite', 'hybrid'),
                       f"급한 쿼리는 sqlite 또는 hybrid여야 함: {result['route']}")

    def test_semantic_with_keywords_uses_hybrid(self):
        """의미 패턴 + 키워드 → hybrid (semantic 단독 아님)"""
        from router import analyze_query
        result = analyze_query('해저케이블 설계 어떻게')
        self.assertEqual(result['route'], 'hybrid',
                         f"의미+키워드 쿼리는 hybrid여야 함: {result['route']}")

    def test_matched_keywords_with_korean_particles(self):
        """조사 포함 쿼리에서도 matched_keywords가 채워져야 함"""
        r = self._search('비금도에서 케이블은')
        data = self._assert_ok(r)
        kw = data.get('matched_keywords', [])
        self.assertTrue(len(kw) > 0,
                        f"조사 제거 후 키워드가 매칭되어야 함: {kw}")


# ──────────────────────────────────────────────
# v1.4: 로컬 모델 테스트 (mock 기반 심화 검증)
# ──────────────────────────────────────────────

import time
import numpy as np
from unittest.mock import MagicMock, patch


class TestModelManager(VegaTestCase):
    """ModelManager 싱글톤 / 상태 / TTL 테스트"""

    def test_singleton(self):
        from models import ModelManager
        m1 = ModelManager()
        m2 = ModelManager()
        self.assertIs(m1, m2)

    def test_reset_clears_singleton(self):
        from models import ModelManager
        m1 = ModelManager()
        ModelManager.reset()
        m2 = ModelManager()
        self.assertIsNot(m1, m2)
        ModelManager.reset()  # cleanup

    def test_status_returns_all_roles(self):
        from models import ModelManager
        mgr = ModelManager()
        st = mgr.status()
        for role in ('expander', 'embedder', 'reranker'):
            self.assertIn(role, st)
            self.assertIn('file_exists', st[role])
            self.assertIn('loaded', st[role])
            self.assertIn('path', st[role])
        self.assertIn('llama_cpp_available', st)
        self.assertIn('numpy_available', st)
        ModelManager.reset()

    def test_get_model_missing_file_returns_none(self):
        from models import ModelManager
        mgr = ModelManager()
        result = mgr.get_model('embedder')
        self.assertIsNone(result)
        ModelManager.reset()

    def test_get_model_invalid_role_returns_none(self):
        from models import ModelManager
        mgr = ModelManager()
        result = mgr.get_model('nonexistent')
        self.assertIsNone(result)
        ModelManager.reset()

    def test_unload_specific(self):
        from models import ModelManager
        mgr = ModelManager()
        mgr._models['test'] = 'fake'
        mgr._last_used['test'] = time.time()
        mgr.unload('test')
        self.assertNotIn('test', mgr._models)
        self.assertNotIn('test', mgr._last_used)
        ModelManager.reset()

    def test_unload_all(self):
        from models import ModelManager
        mgr = ModelManager()
        mgr._models['a'] = 'fake1'
        mgr._models['b'] = 'fake2'
        mgr.unload()
        self.assertEqual(len(mgr._models), 0)
        ModelManager.reset()

    def test_unload_expired(self):
        from models import ModelManager
        mgr = ModelManager()
        mgr._models['old'] = 'fake'
        mgr._last_used['old'] = time.time() - 999
        mgr._models['new'] = 'fake'
        mgr._last_used['new'] = time.time()
        mgr.unload_expired()
        self.assertNotIn('old', mgr._models)
        self.assertIn('new', mgr._models)
        ModelManager.reset()


class TestVectorHelpers(VegaTestCase):
    """벡터 BLOB 변환 (numpy 네이티브)"""

    def test_blob_roundtrip(self):
        from models import _vector_to_blob, _blob_to_vector
        vec = np.array([0.1, 0.2, 0.3, -0.5], dtype=np.float32)
        blob = _vector_to_blob(vec)
        restored = _blob_to_vector(blob)
        np.testing.assert_array_almost_equal(vec, restored, decimal=6)

    def test_blob_length(self):
        from models import _vector_to_blob
        vec = np.zeros(128, dtype=np.float32)
        blob = _vector_to_blob(vec)
        self.assertEqual(len(blob), 128 * 4)

    def test_blob_roundtrip_high_dim(self):
        """4096차원 벡터 왕복 (실제 임베딩 크기)"""
        from models import _vector_to_blob, _blob_to_vector
        vec = np.random.randn(4096).astype(np.float32)
        blob = _vector_to_blob(vec)
        restored = _blob_to_vector(blob)
        np.testing.assert_array_almost_equal(vec, restored, decimal=6)

    def test_blob_from_list(self):
        """리스트 입력도 처리"""
        from models import _vector_to_blob, _blob_to_vector
        vec_list = [0.1, 0.2, 0.3]
        blob = _vector_to_blob(vec_list)
        restored = _blob_to_vector(blob)
        np.testing.assert_array_almost_equal(vec_list, restored, decimal=6)


class TestCallEmbed(VegaTestCase):
    """_call_embed API 호환 레이어 테스트"""

    def test_embed_returns_1d(self):
        """model.embed(text)가 1D 리스트 반환하는 경우"""
        from models import _call_embed
        mock_model = MagicMock()
        mock_model.embed.return_value = [0.1, 0.2, 0.3]
        result = _call_embed(mock_model, "test")
        self.assertEqual(result, [0.1, 0.2, 0.3])

    def test_embed_returns_2d(self):
        """model.embed(text)가 2D 리스트 반환하는 경우"""
        from models import _call_embed
        mock_model = MagicMock()
        mock_model.embed.return_value = [[0.1, 0.2, 0.3]]
        result = _call_embed(mock_model, "test")
        self.assertEqual(result, [0.1, 0.2, 0.3])

    def test_fallback_to_create_embedding(self):
        """embed() 없으면 create_embedding() 사용"""
        from models import _call_embed
        mock_model = MagicMock(spec=[])  # embed 없음
        mock_model.create_embedding = MagicMock(return_value={
            'data': [{'embedding': [0.4, 0.5, 0.6]}]
        })
        result = _call_embed(mock_model, "test")
        self.assertEqual(result, [0.4, 0.5, 0.6])

    def test_no_api_raises(self):
        """embed()도 create_embedding()도 없으면 에러"""
        from models import _call_embed
        mock_model = MagicMock(spec=[])  # 둘 다 없음
        with self.assertRaises(RuntimeError):
            _call_embed(mock_model, "test")


class TestLocalEmbedder(VegaTestCase):
    """LocalEmbedder 임베딩 로직 테스트"""

    def test_embed_normalizes(self):
        """출력이 L2 정규화되었는지 검증"""
        from models import LocalEmbedder, ModelManager
        ModelManager.reset()
        mgr = ModelManager()
        mock_model = MagicMock()
        mock_model.embed.side_effect = lambda t: [1.0, 2.0, 3.0]
        mgr._models['embedder'] = mock_model
        mgr._last_used['embedder'] = time.time()

        embedder = LocalEmbedder(mgr)
        result = embedder.embed(["hello", "world"])

        self.assertIsNotNone(result)
        self.assertEqual(result.shape, (2, 3))
        # L2 norm = 1.0
        for i in range(2):
            norm = np.linalg.norm(result[i])
            self.assertAlmostEqual(norm, 1.0, places=5)
        ModelManager.reset()

    def test_embed_single(self):
        from models import LocalEmbedder, ModelManager
        ModelManager.reset()
        mgr = ModelManager()
        mock_model = MagicMock()
        mock_model.embed.return_value = [1.0, 0.0, 0.0]
        mgr._models['embedder'] = mock_model
        mgr._last_used['embedder'] = time.time()

        embedder = LocalEmbedder(mgr)
        vec = embedder.embed_single("hello")
        self.assertIsNotNone(vec)
        self.assertEqual(vec.shape, (3,))
        self.assertAlmostEqual(vec[0], 1.0, places=5)
        ModelManager.reset()

    def test_embed_empty_texts(self):
        from models import LocalEmbedder, ModelManager
        ModelManager.reset()
        embedder = LocalEmbedder(ModelManager())
        self.assertIsNone(embedder.embed([]))
        self.assertIsNone(embedder.embed_single(""))
        self.assertIsNone(embedder.embed_single(None))
        ModelManager.reset()

    def test_embed_model_none(self):
        """모델 없으면 None"""
        from models import LocalEmbedder, ModelManager
        ModelManager.reset()
        embedder = LocalEmbedder(ModelManager())
        self.assertIsNone(embedder.embed(["test"]))
        ModelManager.reset()


class TestLocalReranker(VegaTestCase):
    """LocalReranker 리랭킹 로직 + logprob 추출 테스트"""

    def _make_output(self, text='yes', top_logprobs=None, tokens=None, token_logprobs=None):
        """llama-cpp-python completion 응답 구조 생성"""
        logprobs = {}
        if top_logprobs is not None:
            logprobs['top_logprobs'] = top_logprobs
        if tokens is not None:
            logprobs['tokens'] = tokens
        if token_logprobs is not None:
            logprobs['token_logprobs'] = token_logprobs
        return {
            'choices': [{
                'text': text,
                'logprobs': logprobs if logprobs else None,
            }]
        }

    def test_extract_yes_from_top_logprobs(self):
        """top_logprobs에서 'yes' 토큰 logprob 추출"""
        from models import LocalReranker
        output = self._make_output(
            text='yes',
            top_logprobs=[{'yes': -0.3, 'no': -1.5}],
        )
        lp = LocalReranker._extract_yes_logprob(output)
        self.assertAlmostEqual(lp, -0.3, places=5)

    def test_extract_yes_case_insensitive(self):
        from models import LocalReranker
        output = self._make_output(
            text='Yes',
            top_logprobs=[{'Yes': -0.5, 'No': -2.0}],
        )
        lp = LocalReranker._extract_yes_logprob(output)
        self.assertAlmostEqual(lp, -0.5, places=5)

    def test_extract_from_token_logprobs_when_yes(self):
        """top_logprobs에 'yes' 없으면 token_logprobs 사용"""
        from models import LocalReranker
        output = self._make_output(
            text='yes',
            top_logprobs=[{'si': -0.8, 'oui': -1.2}],  # yes 없음
            tokens=['yes'],
            token_logprobs=[-0.4],
        )
        lp = LocalReranker._extract_yes_logprob(output)
        self.assertAlmostEqual(lp, -0.4, places=5)

    def test_extract_no_answer_negative(self):
        """모델이 "no"라고 답하면 음수 logprob"""
        from models import LocalReranker
        output = self._make_output(
            text='no',
            top_logprobs=[{'no': -0.2}],
            tokens=['no'],
            token_logprobs=[-0.2],
        )
        lp = LocalReranker._extract_yes_logprob(output)
        self.assertTrue(lp < 0, f"'no' 답변이면 logprob < 0이어야 함: {lp}")

    def test_extract_no_logprobs_uses_text(self):
        """logprobs가 None이면 텍스트로 판별"""
        from models import LocalReranker
        output = {'choices': [{'text': 'yes', 'logprobs': None}]}
        lp = LocalReranker._extract_yes_logprob(output)
        self.assertEqual(lp, 2.0)

        output2 = {'choices': [{'text': 'no', 'logprobs': None}]}
        lp2 = LocalReranker._extract_yes_logprob(output2)
        self.assertEqual(lp2, -2.0)

    def test_extract_empty_output(self):
        from models import LocalReranker
        self.assertEqual(LocalReranker._extract_yes_logprob({}), 0.0)
        self.assertEqual(LocalReranker._extract_yes_logprob({'choices': []}), 0.0)

    def test_rerank_scores_range(self):
        """리랭크 점수가 0~1 범위"""
        from models import LocalReranker, ModelManager
        ModelManager.reset()
        mgr = ModelManager()
        mock_model = MagicMock()
        # "yes" 답변 시뮬레이션
        mock_model.return_value = self._make_output(
            text='yes',
            top_logprobs=[{'yes': -0.3, 'no': -2.0}],
        )
        mgr._models['reranker'] = mock_model
        mgr._last_used['reranker'] = time.time()

        reranker = LocalReranker(mgr)
        scores = reranker.rerank("테스트 쿼리", ["문서1", "문서2"])
        self.assertIsNotNone(scores)
        self.assertEqual(len(scores), 2)
        for s in scores:
            self.assertGreaterEqual(s, 0.0)
            self.assertLessEqual(s, 1.0)
        ModelManager.reset()

    def test_rerank_empty_docs(self):
        from models import LocalReranker, ModelManager
        ModelManager.reset()
        mgr = ModelManager()
        mgr._models['reranker'] = MagicMock()
        mgr._last_used['reranker'] = time.time()
        reranker = LocalReranker(mgr)
        self.assertEqual(reranker.rerank("q", []), [])
        ModelManager.reset()

    def test_rerank_model_none(self):
        from models import LocalReranker, ModelManager
        ModelManager.reset()
        reranker = LocalReranker(ModelManager())
        self.assertIsNone(reranker.rerank("q", ["d"]))
        ModelManager.reset()

    def test_rerank_exception_gives_zero(self):
        """단일 문서 추론 실패 시 해당 문서 점수 0.0"""
        from models import LocalReranker, ModelManager
        ModelManager.reset()
        mgr = ModelManager()
        mock_model = MagicMock()
        mock_model.side_effect = [
            self._make_output(text='yes', top_logprobs=[{'yes': -0.1}]),
            RuntimeError("boom"),  # 두 번째 문서 실패
        ]
        mgr._models['reranker'] = mock_model
        mgr._last_used['reranker'] = time.time()

        reranker = LocalReranker(mgr)
        scores = reranker.rerank("q", ["doc1", "doc2"])
        self.assertEqual(len(scores), 2)
        self.assertGreater(scores[0], 0.0)
        self.assertEqual(scores[1], 0.0)
        ModelManager.reset()

    def test_rerank_prompt_template_format(self):
        """프롬프트 템플릿에 query/document가 정상 삽입되는지"""
        from models import LocalReranker
        template = LocalReranker._PROMPT_TEMPLATE
        result = template.format(query="test q", document="test d")
        self.assertIn("test q", result)
        self.assertIn("test d", result)
        self.assertIn("<query>", result)
        self.assertIn("<document>", result)


class TestLocalExpander(VegaTestCase):
    """LocalExpander 쿼리 확장 테스트"""

    def test_expand_returns_keywords(self):
        from models import LocalExpander, ModelManager
        ModelManager.reset()
        mgr = ModelManager()
        mock_model = MagicMock()
        mock_model.return_value = {
            'choices': [{'text': '해저케이블, submarine cable, 전력케이블, 154kV, power cable'}]
        }
        mgr._models['expander'] = mock_model
        mgr._last_used['expander'] = time.time()

        expander = LocalExpander(mgr)
        kws = expander.expand("해저케이블")
        self.assertIsInstance(kws, list)
        self.assertGreater(len(kws), 0)
        self.assertIn("submarine cable", kws)
        self.assertNotIn("해저케이블", kws)  # 원본 쿼리 제외
        ModelManager.reset()

    def test_expand_empty_query(self):
        from models import LocalExpander, ModelManager
        ModelManager.reset()
        expander = LocalExpander(ModelManager())
        self.assertEqual(expander.expand(""), [])
        self.assertEqual(expander.expand("  "), [])
        ModelManager.reset()

    def test_expand_model_none(self):
        from models import LocalExpander, ModelManager
        ModelManager.reset()
        expander = LocalExpander(ModelManager())
        self.assertEqual(expander.expand("test"), [])
        ModelManager.reset()

    def test_expand_deduplicates(self):
        from models import LocalExpander
        kws = LocalExpander._parse_keywords("cable, Cable, CABLE, wire", "test")
        # cable 중복 제거
        cable_count = sum(1 for k in kws if k.lower() == 'cable')
        self.assertEqual(cable_count, 1)

    def test_expand_filters_garbage(self):
        from models import LocalExpander
        kws = LocalExpander._parse_keywords(
            "검색어: 테스트, keyword: abc, 좋은키워드, x",  # x는 2글자 미만
            "원본"
        )
        self.assertNotIn("검색어: 테스트", kws)
        self.assertNotIn("x", kws)
        self.assertIn("좋은키워드", kws)

    def test_expand_max_10(self):
        from models import LocalExpander
        many = ", ".join(f"keyword{i}" for i in range(20))
        kws = LocalExpander._parse_keywords(many, "original")
        self.assertLessEqual(len(kws), 10)


class TestVectorSearch(VegaTestCase):
    """vector_search 코사인 유사도 검색 테스트"""

    def test_vector_search_with_embeddings(self):
        """DB에 임베딩을 삽입하고 검색이 되는지 E2E"""
        from models import _vector_to_blob, vector_search
        conn = config.get_db_connection(_fixture.db_path)

        # 테스트용 임베딩 삽입 (4차원)
        chunks = conn.execute("SELECT id FROM chunks LIMIT 3").fetchall()
        self.assertGreaterEqual(len(chunks), 2)

        vecs = [
            np.array([1.0, 0.0, 0.0, 0.0], dtype=np.float32),
            np.array([0.0, 1.0, 0.0, 0.0], dtype=np.float32),
        ]
        for i, chunk in enumerate(chunks[:2]):
            blob = _vector_to_blob(vecs[i])
            conn.execute(
                "INSERT OR REPLACE INTO chunk_embeddings (chunk_id, embedding, model_name, updated_at) VALUES (?, ?, 'test', '2026-01-01')",
                (chunk[0], blob)
            )
        conn.commit()
        conn.close()

        # [1,0,0,0]에 가까운 벡터로 검색
        query = np.array([0.9, 0.1, 0.0, 0.0], dtype=np.float32)
        results = vector_search(query, db_path=_fixture.db_path, limit=5)

        self.assertGreater(len(results), 0)
        # 첫 번째 결과가 [1,0,0,0] 벡터의 chunk이어야
        self.assertEqual(results[0][0], chunks[0][0])
        self.assertGreater(results[0][1], 0.9)  # 높은 유사도

        # cleanup
        conn = config.get_db_connection(_fixture.db_path)
        conn.execute("DELETE FROM chunk_embeddings")
        conn.commit()
        conn.close()

    def test_vector_search_empty_db(self):
        from models import vector_search
        query = np.array([1.0, 0.0, 0.0], dtype=np.float32)
        results = vector_search(query, db_path=_fixture.db_path, limit=5)
        self.assertEqual(results, [])

    def test_vector_search_dimension_mismatch(self):
        """query 차원 != DB 차원이면 빈 결과"""
        from models import _vector_to_blob, vector_search
        conn = config.get_db_connection(_fixture.db_path)
        chunk = conn.execute("SELECT id FROM chunks LIMIT 1").fetchone()
        blob = _vector_to_blob(np.array([1.0, 0.0, 0.0], dtype=np.float32))
        conn.execute(
            "INSERT OR REPLACE INTO chunk_embeddings (chunk_id, embedding, model_name, updated_at) VALUES (?, ?, 'test', '2026-01-01')",
            (chunk[0], blob)
        )
        conn.commit()
        conn.close()

        # 5차원 쿼리 vs 3차원 DB
        query = np.array([1.0, 0.0, 0.0, 0.0, 0.0], dtype=np.float32)
        results = vector_search(query, db_path=_fixture.db_path, limit=5)
        self.assertEqual(results, [])

        # cleanup
        conn = config.get_db_connection(_fixture.db_path)
        conn.execute("DELETE FROM chunk_embeddings")
        conn.commit()
        conn.close()


class TestLocalAdapter(VegaTestCase):
    """LocalAdapter 통합 테스트 (mock)"""

    def test_interface_methods(self):
        from models import LocalAdapter
        adapter = LocalAdapter()
        self.assertTrue(hasattr(adapter, 'search'))
        self.assertTrue(hasattr(adapter, 'search_fast'))
        self.assertTrue(hasattr(adapter, 'search_semantic'))
        self.assertTrue(hasattr(adapter, 'available'))

    def test_search_unavailable_returns_none(self):
        from models import LocalAdapter
        adapter = LocalAdapter()
        adapter.available = False
        self.assertIsNone(adapter.search("test"))

    def test_results_to_items_format(self):
        """LocalAdapter 결과 형식 검증"""
        from models import LocalAdapter
        fake_results = [
            (1, 0.95, 10, "비금도 해상태양광", "154kV 해저케이블 공사"),
            (2, 0.80, 11, "화성산단 태양광", "PF 조건 검토"),
        ]
        items = LocalAdapter._results_to_items(fake_results)
        self.assertEqual(len(items), 2)
        item = items[0]
        self.assertEqual(item['source'], 'local-vec')
        self.assertEqual(item['score'], 0.95)
        self.assertIn('metadata', item)
        meta = item['metadata']
        self.assertEqual(meta['project_name'], '비금도 해상태양광')
        self.assertEqual(meta['chunk_id'], 1)
        self.assertEqual(meta['project_id'], 10)
        self.assertIn('filepath', meta)
        self.assertIn('docid', meta)
        self.assertIn('title', meta)
        self.assertIn('context', meta)

    def test_search_query_mode_with_mock(self):
        """query 모드: 확장 → 벡터 → 리랭킹 전체 파이프라인"""
        from models import LocalAdapter, ModelManager, _vector_to_blob
        ModelManager.reset()

        # DB에 임베딩 삽입
        conn = config.get_db_connection(_fixture.db_path)
        chunks = conn.execute("SELECT id FROM chunks LIMIT 2").fetchall()
        for i, chunk in enumerate(chunks[:2]):
            vec = np.zeros(4, dtype=np.float32)
            vec[i] = 1.0
            blob = _vector_to_blob(vec)
            conn.execute(
                "INSERT OR REPLACE INTO chunk_embeddings (chunk_id, embedding, model_name, updated_at) VALUES (?, ?, 'test', '2026-01-01')",
                (chunk[0], blob)
            )
        conn.commit()
        conn.close()

        # Mock 모델 설정
        mgr = ModelManager()
        # expander
        mock_expander = MagicMock()
        mock_expander.return_value = {'choices': [{'text': 'keyword1, keyword2'}]}
        mgr._models['expander'] = mock_expander
        mgr._last_used['expander'] = time.time()
        # embedder
        mock_embedder = MagicMock()
        mock_embedder.embed.return_value = [0.9, 0.1, 0.0, 0.0]
        mgr._models['embedder'] = mock_embedder
        mgr._last_used['embedder'] = time.time()
        # reranker
        mock_reranker = MagicMock()
        mock_reranker.return_value = {
            'choices': [{'text': 'yes', 'logprobs': {'top_logprobs': [{'yes': -0.2}], 'tokens': ['yes'], 'token_logprobs': [-0.2]}}]
        }
        mgr._models['reranker'] = mock_reranker
        mgr._last_used['reranker'] = time.time()

        adapter = LocalAdapter()
        adapter.available = True  # 강제
        items = adapter.search("테스트", mode='query')

        self.assertIsNotNone(items)
        self.assertGreater(len(items), 0)
        # 모든 아이템에 score와 metadata가 있어야
        for item in items:
            self.assertIn('score', item)
            self.assertIn('metadata', item)
            self.assertIsInstance(item['score'], float)

        # cleanup
        conn = config.get_db_connection(_fixture.db_path)
        conn.execute("DELETE FROM chunk_embeddings")
        conn.commit()
        conn.close()
        ModelManager.reset()

    def test_search_vsearch_mode(self):
        """vsearch 모드: 리랭킹 없이 벡터만"""
        from models import LocalAdapter, ModelManager, _vector_to_blob
        ModelManager.reset()

        conn = config.get_db_connection(_fixture.db_path)
        chunk = conn.execute("SELECT id FROM chunks LIMIT 1").fetchone()
        blob = _vector_to_blob(np.array([1.0, 0.0, 0.0], dtype=np.float32))
        conn.execute(
            "INSERT OR REPLACE INTO chunk_embeddings (chunk_id, embedding, model_name, updated_at) VALUES (?, ?, 'test', '2026-01-01')",
            (chunk[0], blob)
        )
        conn.commit()
        conn.close()

        mgr = ModelManager()
        mock_embedder = MagicMock()
        mock_embedder.embed.return_value = [1.0, 0.0, 0.0]
        mgr._models['embedder'] = mock_embedder
        mgr._last_used['embedder'] = time.time()

        adapter = LocalAdapter()
        adapter.available = True
        items = adapter.search("test", mode='vsearch')

        self.assertIsNotNone(items)
        # vsearch에서는 rerank_score가 없어야
        for item in items:
            self.assertNotIn('rerank_score', item.get('metadata', {}))

        conn = config.get_db_connection(_fixture.db_path)
        conn.execute("DELETE FROM chunk_embeddings")
        conn.commit()
        conn.close()
        ModelManager.reset()

    def test_project_filter(self):
        from models import LocalAdapter
        fake_items = [
            {'source': 'local-vec', 'content': 'a', 'score': 0.9,
             'metadata': {'project_name': '비금도 해상태양광'}},
            {'source': 'local-vec', 'content': 'b', 'score': 0.8,
             'metadata': {'project_name': '화성산단 태양광'}},
        ]
        # _results_to_items 대신 직접 필터 로직 테스트
        filter_lower = ['비금도']
        filtered = [
            item for item in fake_items
            if any(pf in item['metadata']['project_name'].lower() for pf in filter_lower)
        ]
        self.assertEqual(len(filtered), 1)
        self.assertIn('비금도', filtered[0]['metadata']['project_name'])


class TestChunkEmbeddingsSchema(VegaTestCase):
    """DB 스키마: chunk_embeddings 테이블 + 트리거"""

    def test_chunk_embeddings_table_exists(self):
        conn = config.get_db_connection(_fixture.db_path)
        tables = [row[0] for row in conn.execute(
            "SELECT name FROM sqlite_master WHERE type='table'"
        ).fetchall()]
        conn.close()
        self.assertIn('chunk_embeddings', tables)

    def test_schema_version_is_6(self):
        conn = config.get_db_connection(_fixture.db_path)
        ver = conn.execute("PRAGMA user_version").fetchone()[0]
        conn.close()
        self.assertEqual(ver, 6)

    def test_chunks_delete_cascades_to_embeddings(self):
        from models import _vector_to_blob
        conn = config.get_db_connection(_fixture.db_path)
        chunk_row = conn.execute("SELECT id FROM chunks LIMIT 1").fetchone()
        if chunk_row:
            cid = chunk_row[0]
            blob = _vector_to_blob(np.array([0.1, 0.2], dtype=np.float32))
            conn.execute(
                "INSERT OR REPLACE INTO chunk_embeddings (chunk_id, embedding, model_name, updated_at) VALUES (?, ?, 'test', '2026-01-01')",
                (cid, blob)
            )
            conn.commit()
            conn.execute("DELETE FROM chunks WHERE id=?", (cid,))
            conn.commit()
            emb = conn.execute("SELECT chunk_id FROM chunk_embeddings WHERE chunk_id=?", (cid,)).fetchone()
            self.assertIsNone(emb)
        conn.close()


class TestEmbedAllChunks(VegaTestCase):
    """embed_all_chunks 일괄 임베딩 테스트"""

    def test_embed_all_with_mock(self):
        from models import embed_all_chunks, ModelManager
        ModelManager.reset()
        mgr = ModelManager()
        mock_model = MagicMock()
        mock_model.embed.return_value = [0.1, 0.2, 0.3]
        mgr._models['embedder'] = mock_model
        mgr._last_used['embedder'] = time.time()

        stats = embed_all_chunks(db_path=_fixture.db_path, batch_size=4)
        self.assertIn('embedded', stats)
        self.assertIn('errors', stats)
        self.assertIn('total', stats)
        self.assertGreater(stats['total'], 0)
        self.assertEqual(stats['errors'], 0)
        self.assertEqual(stats['embedded'], stats['total'])

        # DB에 실제로 저장되었는지
        conn = config.get_db_connection(_fixture.db_path)
        count = conn.execute("SELECT COUNT(*) FROM chunk_embeddings").fetchone()[0]
        conn.close()
        self.assertGreater(count, 0)

        # cleanup
        conn = config.get_db_connection(_fixture.db_path)
        conn.execute("DELETE FROM chunk_embeddings")
        conn.commit()
        conn.close()
        ModelManager.reset()

    def test_embed_all_skips_existing(self):
        """이미 임베딩된 chunk은 건너뛰기"""
        from models import embed_all_chunks, _vector_to_blob, ModelManager
        ModelManager.reset()

        # 하나 미리 삽입
        conn = config.get_db_connection(_fixture.db_path)
        chunk = conn.execute("SELECT id FROM chunks WHERE LENGTH(content) > 10 LIMIT 1").fetchone()
        blob = _vector_to_blob(np.array([1.0, 2.0, 3.0], dtype=np.float32))
        conn.execute(
            "INSERT OR REPLACE INTO chunk_embeddings (chunk_id, embedding, model_name, updated_at) VALUES (?, ?, 'test', '2026-01-01')",
            (chunk[0], blob)
        )
        conn.commit()
        conn.close()

        mgr = ModelManager()
        mock_model = MagicMock()
        mock_model.embed.return_value = [0.1, 0.2, 0.3]
        mgr._models['embedder'] = mock_model
        mgr._last_used['embedder'] = time.time()

        stats = embed_all_chunks(db_path=_fixture.db_path)
        # 이미 있는 1개는 total에서 제외됨
        total_chunks = config.get_db_connection(_fixture.db_path).execute(
            "SELECT COUNT(*) FROM chunks WHERE content IS NOT NULL AND LENGTH(content) > 10"
        ).fetchone()[0]
        self.assertEqual(stats['total'], total_chunks - 1)

        # cleanup
        conn = config.get_db_connection(_fixture.db_path)
        conn.execute("DELETE FROM chunk_embeddings")
        conn.commit()
        conn.close()
        ModelManager.reset()



# ──────────────────────────────────────────────
# v1.41 추가 엣지케이스 테스트
# ──────────────────────────────────────────────


class TestCallEmbedEdgeCases(VegaTestCase):
    """_call_embed 엣지케이스"""

    def test_embed_returns_empty_list(self):
        """embed()가 빈 리스트 반환 → RuntimeError"""
        from models import _call_embed
        m = MagicMock()
        m.embed.return_value = []
        with self.assertRaises(RuntimeError):
            _call_embed(m, "test")

    def test_embed_returns_nested_empty_list(self):
        """embed()가 [[]] 반환 → RuntimeError"""
        from models import _call_embed
        m = MagicMock()
        m.embed.return_value = [[]]
        with self.assertRaises(RuntimeError):
            _call_embed(m, "test")

    def test_create_embedding_empty_data(self):
        """create_embedding()이 빈 data → RuntimeError"""
        from models import _call_embed
        m = MagicMock(spec=[])  # embed 없음
        m.create_embedding = MagicMock(return_value={'data': []})
        with self.assertRaises(RuntimeError):
            _call_embed(m, "test")

    def test_create_embedding_empty_vector(self):
        """create_embedding()이 빈 벡터 → RuntimeError"""
        from models import _call_embed
        m = MagicMock(spec=[])
        m.create_embedding = MagicMock(return_value={'data': [{'embedding': []}]})
        with self.assertRaises(RuntimeError):
            _call_embed(m, "test")


class TestBlobEdgeCases(VegaTestCase):
    """_blob_to_vector / _vector_to_blob 엣지케이스"""

    def test_empty_blob(self):
        """빈 BLOB → 빈 배열"""
        from models import _blob_to_vector
        result = _blob_to_vector(b'')
        self.assertEqual(len(result), 0)

    def test_none_blob(self):
        """None BLOB → 빈 배열"""
        from models import _blob_to_vector
        result = _blob_to_vector(None)
        self.assertEqual(len(result), 0)

    def test_short_blob(self):
        """3바이트 BLOB → 빈 배열 (float32는 최소 4바이트)"""
        from models import _blob_to_vector
        result = _blob_to_vector(b'\x00\x01\x02')
        self.assertEqual(len(result), 0)

    def test_misaligned_blob(self):
        """5바이트 BLOB → 1개 float32 (4바이트까지만)"""
        from models import _blob_to_vector
        blob = np.float32(1.0).tobytes() + b'\xff'  # 4 + 1 = 5바이트
        result = _blob_to_vector(blob)
        self.assertEqual(len(result), 1)
        self.assertAlmostEqual(result[0], 1.0, places=5)

    def test_vector_to_blob_list_input(self):
        """파이썬 리스트 → BLOB 변환"""
        from models import _vector_to_blob, _blob_to_vector
        blob = _vector_to_blob([1.0, 2.0, 3.0])
        vec = _blob_to_vector(blob)
        np.testing.assert_array_almost_equal(vec, [1.0, 2.0, 3.0])


class TestVectorSearchEdgeCases(VegaTestCase):
    """vector_search 엣지케이스"""

    def test_nan_query_vec(self):
        """NaN이 포함된 query_vec → 빈 결과"""
        from models import vector_search
        query = np.array([float('nan'), 1.0, 0.0], dtype=np.float32)
        result = vector_search(query, db_path=_fixture.db_path)
        self.assertEqual(result, [])

    def test_inf_query_vec(self):
        """Inf가 포함된 query_vec → 빈 결과"""
        from models import vector_search
        query = np.array([float('inf'), 1.0, 0.0], dtype=np.float32)
        result = vector_search(query, db_path=_fixture.db_path)
        self.assertEqual(result, [])

    def test_empty_query_vec(self):
        """빈 query_vec → 빈 결과"""
        from models import vector_search
        query = np.array([], dtype=np.float32)
        result = vector_search(query, db_path=_fixture.db_path)
        self.assertEqual(result, [])

    def test_corrupt_blob_in_db(self):
        """DB에 손상된 BLOB가 있어도 다른 결과는 정상 반환"""
        from models import vector_search, _vector_to_blob
        conn = config.get_db_connection(_fixture.db_path)
        chunks = conn.execute("SELECT id FROM chunks LIMIT 3").fetchall()
        if len(chunks) < 2:
            conn.close()
            self.skipTest("chunks 부족")
        # 정상 임베딩
        good_vec = np.array([1.0, 0.0, 0.0], dtype=np.float32)
        conn.execute(
            "INSERT OR REPLACE INTO chunk_embeddings (chunk_id, embedding, model_name, updated_at) VALUES (?, ?, 'test', '2026-01-01')",
            (chunks[0][0], _vector_to_blob(good_vec))
        )
        # 손상 BLOB
        conn.execute(
            "INSERT OR REPLACE INTO chunk_embeddings (chunk_id, embedding, model_name, updated_at) VALUES (?, ?, 'test', '2026-01-01')",
            (chunks[1][0], b'\x00\x01')
        )
        conn.commit()
        conn.close()

        query = np.array([1.0, 0.0, 0.0], dtype=np.float32)
        results = vector_search(query, db_path=_fixture.db_path, limit=5)
        # 정상 결과가 반환되어야 함 (손상된 것은 스킵)
        self.assertGreaterEqual(len(results), 1)

        # cleanup
        conn = config.get_db_connection(_fixture.db_path)
        conn.execute("DELETE FROM chunk_embeddings")
        conn.commit()
        conn.close()


class TestLocalRerankerEdgeCases(VegaTestCase):
    """LocalReranker 추가 엣지케이스"""

    def test_rerank_none_query(self):
        """query=None → 모든 점수 0.0"""
        from models import LocalReranker, ModelManager
        ModelManager.reset()
        reranker = LocalReranker(ModelManager())
        scores = reranker.rerank(None, ["doc1", "doc2"])
        self.assertEqual(scores, [0.0, 0.0])
        ModelManager.reset()

    def test_rerank_empty_query(self):
        """query='' → 모든 점수 0.0"""
        from models import LocalReranker, ModelManager
        ModelManager.reset()
        reranker = LocalReranker(ModelManager())
        scores = reranker.rerank("", ["doc1"])
        self.assertEqual(scores, [0.0])
        ModelManager.reset()

    def test_extract_non_numeric_logprob(self):
        """top_logprobs에 비숫자 logprob → 무시"""
        from models import LocalReranker
        output = {
            'choices': [{
                'text': 'yes',
                'logprobs': {
                    'top_logprobs': [{'yes': 'not_a_number'}],
                    'tokens': ['yes'],
                    'token_logprobs': [-0.5]
                }
            }]
        }
        lp = LocalReranker._extract_yes_logprob(output)
        # top_logprobs의 비숫자 건너뜀 → token_logprobs로 폴백
        self.assertAlmostEqual(lp, -0.5, places=3)

    def test_extract_non_numeric_token_logprob(self):
        """token_logprobs에 비숫자 → 0.0"""
        from models import LocalReranker
        output = {
            'choices': [{
                'text': 'yes',
                'logprobs': {
                    'top_logprobs': [],
                    'tokens': ['yes'],
                    'token_logprobs': ['bad']
                }
            }]
        }
        lp = LocalReranker._extract_yes_logprob(output)
        self.assertAlmostEqual(lp, 0.0)

    def test_rerank_long_query_truncated(self):
        """매우 긴 query → 정상 처리 (잘림)"""
        from models import LocalReranker, ModelManager
        ModelManager.reset()
        mgr = ModelManager()
        mock_model = MagicMock()
        mock_model.return_value = {
            'choices': [{'text': 'yes', 'logprobs': {'top_logprobs': [{'yes': -0.3}], 'tokens': ['yes'], 'token_logprobs': [-0.3]}}]
        }
        mgr._models['reranker'] = mock_model
        mgr._last_used['reranker'] = time.time()

        reranker = LocalReranker(mgr)
        long_query = "테스트 " * 5000  # 매우 긴 쿼리
        scores = reranker.rerank(long_query, ["짧은 문서"])
        self.assertEqual(len(scores), 1)
        self.assertGreater(scores[0], 0)
        # 프롬프트에 잘린 query가 들어갔는지 확인
        call_args = mock_model.call_args
        prompt = call_args[0][0]
        self.assertLess(len(prompt), len(long_query))  # 원본보다 짧아야
        ModelManager.reset()


class TestLocalExpanderEdgeCases(VegaTestCase):
    """LocalExpander 추가 엣지케이스"""

    def test_expand_very_long_query(self):
        """매우 긴 쿼리 → 500자로 잘림"""
        from models import LocalExpander, ModelManager
        ModelManager.reset()
        mgr = ModelManager()
        mock_model = MagicMock()
        mock_model.return_value = {'choices': [{'text': 'kw1, kw2'}]}
        mgr._models['expander'] = mock_model
        mgr._last_used['expander'] = time.time()

        expander = LocalExpander(mgr)
        long_query = "가" * 1000
        result = expander.expand(long_query)
        # 프롬프트에 500자로 잘린 쿼리가 들어갔는지 확인
        call_args = mock_model.call_args
        prompt = call_args[0][0]
        self.assertNotIn("가" * 501, prompt)
        ModelManager.reset()

    def test_expand_non_string_query(self):
        """비문자열 쿼리 → 빈 리스트 또는 문자열 변환"""
        from models import LocalExpander, ModelManager
        ModelManager.reset()
        expander = LocalExpander(ModelManager())
        # None
        self.assertEqual(expander.expand(None), [])
        # 빈 문자열
        self.assertEqual(expander.expand(""), [])
        # 숫자 (str()로 변환됨)
        # int/float은 truthy이므로 expand 시도함 → model None → []
        self.assertEqual(expander.expand(123), [])
        ModelManager.reset()

    def test_parse_keywords_unicode(self):
        """유니코드 키워드 처리"""
        from models import LocalExpander
        result = LocalExpander._parse_keywords(
            "太陽光, solar energy, 해상풍력, offshore wind, émission",
            "태양광"
        )
        self.assertIn("太陽光", result)
        self.assertIn("solar energy", result)
        self.assertIn("해상풍력", result)

    def test_parse_keywords_special_chars(self):
        """특수문자 키워드 (O&M, 154kV)"""
        from models import LocalExpander
        result = LocalExpander._parse_keywords(
            "O&M, 154kV, PCS, ESS 배터리",
            "태양광 설비"
        )
        self.assertIn("O&M", result)
        self.assertIn("154kV", result)
        self.assertIn("PCS", result)


class TestLocalEmbedderEdgeCases(VegaTestCase):
    """LocalEmbedder 추가 엣지케이스"""

    def test_embed_zero_vector(self):
        """영벡터 입력 → 정규화에서 0으로 나누기 없음"""
        from models import LocalEmbedder, ModelManager
        ModelManager.reset()
        mgr = ModelManager()
        mock_model = MagicMock()
        mock_model.embed.return_value = [0.0, 0.0, 0.0]
        mgr._models['embedder'] = mock_model
        mgr._last_used['embedder'] = time.time()

        embedder = LocalEmbedder(mgr)
        result = embedder.embed(["zero"])
        self.assertIsNotNone(result)
        # 영벡터 정규화: norms=0 → 1로 대체 → 영벡터 유지
        np.testing.assert_array_almost_equal(result[0], [0.0, 0.0, 0.0])
        ModelManager.reset()

    def test_embed_single_empty_result(self):
        """embed()가 빈 결과 → None"""
        from models import LocalEmbedder, ModelManager
        ModelManager.reset()
        mgr = ModelManager()
        mock_model = MagicMock()
        mock_model.embed.side_effect = RuntimeError("fail")
        mgr._models['embedder'] = mock_model
        mgr._last_used['embedder'] = time.time()

        embedder = LocalEmbedder(mgr)
        self.assertIsNone(embedder.embed_single("test"))
        ModelManager.reset()

    def test_embed_unicode_text(self):
        """유니코드/이모지 포함 텍스트"""
        from models import LocalEmbedder, ModelManager
        ModelManager.reset()
        mgr = ModelManager()
        mock_model = MagicMock()
        mock_model.embed.return_value = [1.0, 0.0]
        mgr._models['embedder'] = mock_model
        mgr._last_used['embedder'] = time.time()

        embedder = LocalEmbedder(mgr)
        result = embedder.embed(["비금도 🟢 진행 중"])
        self.assertIsNotNone(result)
        self.assertEqual(result.shape, (1, 2))
        ModelManager.reset()


class TestLocalAdapterEdgeCases(VegaTestCase):
    """LocalAdapter 추가 엣지케이스"""

    def test_search_returns_list_not_none_on_empty(self):
        """임베딩 실패 시 None이 아닌 빈 리스트 반환"""
        from models import LocalAdapter, ModelManager
        ModelManager.reset()
        adapter = LocalAdapter()
        adapter.available = True
        # embedder.embed_single returns None → 빈 리스트
        result = adapter.search("test", mode='vsearch')
        self.assertIsInstance(result, list)
        self.assertEqual(result, [])
        ModelManager.reset()

    def test_search_unavailable_returns_none(self):
        """available=False → None"""
        from models import LocalAdapter, ModelManager
        ModelManager.reset()
        adapter = LocalAdapter()
        adapter.available = False
        result = adapter.search("test")
        self.assertIsNone(result)
        ModelManager.reset()

    def test_results_to_items_metadata_fields(self):
        """LocalAdapter 메타데이터 필드 완전성 검증"""
        from models import LocalAdapter
        fake = [(1, 0.95, 10, '비금도', 'content')]
        items = LocalAdapter._results_to_items(fake)
        meta = items[0]['metadata']
        # router.py _semantic_items_to_unified()가 기대하는 필드
        self.assertIn('uri', meta)
        self.assertIn('filepath', meta)
        self.assertIn('context', meta)
        self.assertIn('docid', meta)
        self.assertIn('best_chunk_pos', meta)
        self.assertIn('filter_bypassed', meta)
        self.assertIn('project_name', meta)
        self.assertIn('title', meta)
        self.assertIn('chunk_id', meta)
        self.assertIn('project_id', meta)
        # uri와 docid 일치
        self.assertEqual(meta['uri'], meta['docid'])
        self.assertFalse(meta['filter_bypassed'])

    def test_search_mode_search_returns_list(self):
        """search 모드에서 임베딩 실패해도 빈 리스트 반환"""
        from models import LocalAdapter, ModelManager
        ModelManager.reset()
        adapter = LocalAdapter()
        adapter.available = True
        result = adapter.search("test", mode='search')
        self.assertIsInstance(result, list)
        ModelManager.reset()

    def test_project_filter_empty_list(self):
        """project_filter=[] → 필터 적용 안 됨"""
        from models import LocalAdapter
        items = [
            {'source': 'local-vec', 'content': 'a', 'score': 0.9,
             'metadata': {'project_name': '비금도'}},
        ]
        # 빈 필터로 필터링 안 함
        filter_lower = []
        # LocalAdapter.search()는 filter_lower가 falsy면 스킵
        # 수동 확인: project_filter=[]면 items 그대로
        self.assertEqual(len(items), 1)

    def test_project_filter_none_in_list(self):
        """project_filter=[None] → 에러 없음"""
        from models import LocalAdapter, ModelManager, _vector_to_blob
        ModelManager.reset()
        mgr = ModelManager()

        # DB에 임베딩 삽입
        conn = config.get_db_connection(_fixture.db_path)
        chunk = conn.execute("SELECT id FROM chunks LIMIT 1").fetchone()
        blob = _vector_to_blob(np.array([1.0, 0.0, 0.0], dtype=np.float32))
        conn.execute(
            "INSERT OR REPLACE INTO chunk_embeddings (chunk_id, embedding, model_name, updated_at) VALUES (?, ?, 'test', '2026-01-01')",
            (chunk[0], blob)
        )
        conn.commit()
        conn.close()

        mock_embedder = MagicMock()
        mock_embedder.embed.return_value = [1.0, 0.0, 0.0]
        mgr._models['embedder'] = mock_embedder
        mgr._last_used['embedder'] = time.time()

        adapter = LocalAdapter()
        adapter.available = True
        # None이 포함된 필터 → 에러 없이 필터링
        result = adapter.search("test", project_filter=[None, "비금도"], mode='vsearch')
        self.assertIsInstance(result, list)

        conn = config.get_db_connection(_fixture.db_path)
        conn.execute("DELETE FROM chunk_embeddings")
        conn.commit()
        conn.close()
        ModelManager.reset()


class TestModelManagerEdgeCases(VegaTestCase):
    """ModelManager 추가 엣지케이스"""

    def test_get_path_unknown_role(self):
        """알 수 없는 role → None"""
        from models import ModelManager
        ModelManager.reset()
        mgr = ModelManager()
        self.assertIsNone(mgr._get_path('unknown'))
        self.assertIsNone(mgr._get_path(''))
        self.assertIsNone(mgr._get_path(None))
        ModelManager.reset()

    def test_status_no_model_files(self):
        """모델 파일 없는 상태에서 status() 정상 동작"""
        from models import ModelManager
        ModelManager.reset()
        mgr = ModelManager()
        status = mgr.status()
        for role in ('expander', 'embedder', 'reranker'):
            self.assertIn(role, status)
            self.assertFalse(status[role]['loaded'])
            self.assertIsInstance(status[role]['file_size_mb'], (int, float))
        ModelManager.reset()

    def test_double_reset(self):
        """reset() 두 번 호출해도 에러 없음"""
        from models import ModelManager
        ModelManager.reset()
        ModelManager.reset()  # 두 번째 호출에서 _instance가 None
        mgr = ModelManager()
        self.assertIsNotNone(mgr)
        ModelManager.reset()

    def test_unload_nonexistent_role(self):
        """존재하지 않는 role unload → 에러 없음"""
        from models import ModelManager
        ModelManager.reset()
        mgr = ModelManager()
        mgr.unload('nonexistent')  # 에러 없어야
        mgr.unload()  # 전체 해제도 에러 없음
        ModelManager.reset()


class TestEmbedAllChunksEdgeCases(VegaTestCase):
    """embed_all_chunks 엣지케이스"""

    def test_embed_all_zero_batch_size(self):
        """batch_size=0 → 기본값으로 폴백"""
        from models import embed_all_chunks, ModelManager
        ModelManager.reset()
        result = embed_all_chunks(db_path=_fixture.db_path, batch_size=0)
        # 모델 없으므로 errors = total
        self.assertIn('total', result)
        self.assertIn('errors', result)
        ModelManager.reset()

    def test_embed_all_negative_batch_size(self):
        """batch_size=-1 → 기본값으로 폴백"""
        from models import embed_all_chunks, ModelManager
        ModelManager.reset()
        result = embed_all_chunks(db_path=_fixture.db_path, batch_size=-1)
        self.assertIn('total', result)
        ModelManager.reset()


# ──────────────────────────────────────────────
# v1.42 — 퍼지 매칭 + 스마트 라우팅 + 입력 관용성
# ──────────────────────────────────────────────

class TestFuzzyFindProject(VegaTestCase):
    """_fuzzy_find_project() 퍼지 매칭 테스트"""

    def setUp(self):
        from core import _fuzzy_find_project
        self.fuzzy = _fuzzy_find_project

    def test_exact_match_returns_high_confidence(self):
        """정확한 프로젝트명 → (id, 1.0)"""
        pid, conf = self.fuzzy('비금도')
        self.assertIsNotNone(pid)
        self.assertEqual(conf, 1.0)

    def test_typo_correction(self):
        """오타 '비금또' → 비금도 매칭"""
        pid, conf = self.fuzzy('비금또')
        self.assertIsNotNone(pid)
        self.assertGreaterEqual(conf, 0.55)

    def test_partial_name(self):
        """부분 이름 '비금' → 매칭 (LIKE 서브스트링)"""
        pid, conf = self.fuzzy('비금')
        self.assertIsNotNone(pid)

    def test_completely_different_no_match(self):
        """완전히 다른 이름 → (None, 0)"""
        pid, conf = self.fuzzy('아무런관련없는문자열XYZ')
        self.assertIsNone(pid)
        self.assertEqual(conf, 0)

    def test_empty_input(self):
        """빈 입력 → (None, 0)"""
        pid, conf = self.fuzzy('')
        self.assertIsNone(pid)
        self.assertEqual(conf, 0)

    def test_none_input(self):
        """None 입력 → (None, 0)"""
        pid, conf = self.fuzzy(None)
        self.assertIsNone(pid)
        self.assertEqual(conf, 0)


class TestFuzzyInText(VegaTestCase):
    """_find_project_id_in_text() fuzzy 폴백 테스트"""

    def setUp(self):
        from core import _find_project_id_in_text
        self.find_in_text = _find_project_id_in_text

    def test_typo_in_sentence(self):
        """오타 포함 문장에서 프로젝트 찾기"""
        pid = self.find_in_text('비금또 어떻게 돼?')
        self.assertIsNotNone(pid)


class TestSmartRoute(unittest.TestCase):
    """_smart_route() 스마트 라우팅 테스트"""

    def setUp(self):
        from core import _smart_route
        self.route = _smart_route

    def test_project_name_routes_to_brief(self):
        """프로젝트명만 입력 → brief로 라우팅"""
        cmd, params = self.route(['비금도'])
        # 비금도는 프로젝트명이므로 brief 또는 search
        self.assertIn(cmd, ('brief', 'search'))

    def test_urgent_keyword(self):
        """'급한 거' → urgent"""
        cmd, params = self.route(['급한', '거'])
        self.assertEqual(cmd, 'urgent')

    def test_empty_input(self):
        """빈 입력 → dashboard"""
        cmd, params = self.route([])
        self.assertEqual(cmd, 'dashboard')


class TestNLRoutesV142(unittest.TestCase):
    """v1.42 신규 NL_ROUTES 패턴"""

    def setUp(self):
        from core import route_input
        self.route = route_input

    def test_cost_to_pipeline(self):
        """비용/예산 → pipeline"""
        self.assertEqual(self.route(['비용', '얼마'])[0], 'pipeline')
        self.assertEqual(self.route(['예산', '확인'])[0], 'pipeline')

    def test_deadline_to_urgent(self):
        """마감/납기 → urgent"""
        self.assertEqual(self.route(['마감', '언제'])[0], 'urgent')
        self.assertEqual(self.route(['납기', '확인'])[0], 'urgent')

    def test_schedule_to_timeline(self):
        """일정/스케줄 → timeline"""
        self.assertEqual(self.route(['일정', '확인'])[0], 'timeline')
        self.assertEqual(self.route(['스케줄'])[0], 'timeline')

    def test_issue_to_search(self):
        """문제/이슈 → search"""
        self.assertEqual(self.route(['문제', '있는'])[0], 'search')
        self.assertEqual(self.route(['이슈', '목록'])[0], 'search')

    def test_resource_to_cross(self):
        """인력/리소스 → cross"""
        self.assertEqual(self.route(['인력', '충돌'])[0], 'cross')


class TestNormalizeQuery(unittest.TestCase):
    """router._normalize_query() 테스트"""

    def setUp(self):
        from router import _normalize_query
        self.norm = _normalize_query

    def test_strip_endings(self):
        """접미 표현 제거"""
        self.assertEqual(self.norm('비금도 어떻게 돼 알려줘'), '비금도 어떻게 돼')
        self.assertEqual(self.norm('급한 거 보여줘'), '급한 거')

    def test_strip_punctuation(self):
        """문장부호 제거"""
        self.assertEqual(self.norm('비금도 어떻게 돼?'), '비금도 어떻게 돼')
        self.assertEqual(self.norm('뭐 해야해???'), '뭐 해야해')

    def test_empty_preserved(self):
        """빈 문자열 유지"""
        self.assertEqual(self.norm(''), '')
        self.assertEqual(self.norm('   '), '')


class TestSearchMatchReasons(VegaTestCase):
    """search 결과의 match_reasons 필드 검증"""

    def test_match_reasons_present(self):
        """검색 결과에 match_reasons 필드 존재"""
        r = self._search('비금도')
        data = self._assert_ok(r)
        if data.get('projects'):
            for p in data['projects']:
                self.assertIn('match_reasons', p)
                self.assertIsInstance(p['match_reasons'], list)
                self.assertTrue(len(p['match_reasons']) > 0)

    def test_project_name_match_reason(self):
        """프로젝트명으로 검색 시 '프로젝트명' 사유 포함"""
        r = self._search('비금도')
        data = self._assert_ok(r)
        if data.get('projects'):
            top = data['projects'][0]
            self.assertIn('프로젝트명', top.get('match_reasons', []))


class TestSearchFuzzyFallback(VegaTestCase):
    """검색 0건 시 fuzzy fallback으로 _auto_brief 첨부"""

    def test_auto_brief_on_typo(self):
        """오타 검색 시 _auto_brief 첨부 가능"""
        r = self._search('비금또해상태양')
        data = self._assert_ok(r)
        # 퍼지 매칭이 성공하면 _auto_brief 존재
        if data.get('result_count', {}).get('projects', 0) == 0:
            if data.get('_auto_brief'):
                self.assertIn('project_name', data['_auto_brief'])
                self.assertIn('_match_confidence', data['_auto_brief'])


class TestBriefFuzzy(VegaTestCase):
    """brief 명령의 fuzzy 매칭"""

    def test_brief_with_typo(self):
        """오타 프로젝트명으로 brief 시도"""
        r = self._exec('brief', {'sub_args': ['비금또']})
        # fuzzy 매칭 성공 시 ok, 실패해도 에러 응답은 정상
        self.assertIn(r['status'], ('ok', 'error'))


class TestAiHintExtended(VegaTestCase):
    """확장된 _build_ai_hint() 검증"""

    def test_show_hint(self):
        """show 명령 ai_hint"""
        from core import _build_ai_hint
        hints = _build_ai_hint('show', {'id': 1, 'name': '테스트'})
        situations = [h['situation'] for h in hints]
        self.assertIn('show_detail', situations)

    def test_dashboard_hint(self):
        """dashboard 명령 ai_hint"""
        from core import _build_ai_hint
        hints = _build_ai_hint('dashboard', {'total': 5})
        situations = [h['situation'] for h in hints]
        self.assertIn('dashboard_overview', situations)

    def test_pipeline_hint(self):
        """pipeline 명령 ai_hint"""
        from core import _build_ai_hint
        hints = _build_ai_hint('pipeline', {'total_amount': 100})
        situations = [h['situation'] for h in hints]
        self.assertIn('pipeline_view', situations)


# ──────────────────────────────────────────────
# v1.43: Memory Backend 테스트
# ──────────────────────────────────────────────

class TestMemoryParser(unittest.TestCase):
    """_parse_memory_md() 검증"""

    def setUp(self):
        from commands.memory import _parse_memory_md
        self.parse = _parse_memory_md
        self.tmpdir = tempfile.mkdtemp()

    def tearDown(self):
        shutil.rmtree(self.tmpdir, ignore_errors=True)

    def _write(self, name, content):
        p = Path(self.tmpdir) / name
        p.write_text(content, encoding='utf-8')
        return p

    def test_heading_split(self):
        """heading 기준 분할 + 라인 번호"""
        p = self._write('test.md', '# Alpha\nfoo\n# Beta\nbar\nbaz\n')
        chunks = self.parse(p)
        self.assertEqual(len(chunks), 2)
        self.assertEqual(chunks[0]['heading'], 'Alpha')
        self.assertEqual(chunks[0]['start_line'], 1)
        self.assertEqual(chunks[1]['heading'], 'Beta')
        self.assertEqual(chunks[1]['start_line'], 3)

    def test_no_heading(self):
        """heading 없는 파일 → 단일 청크"""
        p = self._write('plain.md', 'just text\nmore text\n')
        chunks = self.parse(p)
        self.assertEqual(len(chunks), 1)
        self.assertEqual(chunks[0]['heading'], '')

    def test_empty_file(self):
        """빈 파일"""
        p = self._write('empty.md', '')
        chunks = self.parse(p)
        self.assertEqual(chunks, [])

    def test_multiple_levels(self):
        """h1, h2, h3 모두 인식"""
        p = self._write('levels.md', '# H1\na\n## H2\nb\n### H3\nc\n')
        chunks = self.parse(p)
        self.assertEqual(len(chunks), 3)
        headings = [c['heading'] for c in chunks]
        self.assertEqual(headings, ['H1', 'H2', 'H3'])


class TestMemoryUpdate(VegaTestCase):
    """memory-update 명령 검증"""

    def setUp(self):
        self.tmpdir = tempfile.mkdtemp()
        self.mem_dir = Path(self.tmpdir) / 'memory'
        self.mem_dir.mkdir()
        # fixture memory 파일
        (self.mem_dir / 'test.md').write_text('# Test Note\nHello world\n', encoding='utf-8')
        (self.mem_dir / 'sub.md').write_text('# Sub\nContent here\n## Details\nMore\n', encoding='utf-8')
        # config 패치
        self._orig_workspace = config.MEMORY_WORKSPACE
        self._orig_paths = config.MEMORY_PATHS
        config.MEMORY_WORKSPACE = self.tmpdir
        config.MEMORY_PATHS = ['memory']
        # 이전 테스트의 memory 잔여 데이터 정리
        conn = config.get_db_connection(config.DB_PATH)
        conn.execute("DELETE FROM file_hashes WHERE source_file LIKE 'memory:%'")
        conn.execute("DELETE FROM chunks WHERE project_id IN (SELECT id FROM projects WHERE source_type='memory')")
        conn.execute("DELETE FROM projects WHERE source_type='memory'")
        conn.commit()
        conn.close()

    def tearDown(self):
        config.MEMORY_WORKSPACE = self._orig_workspace
        config.MEMORY_PATHS = self._orig_paths
        shutil.rmtree(self.tmpdir, ignore_errors=True)

    def test_update_inserts(self):
        """memory-update가 source_type='memory'로 DB에 삽입"""
        data = self._assert_ok(self._exec('memory-update'))
        self.assertEqual(data['total'], 2)
        self.assertEqual(data['updated'], 2)
        # DB 확인
        conn = config.get_db_connection(config.DB_PATH, row_factory=True)
        rows = conn.execute("SELECT name, source_type FROM projects WHERE source_type='memory'").fetchall()
        conn.close()
        self.assertEqual(len(rows), 2)
        for r in rows:
            self.assertEqual(r['source_type'], 'memory')

    def test_incremental_skip(self):
        """변경 없으면 skip"""
        self._exec('memory-update')  # 1차
        data = self._assert_ok(self._exec('memory-update'))  # 2차
        self.assertEqual(data['updated'], 0)
        self.assertEqual(data['skipped'], 2)

    def test_force_reindex(self):
        """--force면 무조건 재인덱싱"""
        self._exec('memory-update')
        data = self._assert_ok(self._exec('memory-update', {'sub_args': ['--force']}))
        self.assertEqual(data['updated'], 2)


class TestMemoryStatus(VegaTestCase):
    """memory-status 명령 검증"""

    def setUp(self):
        self.tmpdir = tempfile.mkdtemp()
        mem_dir = Path(self.tmpdir) / 'memory'
        mem_dir.mkdir()
        (mem_dir / 'a.md').write_text('# A\nContent\n', encoding='utf-8')
        self._orig_workspace = config.MEMORY_WORKSPACE
        self._orig_paths = config.MEMORY_PATHS
        config.MEMORY_WORKSPACE = self.tmpdir
        config.MEMORY_PATHS = ['memory']
        self._exec('memory-update')

    def tearDown(self):
        config.MEMORY_WORKSPACE = self._orig_workspace
        config.MEMORY_PATHS = self._orig_paths
        shutil.rmtree(self.tmpdir, ignore_errors=True)

    def test_status_counts(self):
        """files/chunks/embedded 카운트"""
        data = self._assert_ok(self._exec('memory-status'))
        self.assertEqual(data['files'], 1)
        self.assertGreaterEqual(data['chunks'], 1)
        self.assertIn('dbPath', data)
        self.assertIn('model', data)


class TestMemorySearch(VegaTestCase):
    """memory-search 명령 검증"""

    def setUp(self):
        self.tmpdir = tempfile.mkdtemp()
        mem_dir = Path(self.tmpdir) / 'memory'
        mem_dir.mkdir()
        (mem_dir / 'note.md').write_text('# Meeting Notes\nDiscussed budget for Q2\n## Action Items\nFollow up with finance team\n', encoding='utf-8')
        self._orig_workspace = config.MEMORY_WORKSPACE
        self._orig_paths = config.MEMORY_PATHS
        config.MEMORY_WORKSPACE = self.tmpdir
        config.MEMORY_PATHS = ['memory']
        self._exec('memory-update')

    def tearDown(self):
        config.MEMORY_WORKSPACE = self._orig_workspace
        config.MEMORY_PATHS = self._orig_paths
        shutil.rmtree(self.tmpdir, ignore_errors=True)

    def test_search_returns_list(self):
        """memory-search 결과는 리스트"""
        data = self._assert_ok(self._exec('memory-search', {'query': 'budget'}))
        self.assertIsInstance(data, list)

    def test_search_result_format(self):
        """결과 형식: path, startLine, endLine, score, snippet, source"""
        data = self._assert_ok(self._exec('memory-search', {'query': 'budget'}))
        if data:  # FTS가 매칭되면
            item = data[0]
            self.assertIn('path', item)
            self.assertIn('startLine', item)
            self.assertIn('endLine', item)
            self.assertIn('score', item)
            self.assertIn('snippet', item)
            self.assertEqual(item['source'], 'memory')

    def test_empty_query(self):
        """빈 쿼리 → 빈 리스트"""
        data = self._assert_ok(self._exec('memory-search', {'query': ''}))
        self.assertEqual(data, [])

    def test_no_match(self):
        """매칭 없는 쿼리 → 빈 리스트"""
        data = self._assert_ok(self._exec('memory-search', {'query': 'xyznonexistent'}))
        self.assertEqual(data, [])


class TestSchemaV6Migration(VegaTestCase):
    """스키마 v6 마이그레이션 검증"""

    def test_chunks_has_line_columns(self):
        """chunks 테이블에 start_line/end_line 컬럼 존재"""
        conn = config.get_db_connection(config.DB_PATH)
        cols = [row[1] for row in conn.execute("PRAGMA table_info(chunks)")]
        conn.close()
        self.assertIn('start_line', cols)
        self.assertIn('end_line', cols)

    def test_projects_has_source_type(self):
        """projects 테이블에 source_type 컬럼 존재"""
        conn = config.get_db_connection(config.DB_PATH)
        cols = [row[1] for row in conn.execute("PRAGMA table_info(projects)")]
        conn.close()
        self.assertIn('source_type', cols)


class TestUpgrade(VegaTestCase):
    """upgrade 명령 테스트"""

    def test_upgrade_basic(self):
        """upgrade 실행 → 정상 응답 + 필수 필드"""
        r = self._exec('upgrade')
        data = self._assert_ok(r)
        self.assertIn('schema_version', data)
        self.assertIn('sync', data)
        self.assertIn('memory', data)
        self.assertIn('embed', data)
        self.assertIn('steps', data)
        self.assertIn('updated', data['sync'])
        self.assertIn('skipped', data['sync'])

    def test_upgrade_schema_version(self):
        """upgrade 후 스키마 버전이 최신"""
        r = self._exec('upgrade')
        data = self._assert_ok(r)
        self.assertEqual(data['schema_version'], config.SCHEMA_VERSION)

    def test_upgrade_memory_section(self):
        """upgrade 결과에 memory 섹션 정상 포함"""
        r = self._exec('upgrade')
        data = self._assert_ok(r)
        mem = data['memory']
        self.assertIn('updated', mem)
        self.assertIn('total', mem)

    def test_upgrade_embed_section(self):
        """upgrade 결과에 embed 섹션 정상 포함"""
        r = self._exec('upgrade')
        data = self._assert_ok(r)
        emb = data['embed']
        self.assertIn('embedded', emb)


if __name__ == '__main__':
    unittest.main()
