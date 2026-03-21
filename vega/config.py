#!/usr/bin/env python3
"""
Vega 프로젝트 검색엔진 — 중앙 설정

모든 파일(vega.py, addons, router.py, project_db_v2.py)은
여기서 설정을 import합니다. 환경변수가 있으면 우선 사용.
"""

import os
from pathlib import Path
from contextlib import contextmanager

SELF_DIR = Path(__file__).parent

# 경로 설정 (환경변수 우선)
DB_PATH = os.environ.get('DB_PATH', str(SELF_DIR / "projects.db"))


def _find_path(env_var, local_candidates, openclaw_globs=None, check_fn=os.path.isfile, use_which=None):
    """공통 경로 탐색: 환경변수 → 로컬 후보 → OpenClaw 글롭 → which → None"""
    import glob as _glob
    env_val = os.environ.get(env_var, '')
    if env_val and check_fn(env_val):
        return env_val
    for c in local_candidates:
        if c and check_fn(c):
            return c
    for pattern in (openclaw_globs or []):
        for match in _glob.glob(os.path.expanduser(pattern)):
            if check_fn(match):
                return match
    if use_which:
        import shutil
        found = shutil.which(use_which)
        if found:
            return found
    return env_val if env_var == 'MD_DIR' else None  # MD_DIR만 기본값 폴백


def _check_md_dir(path):
    """MD_DIR 유효성: 디렉토리이고 .md 파일이 있는지"""
    return os.path.isdir(path) and any(f.endswith('.md') for f in os.listdir(path))


MD_DIR = _find_path(
    'MD_DIR',
    [str(SELF_DIR / "projects")],
    ['~/.openclaw/agents/main/qmd/xdg-data/qmd/knowledge/projects',
     '~/.openclaw/*/qmd/xdg-data/qmd/knowledge/projects'],
    check_fn=lambda p: os.path.isdir(p) and (p == str(SELF_DIR / "projects") or _check_md_dir(p)),
) or str(SELF_DIR / "projects")

# QMD 실행 경로 — 동적 탐색 (v1.3: 직접 바이너리 + wrapper 이중 탐색)
QMD_BIN = _find_path('QMD_BIN', [], use_which='qmd')

def _check_executable(path):
    return os.path.isfile(path) and os.access(path, os.X_OK)

QMD_WRAPPER = _find_path(
    'QMD_WRAPPER',
    [str(SELF_DIR / 'qmd' / 'qmd-wrapper.sh'),
     os.path.join(os.environ.get('VEGA_HOME', ''), 'qmd', 'qmd-wrapper.sh'),
     os.path.expanduser('~/.vega/qmd/qmd-wrapper.sh'),
     '/home/node/.openclaw/.bun/bin/qmd-wrapper.sh',
     os.path.expanduser('~/.openclaw/.bun/bin/qmd-wrapper.sh')],
    ['~/.openclaw/*/bin/qmd-wrapper.sh',
     '/home/node/.openclaw/*/bin/qmd-wrapper.sh'],
    check_fn=_check_executable,
    use_which='qmd-wrapper.sh',
)

def get_qmd_executable():
    """QMD 실행 가능한 경로 반환. wrapper > binary > None."""
    if QMD_WRAPPER:
        return QMD_WRAPPER
    if QMD_BIN:
        return QMD_BIN
    return None

# DB 연결 공통 함수 (WAL + busy_timeout)
def get_db_connection(db_path=None, row_factory=False):
    """모든 모듈이 공유하는 DB 연결 함수. WAL 모드 + 동시접근 안전."""
    import sqlite3
    path = db_path or DB_PATH
    conn = sqlite3.connect(path, timeout=10)
    conn.execute("PRAGMA journal_mode=WAL")
    conn.execute("PRAGMA busy_timeout=5000")
    if row_factory:
        conn.row_factory = sqlite3.Row
    return conn


@contextmanager
def db_session(db_path=None, row_factory=False):
    """컨텍스트 매니저로 커넥션 자동 정리. conn.close()를 깜빡할 수 없음."""
    conn = get_db_connection(db_path, row_factory)
    try:
        yield conn
    finally:
        conn.close()


# 통일 에러 클래스
class VegaError(Exception):
    """명령 핸들러에서 사용자 에러를 던질 때 사용. execute()에서 통일 처리."""
    def __init__(self, message, usage=None, error_type='user_error'):
        self.message = message
        self.usage = usage
        self.error_type = error_type
        super().__init__(message)


# ── 로컬 AI 모델 설정 (v1.4) ──
MODELS_DIR = str(SELF_DIR / "models")
MODEL_EXPANDER  = os.environ.get('VEGA_MODEL_EXPANDER',  os.path.join(MODELS_DIR, 'Qwen3.5-9B-Q4_K_M.gguf'))
MODEL_EMBEDDER  = os.environ.get('VEGA_MODEL_EMBEDDER',  os.path.join(MODELS_DIR, 'qwen3-embedding-8b-q4_k_m.gguf'))
MODEL_RERANKER  = os.environ.get('VEGA_MODEL_RERANKER',  os.path.join(MODELS_DIR, 'qwen3-reranker-4b.gguf'))
MODEL_UNLOAD_TTL = int(os.environ.get('VEGA_MODEL_TTL', '300'))   # 초: 비활성 후 모델 해제
INFERENCE_BACKEND = os.environ.get('VEGA_INFERENCE', 'local')      # 'local' | 'qmd' | 'sqlite_only'

# Memory backend (v1.43)
MEMORY_PATHS = [p.strip() for p in os.environ.get('VEGA_MEMORY_PATHS', 'MEMORY.md,memory,projects').split(',') if p.strip()]
MEMORY_WORKSPACE = os.environ.get('VEGA_MEMORY_WORKSPACE', '')  # 빈 값 = MD_DIR 부모 자동

# DB 스키마 버전 관리
SCHEMA_VERSION = 6

def check_schema_version(conn):
    """스키마 버전 확인. 불일치 시 재빌드 필요 표시."""
    row = conn.execute("PRAGMA user_version").fetchone()
    ver = row[0] if row else 0
    return ver >= SCHEMA_VERSION

def set_schema_version(conn):
    """스키마 버전 설정."""
    conn.execute(f"PRAGMA user_version = {SCHEMA_VERSION}")


# audit_log 기록 유틸리티
def write_audit_log(conn, project_id, action, field=None, old_value=None, new_value=None, actor='user'):
    """변경 이력을 audit_log 테이블에 기록."""
    try:
        conn.execute("""
            INSERT INTO audit_log (project_id, action, actor, field, old_value, new_value)
            VALUES (?, ?, ?, ?, ?, ?)
        """, (project_id, action, actor, field,
              str(old_value)[:500] if old_value else None,
              str(new_value)[:500] if new_value else None))
    except Exception:
        pass  # audit_log 실패가 메인 동작을 막지 않음


# 거래처/자재 사전 (addons.py에서 사용)
# 리랭킹 모드 (환경변수 VEGA_RERANK으로 오버라이드 가능)
#   'full'      — Vega 퓨전 스코어링 + QMD 리랭커 (기본값)
#   'vega_only' — Vega 퓨전 스코어링만, QMD 리랭커 비활성 (--no-rerank)
#   'none'      — 리랭킹 없음: SQLite BM25 원본 순서 + QMD 네이티브 점수 유지
RERANK_MODE = os.environ.get('VEGA_RERANK', 'full')

KNOWN_VENDORS = [
    ('ZTT',['ztt','zhongtian']),('대한전선',['대한전선']),('가온전선',['가온전선']),
    ('진코(Jinko)',['진코','jinko','jay yu']),('JA Solar',['ja solar']),('트리나솔라',['트리나','trina']),
    ('한화솔루션',['한화솔루션']),('화웨이',['화웨이','huawei','sun2000']),
    ('현대L&C',['현대l&c','현대엘앤씨']),('현대엔지니어링',['현대엔지니어링']),
    ('엔라이튼',['엔라이튼']),('한국전기기술',['한국전기기술']),('모비언트',['모비언트']),
    ('신한은행',['신한은행','신한자산']),('여수수협',['여수수협']),
    ('법무법인 태평양',['태평양','bkl']),('법무법인 세종',['법무법인 세종']),
    ('오늘회계법인',['오늘회계']),('서울보증보험',['서울보증']),
    ('한국전력',['한전','한국전력','kepco']),('한국환경공단',['환경공단']),
    ('한화시스템',['한화시스템']),('Peak Energy',['peak energy']),
]
KNOWN_MATERIALS = [
    ('진코 635Wp',['진코635','진코 635','635wp']),('한화 640Wp',['한화 640','한화640','640wp']),
    ('경량모듈',['경량모듈']),('화웨이 330kW',['330ktl','sun2000-330']),
    ('154kV 해저케이블',['154kv.*해저','해저케이블.*154']),('XLPE 케이블',['xlpe']),
    ('솔라케이블',['솔라케이블']),('CV 케이블',['cv 케이블','cv케이블']),
    ('TPO 브라켓',['tpo.*브라켓','tpo 브라켓']),('캐노피 구조물',['캐노피']),
    ('ESS/BESS',['ess','bess']),('PCS',['pcs']),('GIS',['gis']),('풍황계측기',['풍황계측','lidar']),
]
