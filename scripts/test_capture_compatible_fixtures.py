"""Offline safety tests; run python3 -B -I -m unittest discover -s scripts -p 'test_capture*.py'."""
import contextlib
import http.client
import importlib.util
import io
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import MagicMock, patch

spec = importlib.util.spec_from_file_location("capture", Path(__file__).with_name("capture-compatible-fixtures.py"))
capture = importlib.util.module_from_spec(spec)
spec.loader.exec_module(capture)


def stream(*objects):
    return ("".join("data: " + json.dumps(obj) + "\n\n" for obj in objects) + "data: [DONE]\n\n").encode()


class CaptureSafety(unittest.TestCase):
    def run_capture(self, body=None, error=None):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        out = Path(directory.name) / "new-capture"
        response = MagicMock()
        response.__enter__.return_value.read.return_value = body
        opener = MagicMock()
        if error:
            opener.open.side_effect = error
        else:
            opener.open.return_value = response
        diagnostic = io.StringIO()
        with patch.dict(os.environ, {"DYNO_LIVE_OPENAI": "1", "OPENAI_BASE_URL": "https://private.example/v1", "DYNO_MODEL": "private-model", "OPENAI_API_KEY": "BearerSecret"}, clear=True), patch.object(capture.sys, "argv", ["capture", str(out)]), patch.object(capture.urllib.request, "build_opener", return_value=opener), contextlib.redirect_stderr(diagnostic), contextlib.redirect_stdout(io.StringIO()):
            code = capture.run()
        self.assertNotIn("BearerSecret", diagnostic.getvalue())
        return code, out, diagnostic.getvalue()

    def test_empty_finish_reason_and_role_are_valid_initial_deltas(self):
        code, out, _ = self.run_capture(stream({"choices": [{"index": 0, "finish_reason": "", "delta": {"role": "", "content": ""}}]}))
        self.assertEqual(code, 0)
        self.assertIn('"finish_reason": ""', (out / "chat.sse").read_text())

    def test_split_credential_never_creates_capture(self):
        code, out, _ = self.run_capture(stream({"choices": [{"index": 0, "delta": {"content": "Bearer"}}]}, {"choices": [{"index": 0, "delta": {"content": "Secret"}}]}))
        self.assertEqual(code, 1)
        self.assertFalse(out.exists())

    def test_usage_details_are_recursively_allowlisted(self):
        code, out, _ = self.run_capture(stream({"choices": [], "usage": {"prompt_tokens": 4, "completion_tokens": 2, "total_tokens": 6, "prompt_tokens_details": {"cached_tokens": 0, "private_host": "https://private.example", "request_id": "private-id"}, "completion_tokens_details": {"reasoning_tokens": 0, "private_metadata": {"secret": "other-secret"}}}}))
        self.assertEqual(code, 0)
        body = (out / "chat.sse").read_text()
        for forbidden in ("private.example", "private-id", "private_metadata", "other-secret"):
            self.assertNotIn(forbidden, body)
        self.assertIn('"cached_tokens": 0', body)
        self.assertIn('"reasoning_tokens": 0', body)

    def test_bad_status_line_never_echoes_server_diagnostic(self):
        code, out, diagnostic = self.run_capture(error=http.client.BadStatusLine("BearerSecret server-controlled status"))
        self.assertEqual(code, 1)
        self.assertFalse(out.exists())
        self.assertNotIn("server-controlled", diagnostic)
        self.assertNotIn("Traceback", diagnostic)

    def test_non_object_frames_have_no_traceback_or_capture(self):
        for obj in ([], 12, None, "server-controlled", {"choices": "not-array"}, {"choices": ["not-object"]}, {"choices": [{"delta": []}]}):
            with self.subTest(frame=obj):
                code, out, diagnostic = self.run_capture(stream(obj))
                self.assertEqual(code, 1)
                self.assertFalse(out.exists())
                self.assertNotIn("Traceback", diagnostic)
                self.assertNotIn("server-controlled", diagnostic)

    def test_retained_choice_fields_have_protocol_types(self):
        for field, value in (("index", True), ("index", {}), ("finish_reason", []), ("finish_reason", 12)):
            with self.subTest(field=field, value=value):
                code, out, diagnostic = self.run_capture(stream({"choices": [{field: value, "delta": {}}]}))
                self.assertEqual(code, 1)
                self.assertFalse(out.exists())
                self.assertNotIn("Traceback", diagnostic)
        for field in ("role", "content"):
            with self.subTest(delta=field):
                code, out, diagnostic = self.run_capture(stream({"choices": [{"index": 0, "delta": {field: {"nested": "private"}}}]}))
                self.assertEqual(code, 1)
                self.assertFalse(out.exists())
                self.assertNotIn("Traceback", diagnostic)

    def test_deeply_nested_json_has_no_traceback(self):
        body = ('data: {"choices":[{"delta":{"content":' + '[' * 1500 + '0' + ']' * 1500 + '}}]}\n\ndata: [DONE]\n\n').encode()
        code, out, diagnostic = self.run_capture(body)
        self.assertEqual(code, 1)
        self.assertFalse(out.exists())
        self.assertNotIn("Traceback", diagnostic)

    def test_surrogate_and_unknown_enums_leave_no_partial_capture(self):
        for choice in ({"delta": {"content": "\ud800"}}, {"delta": {"role": "free text"}}, {"finish_reason": "free text"}):
            code, out, diagnostic = self.run_capture(stream({"choices": [choice]}))
            self.assertEqual(code, 1)
            self.assertFalse(out.exists())
            self.assertNotIn("Traceback", diagnostic)

    def test_bom_preserves_first_frame(self):
        code, out, _ = self.run_capture(b'\xef\xbb\xbf' + stream({"choices": [{"delta": {"content": "first frame"}}]}))
        self.assertEqual(code, 0)
        self.assertIn("first frame", (out / "chat.sse").read_text())

    def test_recursive_parser_failure_has_no_traceback(self):
        with patch.object(capture.json, "loads", side_effect=RecursionError):
            code, out, diagnostic = self.run_capture(b'data: {}\n\ndata: [DONE]\n\n')
        self.assertEqual(code, 1)
        self.assertFalse(out.exists())
        self.assertNotIn("Traceback", diagnostic)

    def test_capture_permissions_ignore_public_umask(self):
        previous = os.umask(0o022)
        try:
            code, out, _ = self.run_capture(stream({"choices": []}))
        finally:
            os.umask(previous)
        self.assertEqual(code, 0)
        self.assertEqual(out.stat().st_mode & 0o777, 0o700)
        for name in ("chat.sse", "manifest.json"):
            self.assertEqual((out / name).stat().st_mode & 0o777, 0o600)

    def test_escaped_credential_never_creates_capture(self):
        code, out, _ = self.run_capture(b'data: {"choices": [{"index": 0, "delta": {"content": "Bearer\\u0053ecret"}}]}\n\ndata: [DONE]\n\n')
        self.assertEqual(code, 1)
        self.assertFalse(out.exists())


if __name__ == "__main__":
    unittest.main()
