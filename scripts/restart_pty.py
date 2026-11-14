#!/usr/bin/env python3
"""End-to-end check of /restart on the real gtf binary under a pseudo terminal.

  python3 scripts/restart_pty.py /path/to/gtf

A fake game listens on localhost; gtf connects, the script types /restart, and
checks that: the process id is unchanged (exec in place), the game connection is
the same one (the game keeps receiving and sending on it), the terminal is still
raw while gtf runs, and after /quit the terminal settings are exactly those the
shell had before gtf started (the new process restored the ORIGINAL settings, not
the raw ones it found).
"""
import os, pty, select, socket, sys, tempfile, termios, time

gtf = sys.argv[1]
home = tempfile.mkdtemp()
srv = socket.socket(); srv.bind(("127.0.0.1", 0)); srv.listen(1)
port = srv.getsockname()[1]

pid, fd = pty.fork()
if pid == 0:
    os.environ.update(HOME=home, TERM="xterm-256color")
    os.execv(gtf, [gtf, "-f", "-q", "-c/addworld w localhost %d" % port, "-c/connect w"])
orig = termios.tcgetattr(fd)  # what a shell would have set before gtf starts raw mode (may already be raw if racing)

def pump(t=0.3):
    out = b""
    end = time.time() + t
    while time.time() < end:
        r, _, _ = select.select([fd], [], [], 0.05)
        if r:
            try:
                out += os.read(fd, 65536)
            except OSError:
                break
    return out

def need(cond, msg):
    if not cond:
        print("FAIL:", msg); os.kill(pid, 9); sys.exit(1)

srv.settimeout(10)
g, _ = srv.accept()
g.sendall(b"hello from the game\r\npartial ")
screen = pump(1.0)
need(b"hello from the game" in screen, "no game text")
raw = termios.tcgetattr(fd)
need(not (raw[3] & termios.ICANON), "terminal is not raw while gtf runs")
os.write(fd, b"/restart\r")
screen = pump(3.0)
g.sendall(b"line completed\r\nafter exec\r\n")
screen = pump(1.0)
need(b"after exec" in screen, "the game's data did not arrive after the restart")
need(os.waitpid(pid, os.WNOHANG) == (0, 0), "gtf ended (same pid must survive an exec)")
now = termios.tcgetattr(fd)
need(not (now[3] & termios.ICANON), "terminal not raw after the restart")
os.write(fd, b"look\r")
g.settimeout(5)
data = b""
while b"look\r\n" not in data:
    data += g.recv(100)
os.write(fd, b"/quit\r")
pump(1.0)
_, status = os.waitpid(pid, 0)
need(os.WIFEXITED(status) and os.WEXITSTATUS(status) == 0, "exit status %r" % status)
final = termios.tcgetattr(fd)
need(final[3] & termios.ICANON and final[3] & termios.ECHO, "terminal not restored to cooked mode after quit: lflag %x" % final[3])
print("ok: pid kept, connection kept, terminal raw while running and restored after quit")
