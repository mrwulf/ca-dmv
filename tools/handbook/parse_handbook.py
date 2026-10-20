"""Index the downloaded handbook pages into sections: heading path plus text. Run with python3 -I."""
import json
import re
import sys
from html.parser import HTMLParser

scratch = sys.argv[1]

PAGES = [
    ("the-california-driver-license", 1), ("getting-an-instruction-permit-and-drivers-license", 2),
    ("the-testing-process", 3), ("changing-replacing-and-renewing-your-drivers-license", 4),
    ("introduction-to-driving", 5), ("navigating-the-roads", 6), ("navigating-the-roads-cont1", 6),
    ("laws-and-rules-of-the-road", 7), ("laws-and-rules-of-the-road-cont1", 7), ("laws-and-rules-of-the-road-cont2", 7),
    ("safe-driving", 8), ("safe-driving-cont1", 8), ("safe-driving-cont2", 8), ("alcohol-and-drugs", 9),
    ("financial-responsibility-insurance-requirements-and-collisions", 10), ("vehicle-registration-requirements", 11),
    ("driver-safety", 12), ("seniors-and-driving", 13), ("glossary", 14),
]


class Extract(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.depth = 0  # >0 while inside the content div
        self.in_content = False
        self.heading = None  # tag name while inside a heading
        self.buf = []
        self.path = {}  # level -> text
        self.sections = []
        self.cur = None

    def start_section(self):
        self.cur = {"h2": self.path.get("h2", ""), "h3": self.path.get("h3", ""), "h4": self.path.get("h4", ""), "text": []}
        self.sections.append(self.cur)

    def handle_starttag(self, tag, attrs):
        a = dict(attrs)
        if tag == "div":
            if not self.in_content and "handbook-page-content" in (a.get("class") or ""):
                self.in_content, self.depth = True, 1
                self.start_section()
                return
            if self.in_content:
                self.depth += 1
        if not self.in_content:
            return
        if tag in ("h2", "h3", "h4"):
            self.heading, self.buf = tag, []
        elif tag == "img" and a.get("alt") and self.cur is not None:
            self.cur["text"].append(a["alt"])

    def handle_endtag(self, tag):
        if not self.in_content:
            return
        if tag == "div":
            self.depth -= 1
            if self.depth == 0:
                self.in_content = False
        if tag == self.heading:
            text = re.sub(r"\s+", " ", "".join(self.buf)).strip()
            lvl = {"h2": 2, "h3": 3, "h4": 4}[tag]
            for k in ("h2", "h3", "h4"):
                if int(k[1]) > lvl:
                    self.path.pop(k, None)
            self.path[tag] = text
            self.heading = None
            self.start_section()
        elif tag in ("p", "li", "td", "th", "figcaption") and self.cur is not None and self.buf:
            self.cur["text"].append(re.sub(r"\s+", " ", "".join(self.buf)).strip())
            self.buf = []

    def handle_data(self, data):
        if self.in_content:
            self.buf.append(data)


out = []
for slug, num in PAGES:
    p = Extract()
    p.feed(open(f"{scratch}/hb/{slug}.html", errors="ignore").read())
    for s in p.sections:
        text = " ".join(t for t in s["text"] if t)
        title = s["h4"] or s["h3"] or s["h2"]
        if not text and not title:
            continue
        out.append({"page": slug, "num": num, "h2": s["h2"], "h3": s["h3"], "h4": s["h4"], "title": title, "text": text})
json.dump(out, open(f"{scratch}/hb/sections.json", "w"), indent=0)
per = {}
for s in out:
    per[s["page"]] = per.get(s["page"], 0) + 1
print(len(out), "sections;", sum(len(s["text"]) for s in out) // 1000, "KB text")
for k, v in per.items():
    print(f"  {v:4d} {k}")
