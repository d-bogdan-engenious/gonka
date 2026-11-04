#!/usr/bin/env python3
"""QA-108 adversarial / negative tests for the dial-stdio rollback create (PR #2193).

Extends the dev's updater-rollback_test.py:
  * DialStdioAdversarialTest drives the real create() against hostile daemon
    replies (malformed 201, Docker-28 500 "context canceled", truncated stream,
    201 without an Id, non-zero dial-stdio exit) and asserts create() surfaces a
    clear error instead of hanging, crashing, or returning a bogus container id.
  * RollbackAdversarialTest reuses the real-Docker harness to prove rollback still
    restores the previous container after REPEATED failures (two pure rollbacks in
    a row), i.e. the dial-stdio create survives more than one cancel.
"""
import importlib.util
import os
from pathlib import Path
import tempfile
import unittest

SOURCE = Path(__file__).resolve().parent

# Load the dev harness (classes + load_container_state) without importing by name
# (the module file has a hyphen). unittest.main there is guarded, so nothing runs.
_spec = importlib.util.spec_from_file_location("rollback_base", SOURCE / "updater-rollback_test.py")
rb = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(rb)
load_container_state = rb.load_container_state

# A controllable fake `docker system dial-stdio`: drains the request, then emits
# the reply selected by ADV_MODE. Mirrors the dev's FAKE_DIAL_STDIO.
ADV_DIAL = r"""#!/usr/bin/env python3
import os, sys
fd = sys.stdin.fileno()
data = b""
while b"\r\n\r\n" not in data:
    chunk = os.read(fd, 65536)
    if not chunk:
        break
    data += chunk
if b"\r\n\r\n" in data:
    head, body = data.split(b"\r\n\r\n", 1)
    try:
        length = int(next(l.split(b":")[1] for l in head.split(b"\r\n")
                          if l.lower().startswith(b"content-length:")))
        while len(body) < length:
            c = os.read(fd, 65536)
            if not c:
                break
            body += c
    except StopIteration:
        pass
mode = os.environ.get("ADV_MODE", "ok")
if mode == "nonjson201":
    reply = b"HTTP/1.1 201 Created\r\nContent-Length: 8\r\nConnection: close\r\n\r\nnot-json"
elif mode == "canceled500":
    payload = b'{"message":"context canceled"}'
    reply = (b"HTTP/1.1 500 Internal Server Error\r\nContent-Type: application/json\r\n"
             b"Content-Length: %d\r\nConnection: close\r\n\r\n%s") % (len(payload), payload)
elif mode == "truncated":
    reply = b"HTTP/1.1 201 Crea"  # closes mid-status-line
elif mode == "noid201":
    payload = b'{"Warnings":[]}'
    reply = (b"HTTP/1.1 201 Created\r\nContent-Type: application/json\r\n"
             b"Content-Length: %d\r\nConnection: close\r\n\r\n%s") % (len(payload), payload)
elif mode == "nonzeroexit":
    sys.exit(3)  # no stdout -> create() sees a non-zero dial-stdio exit
else:
    payload = b'{"Id":"restored","Warnings":[]}'
    reply = (b"HTTP/1.1 201 Created\r\nContent-Type: application/json\r\n"
             b"Content-Length: %d\r\nConnection: close\r\n\r\n%s") % (len(payload), payload)
sys.stdout.buffer.write(reply)
"""


class DialStdioAdversarialTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="gonka-dial-adv-")
        self.fake = Path(self.temp.name) / "docker"
        self.fake.write_text(ADV_DIAL)
        self.fake.chmod(0o755)
        self.state = load_container_state()
        self.state.DOCKER = str(self.fake)

    def tearDown(self):
        os.environ.pop("ADV_MODE", None)
        self.temp.cleanup()

    def _create_raises(self, mode, needle):
        os.environ["ADV_MODE"] = mode
        with self.assertRaises(RuntimeError) as caught:
            self.state.create({"Image": "busybox"}, "proxy")
        self.assertIn(needle, str(caught.exception))

    def test_sanity_ok_reply_returns_id(self):
        self.assertEqual(self.state.create({"Image": "busybox"}, "proxy"), "restored")

    def test_non_json_201_body_is_rejected(self):
        self._create_raises("nonjson201", "non-JSON body")

    def test_context_canceled_500_is_named(self):
        # The Docker 28 variant of the same bug; create() must surface the message.
        self._create_raises("canceled500", "context canceled")

    def test_truncated_reply_is_error_not_hang(self):
        self._create_raises("truncated", "could not restore")

    def test_201_without_id_is_rejected(self):
        # A success status with no container id must never be treated as restored.
        self._create_raises("noid201", "could not restore")

    def test_dial_stdio_nonzero_exit_is_error(self):
        self._create_raises("nonzeroexit", "dial-stdio failed")


class RollbackAdversarialTest(rb.RollbackTest):
    def test_repeated_failures_restore_each_time(self):
        first = self.update("proxy")
        self.assertNotEqual(first.returncode, 0)
        self.assert_restored("proxy")
        self.assertFalse((self.root / "state/pending").exists())
        # Re-arm the one-shot failure and fail the same boundary again: the
        # rollback create must survive a second cancel and restore the original.
        (self.root / "injected").unlink()
        second = self.update("proxy")
        self.assertNotEqual(second.returncode, 0)
        self.assert_restored("proxy")
        self.assertFalse((self.root / "state/pending").exists())


if __name__ == "__main__":
    unittest.main(verbosity=2)
