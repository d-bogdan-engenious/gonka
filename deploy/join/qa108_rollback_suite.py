#!/usr/bin/env python3
"""QA-108 — QA-authored test suite for the dial-stdio rollback fix (PR #2193).

Independent HAPPY-PATH + ADVERSARIAL/NEGATIVE coverage written by QA. It reuses
only the test *scaffolding* from the shipped updater-rollback_test.py (the compose
project setup, the failure-injection docker-wrapper, and load_container_state) —
every test case and assertion below is ours.

Each QA case is named test_qa_* so the suite can be run in isolation:
    python3 qa108_rollback_suite.py -v -k qa
On the #2193 updater every test_qa_* passes; on the pre-#2193 updater the
happy-path cases FAIL (the rollback create is cancelled), which is how our own
tests reproduce the bug.
"""
import importlib.util
import os
from pathlib import Path
import tempfile
import unittest

SOURCE = Path(__file__).resolve().parent

# Reuse the scaffolding only (compose setup + failure injection + state loader).
_spec = importlib.util.spec_from_file_location("rollback_base", SOURCE / "updater-rollback_test.py")
_rb = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_rb)
load_container_state = _rb.load_container_state
DOCKER = _rb.DOCKER
command = _rb.command


# ---------------------------------------------------------------------------
# HAPPY PATH (QA): a failed update must roll the previous container back.
# These reuse RollbackTest's setUp (builds the busybox compose project + the
# docker-wrapper that fails one service's `compose up -d`). The assertions are
# ours. On pre-#2193 these FAIL -> our own reproduction of the bug.
# ---------------------------------------------------------------------------
class QaRollbackHappyPath(_rb.RollbackTest):
    def _assert_restored_to_previous(self, service):
        current, old = self.inspect(service), self.before[service]
        self.assertIsNotNone(current, f"{service} was not restored after the failed update")
        self.assertEqual(current["Image"], old["Image"], f"{service}: image is not the previous one")
        self.assertEqual(current["Config"]["Env"], old["Config"]["Env"], f"{service}: release/env not restored")
        self.assertEqual(current["State"]["Health"]["Status"], "healthy", f"{service}: not healthy after rollback")
        self.assertFalse((self.root / "state/pending").exists(), "pending state left behind")

    def test_qa_failed_proxy_update_restores_previous_and_preserves_data(self):
        result = self.update("proxy")
        self.assertNotEqual(result.returncode, 0, "update should have failed")
        self._assert_restored_to_previous("proxy")
        pid = self.inspect("proxy")["Id"]
        self.assertEqual(command(DOCKER, "exec", pid, "cat", "/data/value").stdout.strip(), "preserved",
                         "named volume data lost during rollback")
        self.assertEqual(command(DOCKER, "exec", pid, "cat", "/config/value").stdout, "old",
                         "previous bind-mounted config not restored")
        self.assertIsNone(self.inspect("proxy-policy"), "half-applied candidate policy not torn down")
        self.assertIsNone(self.inspect("proxy-policy2"), "half-applied candidate policy2 not torn down")

    def test_qa_failed_versiond_update_restores_previous(self):
        result = self.update("versiond")
        self.assertNotEqual(result.returncode, 0)
        self._assert_restored_to_previous("versiond")

    def test_qa_failed_policy_update_restores_public_group(self):
        result = self.update("proxy-policy")
        self.assertNotEqual(result.returncode, 0)
        self._assert_restored_to_previous("proxy")
        self.assertIsNone(self.inspect("proxy-policy"))
        self.assertIsNone(self.inspect("proxy-policy2"))

    def test_qa_candidate_healthcheck_failure_restores_previous(self):
        # Adversarial variant of happy path: the new container starts but never
        # becomes healthy -> the updater must still roll back to the old one.
        import json
        self.candidate["services"]["versiond"]["healthcheck"]["test"] = ["CMD", "false"]
        self.compose_file.write_text(json.dumps(self.candidate))
        result = self.update()
        self.assertNotEqual(result.returncode, 0)
        self._assert_restored_to_previous("versiond")


# ---------------------------------------------------------------------------
# ADVERSARIAL / NEGATIVE (QA): drive the real create() against hostile daemon
# replies via a controllable fake `docker system dial-stdio`.
# ---------------------------------------------------------------------------
_ADV_DIAL = r"""#!/usr/bin/env python3
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
    sys.exit(3)
else:
    payload = b'{"Id":"restored","Warnings":[]}'
    reply = (b"HTTP/1.1 201 Created\r\nContent-Type: application/json\r\n"
             b"Content-Length: %d\r\nConnection: close\r\n\r\n%s") % (len(payload), payload)
sys.stdout.buffer.write(reply)
"""


class QaCreateAdversarial(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="gonka-qa-dial-")
        self.fake = Path(self.temp.name) / "docker"
        self.fake.write_text(_ADV_DIAL)
        self.fake.chmod(0o755)
        self.state = load_container_state()
        self.state.DOCKER = str(self.fake)

    def tearDown(self):
        os.environ.pop("ADV_MODE", None)
        self.temp.cleanup()

    def _raises(self, mode, needle):
        os.environ["ADV_MODE"] = mode
        with self.assertRaises(RuntimeError) as caught:
            self.state.create({"Image": "busybox"}, "proxy")
        self.assertIn(needle, str(caught.exception))

    def test_qa_create_sanity_ok_reply_returns_id(self):
        self.assertEqual(self.state.create({"Image": "busybox"}, "proxy"), "restored")

    def test_qa_create_rejects_non_json_201_body(self):
        self._raises("nonjson201", "non-JSON body")

    def test_qa_create_names_context_canceled_500(self):
        self._raises("canceled500", "context canceled")

    def test_qa_create_errors_on_truncated_reply_not_hang(self):
        self._raises("truncated", "could not restore")

    def test_qa_create_rejects_201_without_id(self):
        self._raises("noid201", "could not restore")

    def test_qa_create_errors_on_nonzero_dial_stdio_exit(self):
        self._raises("nonzeroexit", "dial-stdio failed")


# ---------------------------------------------------------------------------
# STRESS (QA): the rollback create must survive more than one cancel.
# ---------------------------------------------------------------------------
class QaRollbackStress(_rb.RollbackTest):
    def test_qa_repeated_failures_restore_each_time(self):
        first = self.update("proxy")
        self.assertNotEqual(first.returncode, 0)
        self.assert_restored("proxy")
        self.assertFalse((self.root / "state/pending").exists())
        (self.root / "injected").unlink()  # re-arm the one-shot failure
        second = self.update("proxy")
        self.assertNotEqual(second.returncode, 0)
        self.assert_restored("proxy")
        self.assertFalse((self.root / "state/pending").exists())


if __name__ == "__main__":
    unittest.main(verbosity=2)
