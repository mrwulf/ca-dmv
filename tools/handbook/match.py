"""Rank handbook sections per question and extract the best supporting sentence. Run with python3 -I."""
import json
import math
import re
import sys
from collections import Counter

scratch, repo = sys.argv[1], sys.argv[2]
secs = json.load(open(f"{scratch}/hb/sections.json"))
bank = json.load(open(f"{repo}/questions/questions.json"))

STOP = set("a an and are as at be by can do does for from has have how if in is it its of on or that the their them then there these they this to up was were what when where which who why will with you your must should may not no yes any all than into out so such per each about over after before while".split())


def stem(w):
    for suf in ("ing", "ed", "es", "s", "ly"):
        if len(w) > len(suf) + 3 and w.endswith(suf):
            return w[: -len(suf)]
    return w


def toks(s):
    return [stem(w) for w in re.findall(r"[a-z0-9$]+", s.lower()) if w not in STOP]


docs = []
for s in secs:
    body = toks(s["text"])
    head = toks(" ".join([s["h2"], s["h3"], s["h4"]]))
    docs.append(Counter(body + head * 3))
N = len(docs)
avg = sum(sum(d.values()) for d in docs) / N
df = Counter(t for d in docs for t in d)
idf = {t: math.log(1 + (N - n + 0.5) / (n + 0.5)) for t, n in df.items()}


def bm25(q, d):
    dl = sum(d.values())
    sc = 0.0
    for t, w in q.items():
        f = d.get(t, 0)
        if f:
            sc += w * idf[t] * f * 2.2 / (f + 1.2 * (0.25 + 0.75 * dl / avg))
    return sc


def sentences(text):
    return [x.strip() for x in re.split(r"(?<=[.!?])\s+", text) if len(x.strip()) > 20]


rows = []
for q in bank:
    correct = q["options"][q["answer"]]
    qt = Counter()
    for t in toks(q["question"]):
        qt[t] += 1.0
    for t in toks(correct):
        qt[t] += 2.0
    for t in toks(q["explanation"]):
        qt[t] += 0.7
    scored = sorted(((bm25(qt, d), i) for i, d in enumerate(docs)), reverse=True)[:3]
    top = scored[0][1]
    ct = set(toks(correct)) | set(toks(q["question"]))
    best, best_ov = "", -1
    for sen in sentences(secs[top]["text"]):
        st = set(toks(sen))
        ov = len(ct & st) / (len(ct) or 1)
        if ov > best_ov:
            best, best_ov = sen, ov
    rows.append({
        "id": q["id"], "q": q["question"], "a": correct, "src": q["source"], "has": bool(q.get("sourceUrl")),
        "cands": [{"i": i, "score": round(sc, 1), "page": secs[i]["page"], "num": secs[i]["num"], "title": secs[i]["title"]} for sc, i in scored],
        "evidence": best, "overlap": round(best_ov, 2),
    })
json.dump(rows, open(f"{scratch}/hb/matches.json", "w"), indent=0)
scores = sorted(r["cands"][0]["score"] for r in rows)
print("questions", len(rows), "score quartiles", scores[len(scores) // 4], scores[len(scores) // 2], scores[3 * len(scores) // 4])
print("overlap>=0.6:", sum(r["overlap"] >= 0.6 for r in rows), " 0.3-0.6:", sum(0.3 <= r["overlap"] < 0.6 for r in rows), " <0.3:", sum(r["overlap"] < 0.3 for r in rows))
