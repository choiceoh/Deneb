"""애드온: 새 프로젝트 템플릿 — 표준화된 프로젝트 .md 파일 자동 생성"""

import re, json
from datetime import datetime
from pathlib import Path

from ._base import BaseAddon


class Template(BaseAddon):
    name = 'template'
    description = '표준화된 프로젝트 .md 파일 자동 생성'
    commands = {'': '대화형 생성 (기본)', 'quick': '최소 정보로 빠르게'}

    TEMPLATE = """# {name}

| 항목 | 내용 |
|------|------|
| **상태** | {status} |
| **발주처** | {client} |
| **규모** | {capacity} |
| **사업구조** | {biz_type} |
| **사내 담당** | {person} |
| **거래처 담당** | {external} |

## 현재 상황
- {situation}

## 다음 예상 액션
-

## 이력
- {date}: 프로젝트 생성

## {date}
"""

    def run(self, cmd, args, ctx):
        if cmd == 'quick' and args:
            # quick "프로젝트명" "고객사" "담당자"
            data = {
                'name': args[0] if len(args) > 0 else '신규 프로젝트',
                'client': args[1] if len(args) > 1 else '',
                'person': args[2] if len(args) > 2 else '',
                'status': '초기 검토 단계 🟡',
                'capacity': '',
                'biz_type': '',
                'external': '',
                'situation': '초기 검토 진행 중',
                'date': datetime.now().strftime('%Y-%m-%d'),
            }
        else:
            data = self._interactive()

        content = self.TEMPLATE.format(**data)

        # 파일명 생성
        safe_name = re.sub(r'[^\w가-힣\s-]', '', data['name']).strip().replace(' ', '_')
        filename = f"{safe_name}.md"
        filepath = Path(ctx.md_dir) / filename

        if filepath.exists():
            print(f"⚠️  파일 이미 존재: {filepath}")
            print(f"   덮어쓰려면 직접 삭제 후 다시 실행하세요.")
            return

        if ctx.json_out:
            print(json.dumps({'filepath': str(filepath), 'content': content}, ensure_ascii=False, indent=2))
            return

        filepath.parent.mkdir(parents=True, exist_ok=True)
        filepath.write_text(content, encoding='utf-8')
        print(f"\n✅ 프로젝트 파일 생성: {filepath}")

        # 자동 DB 임포트
        try:
            importer = Path(__file__).parent.parent / 'project_db_v2.py'
            if importer.exists():
                import subprocess
                result = subprocess.run(
                    ['python3', str(importer), 'import', str(ctx.md_dir), '--db', ctx.db_path],
                    capture_output=True, text=True, timeout=10
                )
                if result.returncode == 0:
                    print(f"   DB 자동 반영 완료")
                else:
                    print(f"   DB 반영 실패 — 수동: python project_db_v2.py import {ctx.md_dir}")
            else:
                print(f"   수동 반영: python project_db_v2.py import {ctx.md_dir}")
        except Exception:
            print(f"   수동 반영: python project_db_v2.py import {ctx.md_dir}")

    def _interactive(self):
        print(f"\n{'━'*70}")
        print(f" 📝 새 프로젝트 생성")
        print(f"{'━'*70}\n")

        def ask(prompt, default=''):
            val = input(f"  {prompt}" + (f" [{default}]" if default else "") + ": ").strip()
            return val or default

        return {
            'name': ask("프로젝트명"),
            'client': ask("발주처/고객사"),
            'capacity': ask("규모 (예: 3MW, 80MW)"),
            'biz_type': ask("사업구조 (EPC/설비리스/O&M/PPA)", "EPC"),
            'person': ask("사내 담당"),
            'external': ask("거래처 담당"),
            'status': ask("초기 상태", "초기 검토 단계 🟡"),
            'situation': ask("현재 상황 요약", "초기 검토 진행 중"),
            'date': datetime.now().strftime('%Y-%m-%d'),
        }
