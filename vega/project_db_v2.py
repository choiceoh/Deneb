"""Backward-compatibility wrapper — actual code lives in db/ package."""
import sys
from pathlib import Path
sys.path.insert(0, str(Path(__file__).parent))
from db import *  # noqa: F401,F403,E402
from db.schema import SCHEMA, init_db  # noqa: F401,E402
from db.parser import (  # noqa: F401,E402
    extract_table_meta, split_sections, DATE_HEADING_RE, HEADING_RE,
    _parse_comm_block,
)
from db.classify import classify_section, extract_tags  # noqa: F401,E402
from db.importer import (  # noqa: F401,E402
    import_files, import_incremental, rebuild_fts, _sanitize_fts,
    upsert_md_file, delete_project_by_source,
    _import_incremental_impl, _FTS_RESERVED,
    search, print_results, list_projects, show_project, show_timeline,
    list_tags, main,
)

if __name__ == '__main__':
    main()
