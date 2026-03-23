"""Backward-compatibility wrapper — actual code lives in editor/ package."""
from editor.md import *  # noqa: F401,F403
from editor.md import (
    find_md_path,
    update_meta_field,
    update_db_field,
    add_history_entry,
    add_action_item,
    append_to_section,
    _backup_file,
    _get_md_path_cache,
    _md_path_cache,
)
