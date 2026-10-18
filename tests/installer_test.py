#!/usr/bin/env python3
import fcntl
import os
import pty
import select
import signal
import subprocess
import struct
import termios
import time
import unittest

INSTALLER = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "install.sh"))


def run_tty(steps, overrides=None):
    pid, fd = pty.fork()
    if pid == 0:
        env = os.environ.copy()
        env.update(TERM="dumb", NO_COLOR="1")
        if overrides:
            env.update(overrides)
        os.execve("/bin/bash", ["bash", INSTALLER], env)

    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 80, 0, 0))
    output = bytearray()

    def until(marker, start):
        deadline = time.monotonic() + 8
        while marker.encode() not in output[start:]:
            if time.monotonic() >= deadline:
                os.kill(pid, signal.SIGKILL)
                raise AssertionError(f"timed out waiting for {marker!r}; output: {output.decode(errors='replace')}")
            ready, _, _ = select.select([fd], [], [], 0.25)
            if ready:
                try:
                    output.extend(os.read(fd, 8192))
                except OSError:
                    break

    try:
        start = 0
        for prompt, answer in steps:
            until(prompt, start)
            start = len(output)
            if answer is None:
                os.write(fd, b"\x04")
                break
            if answer == "<CTRL-C>":
                os.write(fd, b"\x03")
                break
            os.write(fd, answer.encode() + b"\n")
        deadline = time.monotonic() + 8
        status = None
        while time.monotonic() < deadline:
            ready, _, _ = select.select([fd], [], [], 0.25)
            if ready:
                try:
                    output.extend(os.read(fd, 8192))
                except OSError:
                    pass
            got, wait_status = os.waitpid(pid, os.WNOHANG)
            if got:
                status = os.waitstatus_to_exitcode(wait_status)
                break
        if status is None:
            os.kill(pid, signal.SIGKILL)
            raise AssertionError(f"installer did not exit; output: {output.decode(errors='replace')}")
        return status, output.decode(errors="replace")
    finally:
        os.close(fd)


class InstallerMenuTest(unittest.TestCase):
    def test_noninteractive_install_is_rejected_cleanly(self):
        result = subprocess.run(["/bin/bash", INSTALLER], capture_output=True, text=True, check=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("interactive menu requires a terminal", result.stderr)

    def test_process_substitution_entry_point(self):
        result = subprocess.run(
            ["bash", "-c", 'bash <(curl -fsSL "file://$1") --help', "ipv8-installer-test", INSTALLER],
            capture_output=True,
            text=True,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Termux on Android arm64", result.stdout)

    def test_dry_run_rejects_invalid_panel_origin_without_writes(self):
        result = subprocess.run(
            ["/bin/bash", INSTALLER, "--dry-run", "--server", "ftp://example.com"],
            capture_output=True,
            text=True,
            check=False,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("server must be a valid HTTPS origin", result.stderr)

    def test_invalid_choice_dns_guide_and_exit(self):
        status, output = run_tty([
            ("Choose an option [1-12]:", "99"),
            ("Choose an option [1-12]:", "8"),
            ("Press Enter to return to the menu.", ""),
            ("Choose an option [1-12]:", "12"),
        ])
        self.assertEqual(status, 0, output)
        self.assertIn("Invalid selection", output)
        self.assertIn("[12] Exit", output)
        self.assertIn("██████╗", output)
        self.assertIn("v=ipv8-1; address=YOUR_CANONICAL_IPV8_ADDRESS", output)
        self.assertIn("does not create IPv8 transport", output)
        self.assertIn("Goodbye. The menu did not make system changes", output)

    def test_panel_menu_disables_systemd_on_termux(self):
        status, output = run_tty([
            ("Choose an option [1-12]:", "1"),
            ("Press Enter to return to the menu.", ""),
            ("Choose an option [1-12]:", "12"),
        ], {"PREFIX": "/data/data/com.termux/files/usr", "TERMUX_VERSION": "test"})
        self.assertEqual(status, 0, output)
        self.assertIn("Panel systemd installation is not supported on Termux", output)
        self.assertNotIn("Downloading the published", output)

    def test_terms_rejection_cancels_before_download(self):
        status, output = run_tty([
            ("Choose an option [1-12]:", "2"),
            ("Installation name [", ""),
            ("Installation directory / binary prefix [", ""),
            ("Public IPv8 panel URL (HTTPS) [", "https://panel.example"),
            ("Accept that IPv8 is experimental", "n"),
            ("Press Enter to return to the menu.", ""),
            ("Choose an option [1-12]:", "12"),
        ])
        self.assertEqual(status, 0, output)
        self.assertIn("Terms were not accepted", output)
        self.assertNotIn("Downloading the published", output)
        self.assertIn("Goodbye. The menu did not make system changes", output)

    def test_eof_at_menu_exits_without_changes(self):
        status, output = run_tty([("Choose an option [1-12]:", None)])
        self.assertNotEqual(status, 0)
        self.assertIn("input ended; no installation changes were made", output)

    def test_ctrl_c_restores_terminal_and_exits_cleanly(self):
        status, output = run_tty([("Choose an option [1-12]:", "<CTRL-C>")])
        self.assertEqual(status, 130, output)
        self.assertIn("setup cancelled. No system files or services were changed", output)

    def test_review_cancel_does_not_download_or_install(self):
        status, output = run_tty([
            ("Choose an option [1-12]:", "2"),
            ("Installation name [", "node-01"),
            ("Installation directory / binary prefix [", ""),
            ("Public IPv8 panel URL (HTTPS) [", "https://panel.example"),
            ("Accept that IPv8 is experimental", "y"),
            ("One-use enrollment token (input hidden):", "abcdefghijklmnop"),
            ("Choose an option [1-3]:", "3"),
            ("Press Enter to return to the menu.", ""),
            ("Choose an option [1-12]:", "12"),
        ])
        self.assertEqual(status, 0, output)
        self.assertIn("node-01", output)
        self.assertIn("Configuration          :", output)
        self.assertIn("Installation cancelled; no download or system changes made", output)
        self.assertNotIn("abcdefghijklmnop", output)
        self.assertNotIn("Downloading the published", output)

    def test_review_edit_returns_to_configuration(self):
        status, output = run_tty([
            ("Choose an option [1-12]:", "2"),
            ("Installation name [", "first-node"),
            ("Installation directory / binary prefix [", ""),
            ("Public IPv8 panel URL (HTTPS) [", "https://panel.example"),
            ("Accept that IPv8 is experimental", "y"),
            ("One-use enrollment token (input hidden):", "abcdefghijklmnop"),
            ("Choose an option [1-3]:", "2"),
            ("Installation name [first-node]", "second-node"),
            ("Installation directory / binary prefix [", ""),
            ("Public IPv8 panel URL (HTTPS) [https://panel.example]", ""),
            ("Accept that IPv8 is experimental", "y"),
            ("One-use enrollment token (input hidden):", "qrstuvwxyzabcdef"),
            ("Choose an option [1-3]:", "3"),
            ("Press Enter to return to the menu.", ""),
            ("Choose an option [1-12]:", "12"),
        ])
        self.assertEqual(status, 0, output)
        self.assertIn("second-node", output)
        self.assertIn("Installation cancelled", output)
        self.assertNotIn("abcdefghijklmnop", output)
        self.assertNotIn("qrstuvwxyzabcdef", output)
        self.assertNotIn("Downloading the published", output)


if __name__ == "__main__":
    unittest.main()
