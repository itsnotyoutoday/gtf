#!/usr/bin/env python3
"""End-to-end check of /update -y -r on a really INSTALLED gtf, against the real GitHub releases.

  python3 scripts/update_pty.py /path/to/installed/bin/gtf [OLD_VERSION NEW_VERSION]

The binary must come from install.sh (a library next to it with install.json) and be OLDER than the newest
published release. The script starts it under a pseudo terminal with HOME pointing at a scratch directory,
connects to a fake game on localhost, types /update, then /update -y -r, and checks that:
  * /update says a newer version is available,
  * the process id is unchanged (the restart is an exec in place),
  * the game connection is the very same one (data flows both ways after the restart),
  * /version then shows the NEW version, and the installed binary and library are the new version on disk.
It changes the installation it is given, so give it a scratch install (install.sh --prefix /tmp/x).
"""
import json, os, pty, select, socket, subprocess, sys, tempfile, time

gtf = sys.argv[1]
old = sys.argv[2] if len(sys.argv) > 2 else None
new = sys.argv[3] if len(sys.argv) > 3 else None
home = tempfile.mkdtemp()
srv = socket.socket(); srv.bind(("127.0.0.1", 0)); srv.listen(1)
port = srv.getsockname()[1]

pid, fd = pty.fork()
if pid == 0:
    os.environ.update(HOME=home, TERM="xterm-256color", GTF_NO_UPDATE_CHECK="1")
    os.execv(gtf, [gtf, "-f", "-q", "-c/addworld w localhost %d" % port, "-c/connect w"])

screen = b""
def pump(t=0.3):
    global screen
    end = time.time() + t
    out = b""
    while time.time() < end:
        r, _, _ = select.select([fd], [], [], 0.05)
        if r:
            try:
                out += os.read(fd, 65536)
            except OSError:
                break
    screen += out
    return out

def need(cond, msg):
    if not cond:
        print("FAIL:", msg)
        # render what the terminal showed (needs the vtdump tool: go build -o /tmp/vtdump ./cmd/vtdump)
        open("/tmp/update_pty_screen.bin", "wb").write(screen)
        if os.path.exists("/tmp/vtdump"):
            print(subprocess.run(["/tmp/vtdump", "-w", "80", "-h", "24", "/tmp/update_pty_screen.bin"], capture_output=True, text=True).stdout)
        os.kill(pid, 9); sys.exit(1)

mark = 0  # everything before this has been matched already
def wait_for(text, secs):
    """Wait until `text` appears in the screen output after the previous match (the output may arrive in any chunking)."""
    global mark
    end = time.time() + secs
    while True:
        i = screen.find(text, mark)
        if i >= 0:
            mark = i + len(text)
            return True
        if time.time() >= end:
            return False
        pump(0.3)

srv.settimeout(10)
g, _ = srv.accept()
g.sendall(b"hello from the game\r\n")
need(wait_for(b"hello from the game", 5), "no game text")

os.write(fd, b"/update\r")
need(wait_for(b"Update available", 40), "/update did not report a newer version (is this install older than the newest release?)")
if new:
    need(new.encode() in screen, "newer version %s not named" % new)

os.write(fd, b"/update -y -r\r")
need(wait_for(b"Installed gtf", 90), "the update was not installed")
need(wait_for(b"Restarting into", 10), "no restart announced")
time.sleep(3)
pump(1.0)
need(os.waitpid(pid, os.WNOHANG) == (0, 0), "gtf ended: an exec in place must keep the pid")
g.sendall(b"still connected after the update\r\n")
need(wait_for(b"still connected after the update", 6), "game data did not arrive after the restart")
os.write(fd, b"look\r")
g.settimeout(5)
data = b""
while b"look\r\n" not in data:
    data += g.recv(100)
os.write(fd, b"/version\r")
pump(1.5)

def rendered():
    """What the terminal shows (escape sequences resolved), via the vtdump tool when it is built."""
    open("/tmp/update_pty_screen.bin", "wb").write(screen)
    if os.path.exists("/tmp/vtdump"):
        return subprocess.run(["/tmp/vtdump", "-w", "80", "-h", "24", "/tmp/update_pty_screen.bin"], capture_output=True, text=True).stdout
    return screen.decode(errors="replace")

if new:
    need(("version %s" % new) in rendered(), "/version does not show %s after the restart" % new)
os.write(fd, b"/quit\r")
pump(1.0)
os.waitpid(pid, 0)

# the installation on disk is the new version too
exe = os.path.realpath(gtf)
out = subprocess.run([exe, "--version"], capture_output=True, text=True).stdout
if new:
    need(new in out, "installed binary says %r" % out)
lib = os.path.join(os.path.dirname(os.path.dirname(exe)), "lib", "gtf", "install.json")
rec = json.load(open(lib))
if new:
    need(rec["version"] == new, "install.json says %s" % rec["version"])
print("ok: %s -> %s via /update -y -r: same pid, same game connection, new version running, installation updated" % (old, new))
