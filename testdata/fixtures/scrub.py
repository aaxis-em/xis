#!/usr/bin/env python3
"""Scrub a Claude Code transcript or hook payload file for use as a fixture.

Usage: scrub.py IN OUT --replace OLD=NEW [--replace OLD=NEW ...]
"""
import argparse, json, re

DROP_TYPES = {"api-request", "api-request-blob", "api-request-shape", "atis-latch"}
DROP_KEYS = {"rendered", "serverClassifierContext", "serverClassifierRequest", "signature", "requestRef", "wireIngestContext"}

def scrub(v, repl):
    if isinstance(v, dict):
        return {k: scrub(x, repl) for k, x in v.items() if k not in DROP_KEYS}
    if isinstance(v, list):
        return [scrub(x, repl) for x in v]
    if isinstance(v, str):
        for old, new in repl:
            v = v.replace(old, new)
        return re.sub(r"[\w.+-]+@[\w-]+\.[\w.]+", "user@example.com", v)
    return v

ap = argparse.ArgumentParser()
ap.add_argument("inp"); ap.add_argument("out")
ap.add_argument("--replace", action="append", default=[])
a = ap.parse_args()
repl = [tuple(r.split("=", 1)) for r in a.replace]
with open(a.inp) as f, open(a.out, "w") as o:
    for line in f:
        line = line.strip()
        if not line:
            continue
        d = json.loads(line)
        if isinstance(d, dict) and d.get("type") in DROP_TYPES:
            continue
        o.write(json.dumps(scrub(d, repl), separators=(",", ":")) + "\n")
