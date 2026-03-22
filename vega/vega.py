#!/usr/bin/env python3
"""
Vega 프로젝트 검색엔진 — CLI 진입점

핵심 인프라는 core.py, 명령 핸들러는 commands/ 디렉토리.

사용법:
  python vega.py "비금도 ZTT 케이블 납기 언제야"
  python vega.py search "케이블"
  python vega.py --human "비금도 케이블"
"""

import sys
from pathlib import Path

# core.py와 같은 디렉토리에서 import
sys.path.insert(0, str(Path(__file__).parent))

# core 모듈 로드 (명령 자동 디스커버리 포함)
from core import main, execute, route_input, register_command

if __name__ == '__main__':
    main()
