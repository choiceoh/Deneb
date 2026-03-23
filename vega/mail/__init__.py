"""mail — 메일 → 프로젝트 .md 자동 삽입 패키지."""
from mail.converter import (
    process_mail,
    process_mail_batch,
    match_project,
    insert_to_md,
    main,
    _load_project_index,
    _extract_keywords,
    _format_entry,
    _find_md_file,
    _find_section_end,
    _auto_sync_db,
    DATE_HEADING_RE,
    _MAX_SUMMARY_LEN,
)

__all__ = [
    "process_mail",
    "process_mail_batch",
    "match_project",
    "insert_to_md",
    "main",
    "_load_project_index",
    "_extract_keywords",
    "_format_entry",
    "_find_md_file",
    "_find_section_end",
    "_auto_sync_db",
    "DATE_HEADING_RE",
    "_MAX_SUMMARY_LEN",
]
