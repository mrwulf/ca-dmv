// Merge the question sources into questions/questions.json.
// Usage: bun tools/merge-questions.ts <scratch> <out>   (see questions/REGENERATE.md for the scratch layout)
import vm from "node:vm";
import { readFileSync, writeFileSync } from "node:fs";
import { createHash } from "node:crypto";

const [scratch, out] = process.argv.slice(2);

type Q = {
  id: string; topic: string; question: string; options: string[]; answer: number;
  explanation: string; source: string; sourceUrl?: string; sourceLabel?: string; image?: string; imageAlt?: string;
};

const TOPIC_MAP: Record<string, string> = {
  signs: "signs", speed: "speed", row: "right-of-way", "right-of-way": "right-of-way",
  lanes: "lanes", parking: "parking", share: "sharing-road", "sharing-road": "sharing-road",
  safe: "safe-driving", dui: "alcohol", alcohol: "alcohol", license: "licenses",
  "adult-specific": "licenses", fines: "licenses", freeway: "freeway", rail: "rail-bus",
  emerg: "emergencies", emergencies: "emergencies",
};

const all: Q[] = [];

// road-ready-ca (MIT): JS data table, a[0] is the correct answer.
const rrCtx: any = vm.createContext({});
vm.runInContext(readFileSync(`${scratch}/rr/questions.js`, "utf8").replace(/^"use strict";/, "") + "\n;this.Q=Q;", rrCtx);
for (const q of rrCtx.Q as any[]) {
  if (q.who === "minor") continue;
  if (q.s && /\b(this|shown|image|picture|above|these)\b/i.test(q.q)) continue; // visual not portable
  all.push({
    id: "", topic: TOPIC_MAP[q.t], question: q.q, options: q.a, answer: 0,
    explanation: q.e, source: "road-ready-ca",
  });
}

// jedi116/dmv-knowledge-test-ca (MIT)
for (const q of JSON.parse(readFileSync(`${scratch}/jedi/questions.json`, "utf8"))) {
  all.push({
    id: "", topic: TOPIC_MAP[q.topic], question: q.question, options: q.options, answer: q.correctIndex,
    explanation: q.explanation, source: "dmv-knowledge-test-ca",
    ...(q.image ? { image: q.image.src.replace("/signs/", "signs/"), imageAlt: q.image.alt } : {}),
  });
}

// DMV published sample tests. Answers and explanations were written from the Driver Handbook; the source
// link for every question comes from questions/sources.json (see tools/handbook/).
const D = (topic: string, question: string, options: string[], answer: number, explanation: string, n: number) =>
  all.push({ id: "", topic, question, options, answer, explanation, source: `dmv-sample-${n}` });

D("lanes", "When is it legal to drive off the road to pass another vehicle?", ["If the vehicle ahead is turning left.", "It is not legal under any conditions.", "If there are two or more one-way lanes."], 1, "Never drive off the paved roadway to pass. Pass only on the road, and only when it is safe and legal.", 1);
D("rail-bus", "When a railroad crossing is not controlled, what is the speed limit when you are within 100 feet and cannot see for 400 feet in both directions?", ["15 mph.", "10 mph.", "25 mph."], 0, "At an uncontrolled crossing, if you are within 100 feet and cannot see 400 feet down the tracks both ways, the limit is 15 mph.", 1);
D("parking", "Which of the following is the proper procedure for parallel parking?", ["Drive forward into the space without stopping.", "Stop next to the vehicle behind the open space, and then drive forward into the space.", "Stop next to the vehicle in front of the open space, and then back into the space."], 2, "Pull up alongside the vehicle in front of the open space, then back into the space.", 1);
D("freeway", "What speed should you be driving when entering onto a highway?", ["At or near the speed of traffic.", "Faster than the speed of traffic.", "Slower than the speed of traffic."], 0, "Use the on-ramp to match the speed of traffic so you can merge smoothly.", 1);
D("safe-driving", "How many feet away should you switch from high beam to low beam headlights when approaching a vehicle coming towards you?", ["900 feet.", "700 feet.", "500 feet."], 2, "Dim your high beams within 500 feet of an oncoming vehicle and within 300 feet when following another vehicle.", 1);
D("parking", "Who can legally park next to curb painted blue?", ["Someone who is either picking up or dropping off passengers at this location.", "A person who is disabled and has a special placard or vehicle license plate for disabled persons.", "A person who will only be parked at the curb for less than 15 minutes."], 1, "Blue curbs are reserved for people with a disabled person placard or license plate.", 1);
D("rail-bus", "What should you do when there is a school bus ahead that starts flashing yellow warning lights?", ["Slow down and prepare to stop.", "Stop immediately and remain stopped.", "Cautiously pass the school bus on the left."], 0, "Flashing yellow lights warn that the bus is about to stop. Slow down and get ready to stop. Flashing red means stop.", 1);
D("speed", "Which of the following is a requirement of California's Basic Speed Law?", ["Always drive the speed limit, regardless of conditions.", "Match your speed to that of your surrounding traffic.", "Never drive faster than is safe for current conditions."], 2, "The Basic Speed Law says never drive faster than is safe for current conditions, even if you are under the posted limit.", 1);
D("licenses", "You must notify the DMV within 5 days if you:", ["Paint your vehicle a different color.", "Sell or transfer ownership of your vehicle.", "Receive a traffic violation."], 1, "When you sell or transfer a vehicle, notify the DMV within 5 days.", 1);
D("safe-driving", "Which of the following is an example of a safe driving practice?", ["Staring only at the middle of the road.", "Always keep your eyes moving to scan the surroundings.", "Using your high-beam headlights in the fog."], 1, "Scan constantly, looking well ahead and to the sides. High beams reflect off fog and reduce what you can see.", 1);

D("right-of-way", "What should you do when reaching an intersection where a person operating a motorized wheelchair has entered the crosswalk?", ["Remain stopped behind the crosswalk line until the motorized wheelchair has safely finished crossing.", "Remain stopped behind the nearest crosswalk line until the motorized wheelchair is beyond the range of your vehicle.", "Assume right-of-way if the motorized wheelchair stops in the crosswalk."], 0, "Pedestrians, including people in motorized wheelchairs, have the right-of-way. Stay stopped until they have finished crossing.", 2);
D("sharing-road", "Which of the following is true about large trucks?", ["They are made of many trailers, which make them more maneuverable than passenger vehicles.", "They have large blind spots, which makes it difficult for the truck driver to see other vehicles.", "They have large and powerful emergency brakes, which gives them the capability to stop quickly."], 1, "Large trucks have big blind spots on all sides (the No-Zones) and need much more room to stop. If you can't see the driver's mirrors, they can't see you.", 2);
D("safe-driving", "What is one of the most common causes of traffic collisions?", ["Paying attention to your surroundings.", "Better traffic flow.", "Driver distractions."], 2, "Distracted driving is a common cause of collisions. Avoid distractions while you drive.", 2);
D("licenses", "Any driver who willfully flees or attempts to evade law enforcement, during which a person is seriously injured, is subject to:", ["Imprisonment in a state prison for up to seven years.", "A fine of less than $1,000.", "Attending an anger-management class."], 0, "Fleeing from police is a serious crime. If someone is seriously hurt, the penalty can be up to seven years in state prison.", 2);
D("right-of-way", "What should a driver do who is stopped at an intersection and wants to make a left turn?", ["Turn immediately if there are no pedestrians.", "Take the right-of-way turn before oncoming traffic.", "Give the right-of-way to any approaching vehicle that is close enough to be dangerous."], 2, "When turning left, yield to oncoming vehicles that are close enough to be a hazard, and to pedestrians.", 2);
D("parking", "In addition to setting your parking brake, what should you do when parking on a hill?", ["Make sure the front vehicle wheels are parallel to the road.", "Make sure your vehicle is left in the \"neutral\" position.", "Make sure your vehicle is in the \"park\" position or in gear."], 2, "Set the parking brake and leave the car in park (automatic) or in gear (manual), and turn the wheels toward or away from the curb as the slope requires.", 2);
D("lanes", "Where should you begin a left turn from a one-way street onto a one-way street?", ["The lane closest to the center of the street.", "The far-left lane.", "You can turn from any lane."], 1, "Start the turn from the far-left lane. If the street you enter has three or more lanes in your direction, you may end the turn in any open lane.", 2);
D("emergencies", "You are required to notify DMV by filing a Report of Traffic Accident Occuring in California (SR-1) form if:", ["You allowed a licensed driver from another state to drive your vehicle.", "You were involved in a collision with more than $1,000 in damages.", "You failed to pay your registration fees."], 1, "File an SR-1 with the DMV within 10 days if a collision caused more than $1,000 in damage, or any injury or death.", 2);
D("safe-driving", "At what point during a rainfall are roads slippery on a hot day?", ["Immediately after it has stopped raining.", "For the first several minutes.", "When it has been raining for a few hours."], 1, "Slow down at the first sign of rain. After a long dry, hot spell, built-up oil and dust make the road most slippery in the first minutes.", 2);

D("parking", "What color curb does not allow vehicles to stop or park?", ["Yellow.", "Red.", "White."], 1, "A red curb means no stopping, standing, or parking. Buses may stop where signs allow it.", 3);
D("emergencies", "You are required to notify DMV by filing a Report of Traffic Accident Occuring in California (SR-1) form if:", ["You change your insurance company.", "You were involved in a collision with an injury.", "Your vehicle fails a smog test."], 1, "File an SR-1 with the DMV within 10 days of a collision that caused injury or death, or more than $1,000 in property damage.", 3);
D("signs", "What is indicated by two sets of double yellow lines spaced 2 feet or more apart?", ["Lanes of traffic moving in the same direction.", "Barrier.", "Carpool/High-Occupancy (HOV) lane."], 1, "Two sets of double yellow lines two feet or more apart are a barrier. Do not drive on or over it.", 3);
D("lanes", "What should you do to make a right turn at an upcoming intersection?", ["Merge into the bicycle lane before making the turn.", "Wait until the bicycle lane ends, then make the turn.", "Make the turn from your current lane and do not enter the bicycle lane."], 0, "Merge into the bicycle lane, no more than 200 feet before the turn, once it is safe. Don't cut across a cyclist.", 3);
D("speed", "A highway has a posted speed limit of 65 mph. What does this mean?", ["You must always drive 65 mph on that highway.", "You may drive faster only if there are no other vehicles.", "You must drive 65 mph only if driving conditions are ideal."], 2, "The posted limit is the maximum under ideal conditions. Weather, traffic, and visibility can make a lower speed the legal one.", 3);
D("parking", "When is parking in a crosshatched (diagonal lines) area allowed?", ["It is never allowed to park in a crosshatched (diagonal lines) area.", "If the area is at least twenty feet away from a railroad track.", "If the area is labeled as a bicycle lane, unless otherwise posted."], 0, "Crosshatched areas next to a disabled parking space are access aisles. Never park in them.", 3);
D("safe-driving", "What should you do if your cell phone rings and you do not have a hands-free device?", ["Answer the call and keep the conversation short.", "Do not answer the phone and let it go to voice mail.", "Answer the call if you are stopped at a red light."], 1, "Holding a phone while driving is illegal in California, even at a red light. Let it go to voicemail.", 3);
D("right-of-way", "If a traffic signal is green, but traffic is blocking the intersection, what should you do?", ["Partially enter the intersection, as allowed by traffic.", "Wait and do not enter the intersection until your vehicle can get completely across.", "Drive around the traffic on the shoulder to help ease the congestion."], 1, "Never enter an intersection unless you can get all the way across. Blocking it can earn you a ticket.", 3);
D("lanes", "In which lane should you end your turn when making a right turn?", ["Any lane free of traffic.", "The lane closest to the left edge of the road.", "The lane closest to the right edge of the road."], 2, "A right turn should end in the right-most lane of the street you enter.", 3);
D("signs", "When is it required for you to obey directions from a crossing guard?", ["At all times.", "During school hours only.", "Only when children are present in front of a school."], 0, "Obey a crossing guard at all times. They have the authority to direct traffic.", 3);

D("safe-driving", "Which of the following is illegal while driving?", ["Wearing a headset or ear plugs that covers both ears.", "Wearing a headset or ear plugs that covers one ear.", "Using cruise control on residential streets."], 0, "You may not wear a headset or earplugs in both ears while driving. One ear is legal.", 4);
D("rail-bus", "What should you do when traffic is slow and heavy, and you must cross railroad tracks before reaching the upcoming intersection?", ["Begin crossing when the vehicle in front of you is crossing the tracks.", "Wait on the tracks until the stoplight at the intersection turns green.", "Wait until you can completely cross the tracks before proceeding."], 2, "Never stop on the tracks. Cross only when there is room on the far side for your whole vehicle.", 4);
D("safe-driving", "Which of the following should you do if you are being followed by a tailgater?", ["Change lanes and allow the tailgater to pass.", "Tap the brakes to signal you are moving at a slower pace.", "Increase your speed to match the speed of the vehicle."], 0, "Keep your speed and, when it is safe, move to another lane so the tailgater can pass. Don't brake-check them or speed up.", 4);
D("speed", "When you are driving the speed limit, you can be given a speeding ticket:", ["If road or weather conditions require a slower speed.", "Under no circumstances because it is always legal.", "Only if you are approaching a sharp curve in the road."], 0, "You can be cited under the Basic Speed Law if the limit is unsafe for conditions like rain, fog, or heavy traffic.", 4);
D("safe-driving", "Which lights should be used if a driver is having trouble seeing other vehicles because of dust or smoke blowing across the roadway?", ["Parking lights.", "Emergency lights.", "Headlights."], 2, "Turn on your low-beam headlights so others can see you. The handbook names rain, fog, and heavy smoke. High beams glare back in fog and smoke.", 4);
D("signs", "What does a flashing yellow traffic signal light direct a driver to do?", ["Stop and proceed when the traffic allows you to proceed safely.", "Slow down and proceed with caution.", "Stop and wait for a flashing green signal."], 1, "A flashing yellow light means slow down and proceed with caution. Flashing red means treat it as a stop sign.", 4);
D("right-of-way", "What should you do if a pedestrian is still in the crosswalk after the \"DON'T WALK\" sign begins to flash and your light has turned green?", ["Wait until the pedestrian is out of your path before proceeding.", "Wait until the pedestrian signals that it is okay for you to proceed.", "Wait until the pedestrian has crossed the street completely before proceeding."], 2, "Pedestrians already in the crosswalk have the right-of-way. Use caution, slow down or stop, and let them finish crossing.", 4);
D("alcohol", "When is it legal for a person to drive with an alcoholic beverage container that has been opened?", ["If the container is under the front seat.", "If the container is in the trunk of the vehicle.", "If the container is in the glove compartment."], 1, "Keep an open container in the trunk or another place where passengers do not sit. It may not be in the glove box.", 4);

// Questions the current Driver Handbook contradicts or does not support. Dropped, not rewritten.
const DROP = new Set([
  // The handbook gives the three-second rule and no four-second rule.
  "You should increase your following distance to at least 4 seconds when:",
]);

// Answers that were out of date or wrong against the current handbook, fixed in place (checked 2026-10).
const CORRECTIONS: { question: string; options: string[]; answer: string; explanation: string }[] = [
  {
    question: "California's minimum liability insurance requirement for bodily injury per person is:",
    options: ["$5,000", "$15,000", "$30,000", "$50,000"], answer: "$30,000",
    explanation: "Your insurance must cover at least $30,000 for a single death or injury, $60,000 for death or injury to more than one person, and $15,000 for property damage.",
  },
  {
    question: "California's minimum auto liability insurance for property damage is:",
    options: ["$1,000", "$5,000", "$15,000", "$25,000"], answer: "$15,000",
    explanation: "Your insurance must cover at least $30,000 for a single death or injury, $60,000 for death or injury to more than one person, and $15,000 for property damage.",
  },
  {
    question: "When following a motorcycle, you should:",
    options: ["Follow more closely than a car so they can see you", "Follow at least 3 seconds behind", "Follow no more than 1 second behind", "Move to the next lane immediately"],
    answer: "Follow at least 3 seconds behind",
    explanation: "Motorcycles are smaller and harder to see, and a rider can brake suddenly or fall. Allow a safe three-second following distance.",
  },
  {
    question: "When you make a U-turn in a residential district, U-turns are legal:",
    options: ["Only at intersections or where allowed by signs", "On every block", "Only if no vehicle is approaching within 200 feet", "Anywhere clear of traffic"],
    answer: "Only if no vehicle is approaching within 200 feet",
    explanation: "In a residential district you may make a U-turn if no vehicles are approaching you within 200 feet. Never make a U-turn where you cannot see clearly for 200 feet in each direction.",
  },
  {
    question: "U-turns are NEVER legal:",
    options: ["At an intersection on a green light, unless a sign prohibits it", "Where a NO U-TURN sign is posted, at a railroad crossing, in front of a fire station, or where you cannot see 200 feet each way", "When making a delivery", "On any rural road"],
    answer: "Where a NO U-TURN sign is posted, at a railroad crossing, in front of a fire station, or where you cannot see 200 feet each way",
    explanation: "Never make a U-turn where a NO U-TURN sign is posted, at or on a railroad crossing, on a one-way street, in front of a fire station, or when you cannot see clearly for 200 feet in each direction. You may U-turn across a double yellow line.",
  },
];
// Law that changed on 2026-01-01 and that the Driver Handbook does not reflect yet. Sourced to the CHP.
const CHP_2026 = "https://www.chp.ca.gov/news-alerts/news-list/new-year-new-laws--chp-highlights-public-safety-laws-taking-effect-in-2026/";
const EXTERNAL_SOURCES: { question: string; url: string; label: string }[] = [
  {
    question: "As of 2026, California's 'Slow Down, Move Over' law also covers:",
    url: CHP_2026, label: "CHP: new laws for 2026 (AB 390)",
  },
];

// Explanations rewritten to match current law and its sources.
const EXPLANATION_FIXES: { question: string; explanation: string }[] = [
  {
    question: "As of 2026, California's 'Slow Down, Move Over' law also covers:",
    explanation: "AB 390, effective January 1, 2026, widens the Move Over law: move out of the lane next to any highway maintenance vehicle or stationary vehicle using flashing hazard lights or warning devices such as cones or flares, or slow to a safe speed if you cannot change lanes.",
  },
  {
    question: "Near a school while children are outside or crossing, unless a lower limit is posted, the speed limit is:",
    explanation: "The speed limit is 25 mph within 500 feet of a school while children are outside or crossing the street. Some school zones are posted lower, as low as 15 mph. Since January 1, 2026 (AB 382), cities and counties may post 20 mph, and 20 mph becomes the limit in signed school zones statewide from 2031. Always obey the posted sign.",
  },
];
for (const f of EXPLANATION_FIXES) {
  const q = all.find((x) => x.question === f.question);
  if (!q) throw new Error("explanation fix target not found: " + f.question);
  q.explanation = f.explanation;
}

for (const c of CORRECTIONS) {
  const q = all.find((x) => x.question === c.question);
  if (!q) throw new Error("correction target not found: " + c.question);
  q.options = c.options;
  q.answer = c.options.indexOf(c.answer);
  if (q.answer < 0) throw new Error("correction answer not in options: " + c.question);
  q.explanation = c.explanation;
}

// De-duplicate: drop a question if its normalized text matches an earlier one, or its first
// 60 normalized characters plus the correct answer match. Prefer entries with images, then ones with a source link.
const norm = (s: string) => s.toLowerCase().replace(/[^a-z0-9 ]/g, "").replace(/\s+/g, " ").trim();
const rank = (q: Q) => (q.image ? 2 : 0) + (q.sourceUrl ? 1 : 0);
all.sort((a, b) => rank(b) - rank(a));
// Options are shuffled per exam, so drop questions whose options refer to each other by position.
const positional = (q: Q) => q.options.some((o) => /\b(both|all|none) of the above\b|\b[abc] and [abc]\b/i.test(o));
const seen = new Set<string>();
const kept: Q[] = [];
for (const q of all) {
  if (positional(q) || DROP.has(q.question)) continue;
  const k1 = norm(q.question);
  const k2 = norm(q.question).slice(0, 60) + "|" + norm(q.options[q.answer]);
  if (seen.has(k1) || seen.has(k2)) continue;
  seen.add(k1); seen.add(k2);
  kept.push(q);
}

// Validate and assign stable ids (hash of the normalized question text, so they survive regeneration).
const stats: Record<string, number> = {};
const ids = new Set<string>();
kept.forEach((q, i) => {
  q.id = "q" + createHash("sha1").update(norm(q.question)).digest("hex").slice(0, 8);
  if (ids.has(q.id)) throw new Error("id collision: " + q.id);
  ids.add(q.id);
  if (!q.topic) throw new Error("no topic: " + q.question);
  if (q.options.length < 2 || q.answer < 0 || q.answer >= q.options.length) throw new Error("bad answer: " + q.question);
  if (!q.explanation) throw new Error("no explanation: " + q.question);
  stats[q.topic] = (stats[q.topic] ?? 0) + 1;
});

// Source links: the handbook page plus a text fragment, so a supporting browser scrolls to and
// highlights the heading (others just open the page). Entries come from tools/handbook/build_sources.py.
const HB = "https://www.dmv.ca.gov/portal/handbook/california-driver-handbook/";
const frag = (t: string) => encodeURIComponent(t).replace(/-/g, "%2D");
const sources: Record<string, { page: string; num: number; heading: string; fragment?: string }> =
  JSON.parse(readFileSync(out.replace(/[^/]*$/, "sources.json"), "utf8"));
let linked = 0;
for (const q of kept) {
  const e = sources[q.id];
  if (!e) continue;
  q.sourceUrl = `${HB}${e.page}/#:~:text=${frag(e.fragment ?? e.heading)}`;
  q.sourceLabel = `Driver Handbook §${e.num}, ${e.heading}`;
  linked++;
}
for (const x of EXTERNAL_SOURCES) {
  const q = kept.find((k) => k.question === x.question);
  if (!q) throw new Error("external source target not found: " + x.question);
  if (!q.sourceUrl) linked++;
  q.sourceUrl = x.url;
  q.sourceLabel = x.label;
}
const stale = Object.keys(sources).filter((id) => !kept.some((q) => q.id === id));
if (stale.length) console.warn(`sources.json has ${stale.length} ids not in the bank:`, stale.slice(0, 5));
console.log(`linked ${linked} of ${kept.length} questions to the handbook`);
writeFileSync(out, JSON.stringify(kept, null, 1) + "\n");
console.log(`in=${all.length} kept=${kept.length}`, stats);
