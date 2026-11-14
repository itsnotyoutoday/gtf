#!/usr/bin/env python3
"""End-to-end check against the local Evennia test game (telnet, localhost:4392,
login `connect god layouttest`): runs gtf in a pty at 100x30, logs in, checks
that GMCP data arrives and the sidebar layout draws HP/MP bars, that typed
input reaches the game, and that panels vanish on a narrow terminal.

Skipped (exit 0) when the game is not listening.   Usage: python3 scripts/e2e_evennia.py
"""
import fcntl, os, pty, select, socket, struct, subprocess, sys, tempfile, termios, time

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
HOST, PORT = "localhost", 4392


def game_up():
    try:
        socket.create_connection((HOST, PORT), 1).close()
        return True
    except OSError:
        return False


class Session:
    def __init__(self, cmd, cols, rows, env):
        self.cols, self.rows = cols, rows
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            e = dict(os.environ, TERM="xterm-256color", **env)
            os.execvpe(cmd[0], cmd, e)
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
        self.buf = b""

    def drain(self, t):
        end = time.time() + t
        while time.time() < end:
            r, _, _ = select.select([self.fd], [], [], 0.05)
            if r:
                try:
                    d = os.read(self.fd, 65536)
                except OSError:
                    return
                if not d:
                    return
                self.buf += d

    def send(self, s, t=0.5):
        os.write(self.fd, s.encode())
        self.drain(t)

    def resize(self, cols, rows, t=0.8):
        self.cols, self.rows = cols, rows
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
        os.kill(self.pid, 28)  # SIGWINCH
        self.drain(t)

    def screen(self, vtdump):
        with tempfile.NamedTemporaryFile(delete=False) as f:
            f.write(self.buf)
        try:
            return subprocess.run([vtdump, "-w", str(self.cols), "-h", str(self.rows), f.name], capture_output=True, text=True, check=True).stdout
        finally:
            os.unlink(f.name)

    def close(self):
        try:
            os.kill(self.pid, 9)
            os.waitpid(self.pid, 0)
        except Exception:
            pass


def check(cond, what, screen=""):
    print(("ok   " if cond else "FAIL ") + what)
    if not cond:
        print(screen)
        sys.exit(1)


def main():
    if not game_up():
        print("Evennia test game not listening on %s:%d: skipping" % (HOST, PORT))
        return 0
    env = dict(os.environ, GOWORK="off")
    tmp = tempfile.gettempdir()
    vtdump, gtf = os.path.join(tmp, "gtf-vtdump"), os.path.join(tmp, "gtf-bin")
    subprocess.check_call(["go", "build", "-o", vtdump, "./cmd/vtdump"], cwd=ROOT, env=env)
    subprocess.check_call(["go", "build", "-o", gtf, "./cmd/gtf"], cwd=ROOT, env=env)
    home = tempfile.mkdtemp()
    s = Session([gtf, "-f", "-c/addworld ev god layouttest %s %d" % (HOST, PORT), "-c/layout use sidebar", "ev"], 100, 30, {"HOME": home})
    try:
        s.drain(3.0)
        scr = s.screen(vtdump)
        check("Connection to ev established" in scr, "connected", scr)
        check("You become god" in scr, "auto-login worked", scr)
        check("Vitals" in scr and "HP" in scr and "█" in scr, "sidebar draws HP bar from GMCP Char.Vitals", scr)
        s.send("look\r", 1.0)
        scr = s.screen(vtdump)
        check("Market Square" in scr, "typed input reaches the game", scr)
        s.send("/echo $[gmcp_get(\"Char.Vitals.hp\")]\r", 0.5)
        s.send("/test echo(strcat(\"hp=\", gmcp_get(\"Char.Vitals.hp\")))\r", 0.8)
        scr = s.screen(vtdump)
        check("hp=" in scr and "hp=\n" not in scr, "gmcp_get() readable from scripts", scr)
        s.send("/quit\r", 0.5)
    finally:
        s.close()
    # a narrow terminal hides the sidebar (its -minw is 80) but the client works
    n = Session([gtf, "-f", "-c/addworld ev god layouttest %s %d" % (HOST, PORT), "-c/layout use sidebar", "ev"], 70, 24, {"HOME": home})
    try:
        n.drain(3.0)
        scr = n.screen(vtdump)
        check("You become god" in scr and "Vitals" not in scr, "sidebar hidden on a 70 column terminal", scr)
    finally:
        n.close()
    print("e2e passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
