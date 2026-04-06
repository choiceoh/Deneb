"""Multi-task dispatcher environment for Deneb RL training.

Routes trajectories to task-specific reward functions based on task_type.
Each task rewards measurable quality signals, not subjective preferences.

Trajectory format (JSONL from Go gateway):
    {"task_type": "memory_json", "system": "...", "user_message": "...",
     "response": "...", "metadata": {...}, "captured_at": 1234567890}

Usage with Atropos:
    python -m atropos.server --port 30101 --env dispatcher
"""

from __future__ import annotations

import json
import re
from typing import Any


def compute_reward(trajectory: dict[str, Any]) -> float:
    """Dispatch to task-specific reward function."""
    task_type = trajectory.get("task_type", "")
    fn = REWARD_FUNCTIONS.get(task_type)
    if fn is None:
        return 0.0
    return fn(trajectory)


# ---------------------------------------------------------------------------
# Task-specific reward functions
# ---------------------------------------------------------------------------


def _fact_extraction_reward(t: dict[str, Any]) -> float:
    """Reward for memory_json CallerTag (fact extraction + dreaming phases).

    Signals:
      - JSON validity (hard requirement)
      - Schema compliance (expected fields present)
      - Fact count reasonableness (not 0, not >20 from single extraction)
      - Content quality (non-empty, reasonable length)
    """
    response = t.get("response", "")
    if not response.strip():
        return 0.0

    # 1. JSON validity (0.3)
    try:
        parsed = json.loads(response)
    except json.JSONDecodeError:
        return 0.0  # complete failure

    reward = 0.3

    # 2. Schema compliance (0.3)
    # Expected: {"facts": [...]} or {"results": [...], "conflicts": [...]}
    if isinstance(parsed, dict):
        if "facts" in parsed and isinstance(parsed["facts"], list):
            facts = parsed["facts"]
            reward += 0.15
            # Check fact structure
            valid_facts = 0
            for fact in facts:
                if isinstance(fact, dict) and fact.get("content"):
                    has_category = bool(fact.get("category"))
                    has_importance = isinstance(fact.get("importance"), (int, float))
                    if has_category and has_importance:
                        valid_facts += 1
            if facts and valid_facts / len(facts) >= 0.8:
                reward += 0.15
        elif "results" in parsed and isinstance(parsed["results"], list):
            # Dreaming verification format
            reward += 0.3
    elif isinstance(parsed, list):
        # Bare array of facts (legacy format)
        reward += 0.2

    # 3. Count reasonableness (0.2)
    items = _extract_items(parsed)
    count = len(items) if items else 0
    if 1 <= count <= 15:
        reward += 0.2
    elif count > 15:
        reward += 0.05  # too many, likely noisy

    # 4. Content quality (0.2)
    if items:
        non_empty = sum(1 for it in items if _item_has_content(it))
        if items and non_empty / len(items) >= 0.8:
            reward += 0.2

    return min(reward, 1.0)


def _compaction_reward(t: dict[str, Any]) -> float:
    """Reward for aurora_compaction CallerTag.

    Signals:
      - Compression achieved (input longer than output)
      - Key entity preservation (names, numbers, file paths survive)
      - XML structure compliance (expected sections present)
    """
    user_msg = t.get("user_message", "")
    response = t.get("response", "")
    if not response.strip():
        return 0.0

    reward = 0.0

    # 1. Compression ratio (0.4)
    if len(user_msg) > 0:
        ratio = len(user_msg) / max(len(response), 1)
        if ratio >= 5:
            reward += 0.4
        elif ratio >= 3:
            reward += 0.3
        elif ratio >= 1.5:
            reward += 0.2
        elif ratio >= 1.0:
            reward += 0.1
        # ratio < 1 means output is longer than input, no reward

    # 2. Entity preservation (0.3)
    entities = _extract_entities(user_msg)
    if entities:
        preserved = sum(1 for e in entities if e in response)
        preservation_rate = preserved / len(entities)
        reward += 0.3 * preservation_rate

    # 3. Structure compliance (0.3)
    # Expected XML sections: <goal>, <progress>, <next_steps>
    expected_tags = ["<goal>", "<progress>", "<next_steps>"]
    found = sum(1 for tag in expected_tags if tag in response)
    reward += 0.3 * (found / len(expected_tags))

    return min(reward, 1.0)


def _verification_reward(t: dict[str, Any]) -> float:
    """Reward for fact verification (dreaming phase, also memory_json CallerTag).

    Signals:
      - JSON format compliance
      - Judgment distribution (not everything "valid" -- uncritical)
      - Reasoning present per judgment
    """
    response = t.get("response", "")
    if not response.strip():
        return 0.0

    try:
        parsed = json.loads(response)
    except json.JSONDecodeError:
        return 0.0

    reward = 0.3  # JSON valid

    if not isinstance(parsed, dict):
        return reward

    # Check for results array
    results = parsed.get("results", [])
    if not isinstance(results, list) or not results:
        return reward

    reward += 0.2  # has results

    # Judgment distribution (0.2)
    valid_count = sum(1 for r in results if r.get("valid") is True)
    invalid_count = sum(1 for r in results if r.get("valid") is False)
    total = valid_count + invalid_count
    if total > 0:
        # Penalize if everything is valid (uncritical) or everything is invalid
        ratio = valid_count / total
        if 0.3 <= ratio <= 0.9:
            reward += 0.2
        elif 0.1 <= ratio < 0.3 or 0.9 < ratio <= 1.0:
            reward += 0.1

    # Reasoning quality (0.3)
    with_reason = sum(1 for r in results if r.get("reason"))
    if results and with_reason / len(results) >= 0.5:
        reward += 0.3

    return min(reward, 1.0)


def _tool_compression_reward(t: dict[str, Any]) -> float:
    """Reward for tool output compression (via CallLocalLLM).

    Signals:
      - Actually shorter than input
      - Error messages preserved
      - File paths preserved
    """
    user_msg = t.get("user_message", "")
    response = t.get("response", "")
    if not response.strip():
        return 0.0

    reward = 0.0

    # 1. Compression (0.3)
    if len(response) < len(user_msg):
        reward += 0.3
    elif len(response) < len(user_msg) * 1.1:
        reward += 0.1  # barely compressed

    # 2. Error preservation (0.4)
    errors = _extract_error_patterns(user_msg)
    if errors:
        preserved = sum(1 for e in errors if e.lower() in response.lower())
        reward += 0.4 * (preserved / len(errors))
    else:
        reward += 0.4  # no errors to preserve, full score

    # 3. Path preservation (0.3)
    paths = _extract_file_paths(user_msg)
    if paths:
        preserved = sum(1 for p in paths if p in response)
        reward += 0.3 * (preserved / len(paths))
    else:
        reward += 0.3  # no paths, full score

    return min(reward, 1.0)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _extract_items(parsed: Any) -> list[dict]:
    """Extract the main item list from parsed JSON."""
    if isinstance(parsed, list):
        return [x for x in parsed if isinstance(x, dict)]
    if isinstance(parsed, dict):
        for key in ("facts", "results", "entries", "items"):
            val = parsed.get(key)
            if isinstance(val, list):
                return [x for x in val if isinstance(x, dict)]
    return []


def _item_has_content(item: dict) -> bool:
    """Check if a fact/result item has meaningful content."""
    for key in ("content", "text", "merged_content", "description"):
        val = item.get(key)
        if isinstance(val, str) and len(val) > 10:
            return True
    return False


def _extract_entities(text: str) -> list[str]:
    """Extract likely named entities (numbers, file paths, proper nouns)."""
    entities = set()
    # File paths
    entities.update(re.findall(r'[\w/]+\.\w{1,4}', text))
    # IP addresses
    entities.update(re.findall(r'\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}', text))
    # Version numbers
    entities.update(re.findall(r'v?\d+\.\d+\.\d+', text))
    # Port numbers
    entities.update(re.findall(r':\d{4,5}\b', text))
    return list(entities)[:20]  # cap to avoid noise


def _extract_error_patterns(text: str) -> list[str]:
    """Extract error messages from tool output."""
    patterns = []
    for line in text.split('\n'):
        lower = line.lower().strip()
        if any(kw in lower for kw in ('error', 'failed', 'fatal', 'panic', 'exception')):
            # Keep the core message, not the full line
            msg = line.strip()[:100]
            if msg:
                patterns.append(msg)
    return patterns[:10]


def _extract_file_paths(text: str) -> list[str]:
    """Extract file paths from tool output."""
    paths = re.findall(r'(?:[\w./-]+/[\w.-]+)', text)
    # Filter to likely real paths (has extension or is a directory pattern)
    return [p for p in paths if '.' in p.split('/')[-1] or len(p.split('/')) >= 3][:15]


# ---------------------------------------------------------------------------
# Registry
# ---------------------------------------------------------------------------

REWARD_FUNCTIONS: dict[str, Any] = {
    "memory_json": _fact_extraction_reward,
    "memory": _fact_extraction_reward,  # alias
    "aurora_compaction": _compaction_reward,
    "session_memory": _verification_reward,  # session memory is structured output
}
