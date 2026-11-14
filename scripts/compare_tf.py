#!/usr/bin/env python3
"""Run real tf and gtf in ptys with the same scripted keystrokes, render both
byte streams through the Go vt emulator (cmd/vtdump) and diff the screens.

    . scripts/env.sh            # sets TF_REF / TF_LIB
    python3 scripts/compare_tf.py [-v] [scenario...]

Exit status 1 if any scenario differs.  The startup banner differs on purpose,
so every scenario first scrolls it away with a /for loop of /echo lines.
"""
import fcntl, os, pty, re, select, socket, struct, subprocess, sys, tempfile, termios, threading, time

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
    "help-intro": [FLUSH, "/help intro\r"],
    "help-cmd": [FLUSH, "/help /echo\r"],
    "listworlds": [FLUSH, "/addworld b hostb 2\r", "/addworld a hosta 1\r", "/listworlds\r"],
    "recall": [FLUSH, "/echo aaa\r", "/echo bbb\r", "/recall -3\r"],
    "limit": [FLUSH, "/echo xa\r", "/echo yb\r", "/echo xc\r", "/limit -m glob x*\r"],
    "status-add": [FLUSH, "/status_add -A @world foo:6\r"],
    "set-visual-off": [FLUSH, "/visual off\r", "/echo plain\r", "typed"],
    "more-page": [FLUSH, "/set more=on\r", "/for i 1 30 /echo n%i\r", "\t"],
    "list-def": [FLUSH, "/def foo = /echo hi\r", "/list foo\r"],
    "unicode": [FLUSH, "/echo caf\u00e9 \u00fcber \u20ac\r", "na\u00efve"],
}


# ---- connection lifecycle -------------------------------------------------
# Server n listens on port 47100+n and is written @n in keystrokes; ports @1 and @3 have no
# listener (connection refused).  {"servers": {n: banner}, "steps": [...]}; ("wait", s),
# ("send", n, bytes), ("close", n) act on the servers.  The bytes the servers received are
# compared as well.
W = ("wait", 0.6)
AB = ["/addworld A localhost @2\r", "/addworld B localhost @4\r"]
SRV = {2: b"AAA\r\n", 4: b"BBB\r\n"}
CONN = {
    "conn-refused": {"steps": [FLUSH, "/connect localhost @1\r", W]},
    "conn-refused-twice": {"steps": [FLUSH, "/connect localhost @1\r", W, "/connect localhost @3\r", W]},
    "conn-telnet-refused": {"steps": [FLUSH, "/telnet localhost @1\r", W, "/telnet localhost @3\r", W]},
    "conn-dns-fail": {"steps": [FLUSH, "/connect nosuchhost.invalid 1\r", ("wait", 2)]},
    "conn-no-such-world": {"steps": [FLUSH, "/connect zork\r", W]},
    "conn-ok": {"servers": {2: b"Welcome\r\nline two\r\n"}, "steps": [FLUSH, "/connect localhost @2\r", W]},
    "conn-telnet-ok": {"servers": {2: b"Welcome\r\n"}, "steps": [FLUSH, "/telnet localhost @2\r", W]},
    "conn-ok-then-unnamed": {"servers": {2: b"Welcome\r\n"}, "steps": [FLUSH, "/connect localhost @1\r", W, "/connect localhost @2\r", W, "/listsockets\r"]},
    "conn-second-world": {"servers": SRV, "steps": [FLUSH] + AB + ["/connect A\r", W, "/connect B\r", W, "/listsockets\r"]},
    "conn-bg-flag": {"servers": SRV, "steps": [FLUSH] + AB + ["/connect A\r", W, "/connect -b B\r", W, "/listsockets\r"]},
    "conn-bg-output": {"servers": SRV, "steps": [FLUSH] + AB + ["/connect A\r", W, "/connect B\r", W,
        ("send", 2, b"bg line\r\n"), W, ("send", 2, b"bg line two\r\n"), W, "/listsockets\r"]},
    "conn-fg-switch": {"servers": SRV, "steps": [FLUSH] + AB + ["/connect A\r", W, "/connect B\r", W,
        ("send", 2, b"bg line\r\n"), W, "/fg A\r", W, ("send", 4, b"from b\r\n"), W, "/fg B\r", W]},
    "conn-fg-none": {"servers": SRV, "steps": [FLUSH] + AB + ["/connect A\r", W, "/fg -n\r", W, "/fg A\r", W, "/fg -n\r", "/echo x\r"]},
    "conn-fg-cycle": {"servers": SRV, "steps": [FLUSH] + AB + ["/connect A\r", W, "/connect B\r", W, "/fg -<\r", W, "/fg >\r", W, "/fg\r", W]},
    "conn-fg-missing": {"servers": SRV, "steps": [FLUSH] + AB + ["/fg A\r", "/fg\r", "/fg -s A\r"]},
    "conn-dc": {"servers": SRV, "steps": [FLUSH] + AB + ["/connect A\r", W, "/dc\r", W, "/dc\r", "/listsockets\r"]},
    "conn-dc-name": {"servers": SRV, "steps": [FLUSH] + AB + ["/connect A\r", W, "/connect B\r", W, "/dc A\r", W, "/listsockets\r", "/dc zork\r"]},
    "conn-dc-none": {"steps": [FLUSH, "/dc\r", "/dc foo\r", "/dc -all\r"]},
    "conn-dc-all": {"servers": SRV, "steps": [FLUSH] + AB + ["/connect A\r", W, "/connect B\r", W, "/dc -all\r", W, "/listsockets\r"]},
    "conn-foreign-close": {"servers": SRV, "steps": [FLUSH] + AB + ["/connect A\r", W, ("close", 2), W, "/listsockets\r"]},
    "conn-foreign-close-bg": {"servers": SRV, "steps": [FLUSH] + AB + ["/connect A\r", W, "/connect B\r", W, ("close", 2), W, "/listsockets\r", "/fg A\r", W]},
    "conn-reconnect": {"servers": SRV, "steps": [FLUSH] + AB + ["/connect A\r", W, ("close", 2), W, "/connect A\r", W, "/listsockets\r"]},
    "conn-reconnect-live": {"servers": SRV, "steps": [FLUSH] + AB + ["/connect A\r", W, "/connect A\r", W]},
    "conn-listsockets-empty": {"steps": [FLUSH, "/listsockets\r"]},
    "conn-world-cmd": {"servers": SRV, "steps": [FLUSH] + AB + ["/world A\r", W, "/world\r", W]},
    "conn-addworld-connect": {"servers": SRV, "steps": [FLUSH, "/addworld mud localhost @2\r", "/listworlds\r", "/connect mud\r", W, "/listworlds\r"]},
    "conn-login": {"servers": {2: b"Welcome\r\n"}, "steps": [FLUSH, "/addworld -T- A joe sekrit localhost @2\r", "/connect A\r", W]},
    "conn-login-off": {"servers": {2: b"Welcome\r\n"}, "steps": [FLUSH, "/addworld -T- A joe sekrit localhost @2\r", "/connect -l A\r", W]},
    "conn-send": {"servers": {2: b"Welcome\r\n"}, "steps": [FLUSH, "/connect localhost @2\r", W, "hello there\r", W, ("send", 2, b"reply\r\n"), W]},
    "conn-hooks": {"servers": SRV, "steps": [FLUSH, "/def -hPENDING = /echo [pending %*]\r", "/def -hCONNECT = /echo [connect %*]\r",
        "/def -hDISCONNECT = /echo [disconnect %*]\r", "/def -hCONFAIL = /echo [confail %*]\r", "/def -hWORLD = /echo [world %*]\r",
        "/connect localhost @1\r", W, "/connect localhost @2\r", W, ("close", 2), W]},
    "conn-refused-while-connected": {"servers": SRV, "steps": [FLUSH] + AB + ["/connect A\r", W, "/connect localhost @1\r", W, "/listsockets\r", "/fg A\r", W, "/listsockets\r"]},
    "conn-refused-bg": {"servers": SRV, "steps": [FLUSH] + AB + ["/connect A\r", W, "/connect -b localhost @1\r", W, "/listsockets\r", "/fg (unnamed1)\r", W]},
    "conn-refused-then-connect": {"servers": SRV, "steps": [FLUSH] + AB + ["/connect localhost @1\r", W, "/connect A\r", W, "/listsockets\r"]},
    "conn-fg-flag": {"servers": SRV, "steps": [FLUSH] + AB + ["/connect A\r", W, "/connect -b B\r", W, ("send", 4, b"bg\r\n"), W, "/fg -q B\r", W]},
    "conn-fg-dead": {"servers": SRV, "steps": [FLUSH] + AB + ["/connect A\r", W, "/connect B\r", W, ("close", 2), W, "/fg A\r", W, "/listsockets\r", "/fg B\r", W, "/listsockets\r"]},
    "conn-send-dead": {"servers": SRV, "steps": [FLUSH] + AB + ["/connect A\r", W, ("close", 2), W, "hello\r", W]},
    "conn-send-none": {"steps": [FLUSH, "hello\r", "/send foo\r"]},
    "conn-bad-commands": {"steps": [FLUSH, "/disconnect\r", "/exit\r", "/quit_not\r"]},
    "conn-textdiv": {"servers": SRV, "steps": [FLUSH, "/set textdiv_str=--- new ---\r"] + AB + ["/connect A\r", W, "/connect B\r", W, ("send", 2, b"bg 1\r\n"), ("send", 2, b"bg 2\r\n"), W, "/fg A\r", W]},
    "conn-long-bg-more": {"servers": SRV, "steps": [FLUSH] + AB + ["/connect A\r", W, "/connect B\r", W, ("send", 2, b"".join(b"line %d\r\n" % i for i in range(40))), W, "/fg A\r", W]},
    "conn-alert-expiry": {"servers": SRV, "steps": [FLUSH] + AB + ["/connect A\r", W, "/connect B\r", W, ("send", 2, b"bg\r\n"), ("wait", 6)]},
    "conn-quiet-flag": {"servers": SRV, "steps": [FLUSH, "/addworld -T- A joe sekrit localhost @2\r", "/connect -q A\r", W]},
    "conn-ssl-flag-listed": {"servers": SRV, "steps": [FLUSH, "/addworld -x A localhost @2\r", "/listworlds\r"]},
    "conn-quit-while-connected": {"servers": SRV, "steps": [FLUSH] + AB + ["/connect A\r", W, "/echo still here\r"]},
}
SCENARIOS.update(CONN)



class Server:
    """A tiny TCP server for the connection scenarios.  It accepts any number
    of connections, sends `banner` to each, records what the client sends, and
    can be told by a scenario step to send more text or to close."""

    def __init__(self, port, banner=b""):
        self.port, self.banner = port, banner
        self.conns, self.received, self.accepted = [], b"", 0
        self.ls = socket.socket()
        self.ls.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        self.ls.bind(("127.0.0.1", port))
        self.ls.listen(5)
        self.alive = True
        threading.Thread(target=self._accept, daemon=True).start()

    def _accept(self):
        while self.alive:
            try:
                c, _ = self.ls.accept()
            except OSError:
                return
            self.accepted += 1
            self.conns.append(c)
            if self.banner:
                c.sendall(self.banner)
            threading.Thread(target=self._read, args=(c,), daemon=True).start()

    def _read(self, c):
        while True:
            try:
                d = c.recv(4096)
            except OSError:
                return
            if not d:
                return
            self.received += d

    def send(self, data):
        for c in self.conns:
            try:
                c.sendall(data)
            except OSError:
                pass

    def close(self):  # close every client connection (a server-initiated disconnect)
        for c in self.conns:
            try:
                c.shutdown(socket.SHUT_RDWR)
            except OSError:
                pass
            c.close()
        self.conns = []

    def stop(self):
        self.alive = False
        self.close()
        self.ls.close()


PORT0 = 47100  # server n listens on PORT0+n; scenario text writes it as @n (ports 1 and 3 stay closed: refused)


def run(cmd, steps, cols=80, rows=24, wait0=1.5, delay=0.25, env=None, servers=None, snaps=None):
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
        if isinstance(s, tuple):  # ("wait", secs) | ("send", n, bytes) | ("close", n)
            if s[0] == "send":
                servers[s[1]].send(s[2])
            elif s[0] == "close":
                servers[s[1]].close()
            elif s[0] == "snap" and snaps is not None:  # keep the bytes so far (exploration)
                snaps.append(bytes(buf))
            drain(s[1] if s[0] == "wait" else delay)
            continue
        try:
            os.write(fd, subst(s).encode())
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


def subst(text):
    return re.sub(r"@(\d)", lambda m: str(PORT0 + int(m.group(1))), text)


def render(vtdump, data, cols=80, rows=24):
    with tempfile.NamedTemporaryFile(delete=False) as f:
        f.write(data)
    try:
        out = subprocess.run([vtdump, "-w", str(cols), "-h", str(rows), f.name], capture_output=True, text=True, check=True).stdout
    finally:
        os.unlink(f.name)
    return out.splitlines()


# Lines that differ on purpose (GoTinyFugue's own identity); everything else is strict.
IDENTITY = [
    (r"^(TinyFugue|GoTinyFugue) version .*", "<version line>"),
    (r"^Copyright \(C\) .*(Ken Keys|Rick Bird).*", "<copyright line>"),
    (r"^(    2005, 2006-2007 Ken Keys.*)$", "<copyright line>"),
    (r"^Type `/quit' to quit (tf|gtf)\.", "<quit hint>"),
    (r"^\+gmcp .*", "<features>"),
]


def normalize(lines):
    out = []
    for l in lines:
        if l.startswith("cursor:") or l.startswith("server-received:"):
            out.append(l)
            continue
        body = l.split("|", 1)[1] if "|" in l else l
        # the clock differs between the two runs only by timing: mask HH:MM
        import re
        body = re.sub(r"(?<!\d)\d\d:\d\d$", "HH:MM", body.rstrip())
        # /listsockets idle times depend on timing
        body = re.sub(r"^([ *][?CSXOcsxo!][ P] +(?:\d+|fg|\d+\+\d+) +)\d+[smhd]", r"\1<idle>", body)
        for pat, rep in IDENTITY:
            if re.match(pat, body):
                body = rep
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
    env = dict(os.environ)  # honours a local go.work (see README)
    subprocess.check_call(["go", "build", "-o", vtdump, "./cmd/vtdump"], cwd=ROOT, env=env)
    subprocess.check_call(["go", "build", "-o", gtf, "./cmd/gtf"], cwd=ROOT, env=env)
    home = tempfile.mkdtemp()
    import fnmatch
    names = [n for n in SCENARIOS if not args or any(fnmatch.fnmatch(n, a) for a in args)]
    bad = 0
    for name in names:
        steps = SCENARIOS[name]
        srvspec = {}
        if isinstance(steps, dict):  # {"servers": {n: banner bytes}, "steps": [...]}
            srvspec, steps = steps.get("servers", {}), steps["steps"]
        outs = []
        cache = None
        if os.environ.get("TF_CACHE"):  # developer aid: reuse the (deterministic) real-tf screens
            import hashlib, json
            key = hashlib.sha1(repr((srvspec, steps, tf)).encode()).hexdigest()
            cache = os.path.join(os.environ["TF_CACHE"], name + "-" + key[:10] + ".json")
            if os.path.exists(cache):
                outs.append(json.load(open(cache)))
        for cmd in ([tf, "-n", "-f", "-L" + lib], [gtf, "-n", "-f"]):
            if len(outs) == 1 and cmd[0] == tf:
                continue
            servers = {n: Server(PORT0 + n, banner) for n, banner in srvspec.items()}
            try:
                outs.append(render(vtdump, run(cmd, steps, env={"HOME": home}, servers=servers)))
                outs[-1].append("server-received: %r" % [servers[n].received for n in sorted(servers)])
            finally:
                for sv in servers.values():
                    sv.stop()
        a, b = outs
        if cache and not os.path.exists(cache):
            json.dump(a, open(cache, "w"))
        na, nb = normalize(a), normalize(b)
        if name == "startup":
            na, nb = na[-5:], nb[-5:]  # status + input rows + cursor only
        # the leading lines of scrolled output are identical; compare all rows
        if na == nb:
            print("ok   ", name)
            continue
        bad += 1
        print("DIFF ", name)
        if len(na) != len(nb):
            print("   row count: tf %d gtf %d" % (len(na), len(nb)))
        for i, (x, y) in enumerate(zip(na, nb)):
            if x != y:
                print("   row %2d tf : %r\n          gtf: %r" % (i + 1, x, y))
        if verbose:
            print("--- tf\n" + "\n".join(a) + "\n--- gtf\n" + "\n".join(b))
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
