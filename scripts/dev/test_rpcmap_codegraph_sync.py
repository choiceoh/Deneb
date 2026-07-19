"""rpcmap→CodeGraph injector behavior: parser shapes and never-guess policy."""

import unittest

from rpcmap_codegraph_sync import ENTRY_RE, HANDLER_RE


class ParseTest(unittest.TestCase):
    def test_entry_and_handler_lines(self):
        m = ENTRY_RE.match("  [rpc] miniapp.people.list")
        self.assertEqual((m.group("kind"), m.group("name")), ("rpc", "miniapp.people.list"))
        h = HANDLER_RE.match("        → peopleList   (gateway-go/internal/x/people.go:91)")
        self.assertEqual(h.group("handler"), "peopleList")
        self.assertEqual(h.group("path"), "gateway-go/internal/x/people.go")
        self.assertEqual(h.group("line"), "91")

    def test_tool_and_event_kinds(self):
        self.assertEqual(ENTRY_RE.match("[tool] wiki").group("kind"), "tool")
        self.assertEqual(ENTRY_RE.match(" [event] chat.delivery_failed").group("kind"), "event")

    def test_hint_lines_do_not_parse_as_handlers(self):
        self.assertIsNone(HANDLER_RE.match("          codegraph node peopleList"))
