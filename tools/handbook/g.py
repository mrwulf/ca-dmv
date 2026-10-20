"""Keyword search across handbook sentences: python3 -I g.py <scratch> <regex> [max]"""
import json, re, sys
scratch, pat = sys.argv[1], sys.argv[2]
mx = int(sys.argv[3]) if len(sys.argv) > 3 else 8
secs = json.load(open(f"{scratch}/hb/sections.json"))
n = 0
for i, s in enumerate(secs):
    for sen in re.split(r"(?<=[.!?])\s+", s["text"]):
        if re.search(pat, sen, re.I):
            print(f"s{i} §{s['num']} {s['title'][:40]} :: {sen[:170]}")
            n += 1
            if n >= mx:
                sys.exit()
