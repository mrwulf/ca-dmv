"""Check that every handbook link's text fragment exists on its page and does not first match the site
header or menu. Run with python3 -I. Usage: python3 -I check_links.py <scratch> <repo>

Needs the pages downloaded into <scratch>/hb/<page>.html (see questions/REGENERATE.md).
"""
import html
import json
import re
import sys
import urllib.parse
from collections import defaultdict

scratch, repo = sys.argv[1], sys.argv[2]
bank = json.load(open(f"{repo}/questions/questions.json"))
pairs = defaultdict(set)
for q in bank:
    if q.get("sourceUrl"):
        base, frag = q["sourceUrl"].split("#:~:text=")
        pairs[base.rstrip("/").split("/")[-1]].add(urllib.parse.unquote(frag))


def text(raw):
    raw = re.sub(r"<(script|style)[^>]*>.*?</\1>", "", raw, flags=re.S)
    return re.sub(r"\s+", " ", html.unescape(re.sub(r"<[^>]+>", " ", raw))).lower()


problems = checked = 0
for page, frags in sorted(pairs.items()):
    raw = open(f"{scratch}/hb/{page}.html", errors="ignore").read()
    cut = raw.find("handbook-page-content")
    head, body = text(raw[:cut]), text(raw[cut:])
    for f in sorted(frags):
        checked += 1
        fl = re.sub(r"\s+", " ", f).lower()
        if fl in head:
            print("IN HEADER/MENU", page, f)
            problems += 1
        elif fl not in body:
            print("MISSING", page, f)
            problems += 1
print(f"{checked} fragments checked, {problems} problems")
sys.exit(1 if problems else 0)
