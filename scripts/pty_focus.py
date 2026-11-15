#!/usr/bin/env python3
"""Real-terminal focus walk: drives a built gtf in a pty with the real byte sequences
(Shift+Tab, Tab, Down, Enter, Esc) against the Evennia PoC on 127.0.0.1:4101 and checks the status
line (the hint row above the world line) after each key. Skipped (exit 0) when the game is down.
usage: pty_focus.py /path/to/gtf /path/to/vtdump"""
import fcntl, os, pty, select, socket, struct, subprocess, sys, tempfile, termios, time

gtf, vtdump = sys.argv[1], sys.argv[2]
try:
    socket.create_connection(("127.0.0.1", 4101), 1).close()
except OSError:
    print("SKIP: the game is not up"); sys.exit(0)
COLS, ROWS = 160, 50
home = tempfile.mkdtemp()
pid, fd = pty.fork()
if pid == 0:
    os.execvpe(gtf, [gtf, "-f", "127.0.0.1", "4101"], dict(os.environ, TERM="xterm-256color", HOME=home))
fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
buf = b""
def drain(t):
    global buf
    end = time.time() + t
    while time.time() < end:
        if select.select([fd], [], [], 0.05)[0]:
            try: d = os.read(fd, 65536)
            except OSError: return
            if not d: return
            buf += d
def send(s, wait=1.5): os.write(fd, s.encode()); drain(wait)
def hint():
    p = os.path.join(home, "cur.bin"); open(p, "wb").write(buf)
    rows = subprocess.run([vtdump, "-w", str(COLS), "-h", str(ROWS), p], capture_output=True, text=True).stdout.splitlines()
    for r in rows:
        r = r.split("|", 1)[-1].strip()
        if r.startswith(("INPUT -", "MENU ", "PANEL ", "SCROLLED BACK")): return r
    return ""
drain(2)
for line in ["connect bob cobalt-fern-52", "board resolute", "man science"]: send(line + "\r", 2.0)
send("/cmcp pack allow\r", 3.0)
send("man science\r", 2.0)
drain(1)
bad = []
def step(key, name, want):
    send(key, 1.5); h = hint(); ok = want in h or (want == "PANEL " and h.startswith("MENU ")) # a screen with no panels (a ship room) has only the menu
    print(("ok   " if ok else "FAIL ") + f"{name:10}: {h[:110]}")
    if not ok: bad.append(name)
step("", "start", "INPUT - Tab or F2")
step("\t", "Tab", "MENU ")
step("\x1b[B", "Down", "MENU ")
step("\x1b[B", "Down", "MENU ")
step("\r", "Enter", "PANEL ")
step("\t", "Tab", "PANEL ")
step("\x1b[Z", "Shift+Tab", "PANEL ")
step("\x1b", "Esc", "INPUT - Tab or F2")
step("\x1b[Z", "Shift+Tab", "PANEL ")
step("\x1b", "Esc", "INPUT - Tab or F2")
# synthetic SGR mouse wheel reports (64 up, 65 down) over the game-output pane: the history scrolls back
# and returns, the hint row says so
for _ in range(60): send("/echo wheel history line\r", 0.05)
drain(1)
for _ in range(4): send("\x1b[<64;40;15M", 0.2)
h = hint(); ok = h.startswith("SCROLLED BACK"); print(("ok   " if ok else "FAIL ") + f"{'wheel up':10}: {h[:80]}")
if not ok: bad.append("wheel up")
for _ in range(30): send("\x1b[<65;40;15M", 0.1)
h = hint(); ok = not h.startswith("SCROLLED BACK"); print(("ok   " if ok else "FAIL ") + f"{'wheel down':10}: {h[:80]}")
if not ok: bad.append("wheel down")
os.write(fd, b"/quit\r"); drain(1)
screen_text = buf.decode("utf-8", "replace")
if "key_shift_tab" in screen_text or "no such command or macro" in screen_text:
    bad.append("'%' key error line"); print("FAIL: a '%' key error line was printed")
sys.exit(1 if bad else 0)
