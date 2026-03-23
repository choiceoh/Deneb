"""db package — split from project_db_v2.py.

Re-exports all public names so that ``from db import X`` works for any X
that was previously importable from ``project_db_v2``.
"""

import sys
from pathlib import Path

# Ensure the vega directory is on sys.path so that ``import config`` works
# from submodules exactly as it did in the monolithic project_db_v2.py.
_self_dir = str(Path(__file__).resolve().parent.parent)
if _self_dir not in sys.path:
    sys.path.insert(0, _self_dir)

# --- schema ---
from .schema import SCHEMA, init_db  # noqa: F401,E402

# --- parser ---
from .parser import (  # noqa: F401,E402
    DATE_HEADING_RE,
    HEADING_RE,
    extract_table_meta,
    split_sections,
    _parse_comm_block,
)

# --- classify ---
from .classify import classify_section, extract_tags  # noqa: F401,E402

# --- importer (import, search, utilities, CLI) ---
from .importer import (  # noqa: F401,E402
    import_files,
    import_incremental,
    _import_incremental_impl,
    upsert_md_file,
    delete_project_by_source,
    rebuild_fts,
    _sanitize_fts,
    _FTS_RESERVED,
    search,
    print_results,
    list_projects,
    show_project,
    show_timeline,
    list_tags,
    main,
)
