#!/usr/bin/env python3
"""Differential test: run TF case files through real tf and through gtf, diff the echoed output.

usage: difftest.py [--tf PATH] [--lib DIR] [--gtf PATH] case.tf ...
A case file is a TF script; its /echo output is compared. Real tf is driven in
a pty with /echo redirected into a file (a macro named echo shadows the builtin).
"""
import argparse, os, pty, select, subprocess, sys, tempfile, time

def run_tf(tf, lib, script, timeout=20):
    d = tempfile.mkdtemp()
    out, src = os.path.join(d, 'out'), os.path.join(d, 'case.tf')
    with open(src, 'w') as f:
        f.write('/test _o := tfopen("%s", "w")\n' % out)
        f.write('/def -i echo = /test tfwrite(_o, {*})\n')
        f.write(script)
        f.write('\n/test tfclose(_o)\n/quit\n')
    m, s = pty.openpty()
    p = subprocess.Popen([tf, '-n', '-f' + src, '-L' + lib], stdin=s, stdout=s, stderr=s,
                         env={'TERM': 'dumb', 'PATH': os.environ['PATH'], 'HOME': d}, close_fds=True)
    t = time.time()
    while p.poll() is None and time.time() - t < timeout:
        r, _, _ = select.select([m], [], [], 0.2)
        if r:
            try: os.read(m, 65536)
            except OSError: break
    if p.poll() is None: p.kill()
    return open(out).read() if os.path.exists(out) else '<no output>'

def run_gtf(gtf, lib, script):
    d = tempfile.mkdtemp(); src = os.path.join(d, 'case.tf')
    open(src, 'w').write(script + '\n')
    r = subprocess.run([gtf, '-f', src, '-L', lib], capture_output=True, text=True, timeout=20)
    return r.stdout

ap = argparse.ArgumentParser()
ap.add_argument('--tf', default=os.environ.get('TF_REF'))
ap.add_argument('--lib', default=os.environ.get('TF_LIB'))
ap.add_argument('--gtf', default='/tmp/gtf')
ap.add_argument('cases', nargs='+')
a = ap.parse_args()
bad = 0
for c in a.cases:
    script = open(c).read()
    want, got = run_tf(a.tf, a.lib, script), run_gtf(a.gtf, a.lib, script)
    if want == got:
        print('ok  ', c)
    else:
        bad += 1
        print('DIFF', c)
        import difflib
        for l in difflib.unified_diff(want.splitlines(), got.splitlines(), 'tf', 'gtf', lineterm='', n=0): print('   ', l)
sys.exit(1 if bad else 0)
