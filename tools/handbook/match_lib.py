import re
STOP = set("a an and are as at be by can do does for from has have how if in is it its of on or that the their them then there these they this to up was were what when where which who why will with you your must should may not no yes any all than into out so such per each about over after before while".split())
def stem(w):
    for suf in ("ing", "ed", "es", "s", "ly"):
        if len(w) > len(suf) + 3 and w.endswith(suf):
            return w[: -len(suf)]
    return w
def toks(s):
    return [stem(w) for w in re.findall(r"[a-z0-9$]+", s.lower()) if w not in STOP]
def sentences(text):
    return [x.strip() for x in re.split(r"(?<=[.!?])\s+", text) if len(x.strip()) > 20]
