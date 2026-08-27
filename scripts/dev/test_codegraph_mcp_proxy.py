"""Framing tests for the CodeGraph MCP proxy.

Two stdio framings exist and are not interchangeable: LSP prefixes each message
with a Content-Length header, while the MCP stdio transport uses newline-
delimited JSON. The proxy used to READ both and WRITE only Content-Length,
which silently broke every NDJSON client — including Deneb's own gateway.
"""

from __future__ import annotations

import io
import json
import unittest

from codegraph_mcp_proxy import encode_message, read_message


class FramingMirrorTests(unittest.TestCase):
    def test_encodes_ndjson_without_a_header(self) -> None:
        raw = encode_message({"jsonrpc": "2.0", "id": 1}, "ndjson")
        self.assertNotIn(b"Content-Length", raw)
        self.assertTrue(raw.endswith(b"\n"))
        # A newline-delimited client parses one line as one message.
        self.assertEqual(json.loads(raw.decode("utf-8")), {"jsonrpc": "2.0", "id": 1})

    def test_defaults_to_ndjson_the_mcp_stdio_standard(self) -> None:
        """The default must be the transport MCP actually specifies.

        It matters because the first write to the CHILD happens before the child
        has said anything, so nothing has been mirrored yet. Defaulting to
        Content-Length there is what made codegraph answer every initialize with
        a -32700 parse error.
        """
        raw = encode_message({"jsonrpc": "2.0", "id": 1})
        self.assertNotIn(b"Content-Length", raw)
        self.assertTrue(raw.endswith(b"\n"))

    def test_content_length_header_is_the_18_byte_line_that_broke_ndjson(self) -> None:
        """Pins the exact production signature.

        Deneb's gateway logged 'unparseable line | bytes=18 invalid character
        C' 79 times over two days. 18 bytes starting with 'C' is the
        Content-Length line itself, read as if it were a whole JSON message.
        """
        raw = encode_message({"jsonrpc": "2.0", "id": None, "error": {"code": -32700}}, "content-length")
        header = raw.split(b"\r\n", 1)[0]
        self.assertTrue(header.startswith(b"C"))
        with self.assertRaises(json.JSONDecodeError):
            json.loads(header.decode("ascii"))

    def test_read_message_reports_ndjson_framing(self) -> None:
        observed = ["content-length"]
        msg = read_message(io.BytesIO(b'{"jsonrpc":"2.0","id":7}\n'), observed)
        self.assertEqual(msg, {"jsonrpc": "2.0", "id": 7})
        self.assertEqual(observed[0], "ndjson")

    def test_read_message_reports_content_length_framing(self) -> None:
        observed = ["ndjson"]
        body = b'{"jsonrpc":"2.0","id":9}'
        stream = io.BytesIO(b"Content-Length: %d\r\n\r\n" % len(body) + body)
        msg = read_message(stream, observed)
        self.assertEqual(msg, {"jsonrpc": "2.0", "id": 9})
        self.assertEqual(observed[0], "content-length")

    def test_reply_in_the_dialect_the_peer_spoke(self) -> None:
        """The mirror property, end to end: whatever came in decides what goes out."""
        for wire, want_header in (
            (b'{"jsonrpc":"2.0","id":1}\n', False),
            (b'Content-Length: 24\r\n\r\n{"jsonrpc":"2.0","id":1}', True),
        ):
            observed = ["ndjson"]
            read_message(io.BytesIO(wire), observed)
            reply = encode_message({"jsonrpc": "2.0", "id": 1, "result": {}}, observed[0])
            self.assertEqual(reply.startswith(b"Content-Length: "), want_header, wire)


if __name__ == "__main__":
    unittest.main()
