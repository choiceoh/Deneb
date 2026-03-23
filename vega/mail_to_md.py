"""Backward-compatibility wrapper — actual code lives in mail/ package."""
from mail.converter import *  # noqa: F401,F403
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

if __name__ == '__main__':
    main()
