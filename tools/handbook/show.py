"""Print a batch of match candidates for review. Usage: python3 -I show.py <scratch> <repo> <start> <count>"""
import json
import re
import sys

scratch, repo, start, count = sys.argv[1], sys.argv[2], int(sys.argv[3]), int(sys.argv[4])
exec(open(f"{scratch}/build/match_lib.py").read())
secs = json.load(open(f"{scratch}/hb/sections.json"))
rows = json.load(open(f"{scratch}/hb/matches.json"))
bank = json.load(open(f"{repo}/questions/questions.json"))
for n in range(start, min(start + count, len(rows))):
    r, q = rows[n], bank[n]
    print(f"#{n} {q['source'][:6]}{'*' if q.get('sourceUrl') else ''} Q: {q['question'][:110]}")
    print(f"    A: {r['a'][:90]}")
    ct = set(toks(r["a"])) | set(toks(q["question"]))
    for k, c in enumerate(r["cands"], 1):
        best, bo = "", -1
        for sen in sentences(secs[c["i"]]["text"]):
            ov = len(ct & set(toks(sen))) / (len(ct) or 1)
            if ov > bo:
                best, bo = sen, ov
        print(f"  {k} s{c['i']} §{c['num']} {c['title'][:38]} [{c['score']:.0f}] :: {best[:150]}")
