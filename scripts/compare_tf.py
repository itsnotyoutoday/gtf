#!/usr/bin/env python3
"""Run real tf and gtf in ptys with the same scripted keystrokes, render both
byte streams through the Go vt emulator (cmd/vtdump) and diff the screens.

    . scripts/env.sh            # sets TF_REF / TF_LIB
    python3 scripts/compare_tf.py [-v] [scenario...]

Exit status 1 if any scenario differs.  The startup banner differs on purpose,
so every scenario first scrolls it away with a /for loop of /echo lines.
"""
import fcntl, os, pty, select, struct, subprocess, sys, tempfile, termios, time

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
FLUSH = "/for i 1 30 /echo scrolled %i\r"

SCENARIOS = {
    "startup": [],
    "echo": [FLUSH, "/echo hello\r", "abc"],
    "typing": [FLUSH, "hello world"],
    "wrap": [FLUSH, "/echo " + "word " * 40 + "\r"],
    "wrap-long-word": [FLUSH, "/echo " + "x" * 200 + "\r"],
    "input-lines": [FLUSH, "/echo one\r", "/echo two\r", "/echo three\r", "xyz"],
    "ctrl-edit": [FLUSH, "abc def ghi", "\x01", "\x0b", "xyz"],
    "word-move": [FLUSH, "abc def ghi", "\x1bb", "\x1bb", "X"],
    "kill-word": [FLUSH, "abc def ghi", "\x17", "Q"],
    "history": [FLUSH, "/echo first\r", "/echo second\r", "\x10", "\x10", "!"],
    "long-input": [FLUSH, "q" * 100],
    "more-off-wrapped-list": [FLUSH, "/set more=on\r", "/for i 1 40 /echo m%i\r", "/echo end\r"],
    "status-clock-off": [FLUSH, "/clock off\r"],
    "unicode": [FLUSH, "/echo caf\u00e9 \u00fcber \u20ac\r", "na\u00efve"],
}


def run(cmd, steps, cols=80, rows=24, wait0=1.5, delay=0.25, env=None):
    pid, fd = pty.fork()
    if pid == 0:
        e = dict(os.environ)
        e.update(env or {})
        e["TERM"] = "xterm"
        os.execvpe(cmd[0], cmd, e)
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
    buf = b""

    def drain(t):
        nonlocal buf
        end = time.time() + t
        while time.time() < end:
            r, _, _ = select.select([fd], [], [], 0.05)
            if r:
                try:
                    d = os.read(fd, 65536)
                except OSError:
                    return
                if not d:
                    return
                buf += d

    drain(wait0)
    for s in steps:
        try:
            os.write(fd, s.encode())
        except OSError:
            break
        drain(delay + (0.6 if s.startswith("/for") else 0))
    drain(0.3)
    try:
        os.kill(pid, 9)
        os.waitpid(pid, 0)
    except Exception:
        pass
    return buf


def render(vtdump, data, cols=80, rows=24):
    with tempfile.NamedTemporaryFile(delete=False) as f:
        f.write(data)
    try:
        out = subprocess.run([vtdump, "-w", str(cols), "-h", str(rows), f.name], capture_output=True, text=True, check=True).stdout
    finally:
        os.unlink(f.name)
    return out.splitlines()


def normalize(lines):
    out = []
    for l in lines:
        if l.startswith("cursor:"):
            out.append(l)
            continue
        body = l.split("|", 1)[1] if "|" in l else l
        # the clock differs between the two runs only by timing: mask HH:MM
        import re
        body = re.sub(r"\b\d\d:\d\d$", "HH:MM", body.rstrip())
        out.append(body)
    return out


def main():
    args = [a for a in sys.argv[1:] if not a.startswith("-")]
    verbose = "-v" in sys.argv
    tf, lib = os.environ.get("TF_REF"), os.environ.get("TF_LIB")
    if not tf or not os.path.exists(tf):
        print("TF_REF not set (source scripts/env.sh); skipping")
        return 0
    vtdump = os.path.join(tempfile.gettempdir(), "gtf-vtdump")
    gtf = os.path.join(tempfile.gettempdir(), "gtf-bin")
    env = dict(os.environ, GOWORK="off")
    subprocess.check_call(["go", "build", "-o", vtdump, "./cmd/vtdump"], cwd=ROOT, env=env)
    subprocess.check_call(["go", "build", "-o", gtf, "./cmd/gtf"], cwd=ROOT, env=env)
    home = tempfile.mkdtemp()
    names = args or list(SCENARIOS)
    bad = 0
    for name in names:
        steps = SCENARIOS[name]
        a = render(vtdump, run([tf, "-n", "-f", "-L" + lib], steps, env={"HOME": home}))
        b = render(vtdump, run([gtf, "-n", "-f"], steps, env={"HOME": home}))
        na, nb = normalize(a), normalize(b)
        if name == "startup":
            na, nb = na[-5:], nb[-5:]  # status + input rows + cursor only
        # the leading lines of scrolled output are identical; compare all rows
        if na == nb:
            print("ok   ", name)
            continue
        bad += 1
        print("DIFF ", name)
        for i, (x, y) in enumerate(zip(na, nb)):
            if x != y:
                print("   row %2d tf : %r\n          gtf: %r" % (i + 1, x, y))
        if verbose:
            print("--- tf\n" + "\n".join(a) + "\n--- gtf\n" + "\n".join(b))
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
