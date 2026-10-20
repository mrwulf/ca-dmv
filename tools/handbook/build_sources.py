"""Turn reviewed decisions into questions/sources.json. Run with python3 -I.

Usage: python3 -I build_sources.py <scratch> <repo>

Reads <scratch>/hb/{sections,matches,decisions}.json. A decision is a section index, or "x" for
"no handbook support found". Questions without a decision use the top-ranked candidate.
Output maps question id -> {page, num, heading}; merge-questions.ts turns it into links.
"""
import json
import sys

scratch, repo = sys.argv[1], sys.argv[2]
secs = json.load(open(f"{scratch}/hb/sections.json"))
rows = json.load(open(f"{scratch}/hb/matches.json"))
dec = json.load(open(f"{scratch}/hb/decisions.json"))


def heading(i):
    """Deepest heading of section i, or the nearest titled section before it on the same page."""
    page = secs[i]["page"]
    while i >= 0 and secs[i]["page"] == page:
        if secs[i]["title"]:
            return secs[i]["title"]
        i -= 1
    return ""


# Sections with no heading of their own (a page's intro paragraph): label by hand and anchor the
# link on a distinctive phrase from the section's text instead of a heading.
UNTITLED = {
    "q5210a286": ("introduction-to-driving", 5, "Health and driving", "headset or earplugs in both ears"),
    "qce018942": ("introduction-to-driving", 5, "Health and driving", "headset or earplugs in both ears"),
    "q18c52d1a": ("introduction-to-driving", 5, "Health and driving", "Fatigue and Drowsiness"),
    "q0276e53e": ("introduction-to-driving", 5, "Health and driving", "Medications"),
    "q4bb17159": ("alcohol-and-drugs", 9, "Driving under the influence", "laws apply to both alcohol and drugs"),
    "qae23fc3f": ("alcohol-and-drugs", 9, "Driving under the influence", "laws apply to both alcohol and drugs"),
    "q382714ae": ("alcohol-and-drugs", 9, "Driving under the influence", "cough syrup"),
    "qde7b1331": ("financial-responsibility-insurance-requirements-and-collisions", 10, "Financial responsibility", "proof of financial responsibility"),
}

# Headings that also appear earlier on the page (in a sentence or the site menu), where a text
# fragment on the heading would highlight the wrong spot. Anchor on the section's own first sentence.
FRAGMENTS = {
    ("navigating-the-roads", "U-TURN"): "A U-turn is when you turn your vehicle around",
    ("laws-and-rules-of-the-road", "STOP Sign"): "Make a full stop before entering the crosswalk",
    ("laws-and-rules-of-the-road", "Crosswalks"): "A crosswalk is the part of the road set aside for pedestrians",
    ("laws-and-rules-of-the-road", "Pedestrians"): "When there is a pedestrian crossing a roadway with or without a crosswalk",
    ("laws-and-rules-of-the-road", "Green Arrow"): "A green arrow means GO",
    ("laws-and-rules-of-the-road-cont1", "Move Over and Slow Down"): "Drivers must move over and slow down for emergency",
    ("laws-and-rules-of-the-road-cont1", "Bicycles"): "Bicyclists have the same rights and responsibilities as other drivers",
    ("safe-driving", "Blind Spots"): "These are areas around the vehicle that a driver cannot see",
    ("financial-responsibility-insurance-requirements-and-collisions", "Insurance Requirements"): "Your insurance must cover at least",
}

out, none = {}, 0
for n, r in enumerate(rows):
    v = dec.get(str(n), str(r["cands"][0]["i"]))
    if v == "x":
        none += 1
        continue
    s = secs[int(v)]
    h = heading(int(v))
    if not h:
        if r["id"] not in UNTITLED:
            none += 1
            continue
        page, num, h, fragment = UNTITLED[r["id"]]
        out[r["id"]] = {"page": page, "num": num, "heading": h, "fragment": fragment}
        continue
    entry = {"page": s["page"], "num": s["num"], "heading": h}
    if (s["page"], h) in FRAGMENTS:
        entry["fragment"] = FRAGMENTS[(s["page"], h)]
    out[r["id"]] = entry
json.dump(out, open(f"{repo}/questions/sources.json", "w"), indent=1, sort_keys=True)
print(f"{len(out)} sourced, {none} without a handbook source, of {len(rows)}")
