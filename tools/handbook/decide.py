"""Record decisions: python3 -I decide.py <scratch> "n:sidx n:x ..."  (rows without an entry default to candidate 1)"""
import json, os, sys
scratch, spec = sys.argv[1], sys.argv[2]
p = f"{scratch}/hb/decisions.json"
d = json.load(open(p)) if os.path.exists(p) else {}
for tok in spec.split():
    n, v = tok.split(":")
    d[n] = v
json.dump(d, open(p, "w"))
print(len(d), "overrides recorded")
