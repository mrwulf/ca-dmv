# Regenerating the question bank

`questions.json` is built, not hand-edited. It is embedded into the binary, so changing it means rebuilding the image.
The app does not need to stay current; refresh only when the DMV handbook changes or the bank needs more questions.

## Sources and licenses

| Source | License | What we take |
|---|---|---|
| [Steven-Tibbs/road-ready-ca](https://github.com/Steven-Tibbs/road-ready-ca) `js/questions.js` | MIT | Adult questions. In that file `a[0]` is the correct answer. `who:"minor"` rows and rows whose `s:` visual the text depends on are dropped |
| [jedi116/dmv-knowledge-test-ca](https://github.com/jedi116/dmv-knowledge-test-ca) `src/data/questions.json`, `public/signs/*.svg` | MIT | Questions plus the sign and road-marking SVGs, copied to `web/signs/` |
| CA DMV [sample tests 1-4](https://www.dmv.ca.gov/portal/driver-education-and-safety/educational-materials/sample-driver-license-dl-knowledge-tests/) | © State of California, published for public study | Question and option text only. The pages carry no answer key, so the answers and explanations in `tools/merge-questions.ts` were written from the sources in its `SRC` table (Driver Handbook headings and Vehicle Code sections), each checked against the page text on 2026-10. Each question links to its source in the app. The under-18-only sample question is dropped (adult rules only). Re-check these when the handbook is revised |

Not used: GitHub repos with no license (all rights reserved), driving-tests.org, Quizlet.
Check a new repo's `license` field with `gh api repos/OWNER/REPO --jq .license.spdx_id` before importing; `null` means do not copy.

## Handbook sources

Each question links to the Driver Handbook section that supports its answer (shown in the "Think this is wrong?" expander and under the explanation). `questions/sources.json` maps question id to `{page, num, heading[, fragment]}`; `merge-questions.ts` turns it into a link with a text fragment (`#:~:text=`) that highlights the heading in browsers that support it. About 89% of questions have a source. The rest have no supporting text in the handbook (for example sign colors and shapes, which the handbook shows only as pictures) and the app says so.

To redo the mapping (about 45 minutes of review for ~450 questions):

1. Download the handbook pages into `<scratch>/hb/<slug>.html`, one per section page. The slugs are the keys of `PAGES` in `tools/handbook/parse_handbook.py` and the URLs are `https://www.dmv.ca.gov/portal/handbook/california-driver-handbook/<slug>/`.
2. `python3 -I tools/handbook/parse_handbook.py <scratch>` indexes them into headed sections.
3. `python3 -I tools/handbook/match.py <scratch> <repo>` ranks the three best sections per question.
4. Review in batches with `python3 -I tools/handbook/show.py <scratch> <repo> <start> <count>`. It prints each question with its candidates and the best supporting sentence. Accept the top candidate by default; record overrides with `decide.py <scratch> "row:section row:x ..."` (`x` means no handbook support). Use `g.py <scratch> <regex>` to search the handbook text for a claim.
5. `python3 -I tools/handbook/build_sources.py <scratch> <repo>` writes `questions/sources.json`, then re-run the merge and `python3 -I tools/handbook/check_links.py <scratch> <repo>`.

Only count a section as the source if its text states the answer. A section that merely covers the topic is not enough, and the Caltrans sign charts page is not a source for sign colors (it does not state them).

Law that changed after the handbook was last revised (for example AB 390 Move Over and AB 382 school zones, both effective 2026-01-01) is kept, with the CHP's announcement as the source, in `EXTERNAL_SOURCES` in `tools/merge-questions.ts`. Do not drop a question only because the handbook has not caught up; confirm the change with an official source (CHP, DMV, or the legislature) first, and re-check whether the handbook has been updated at each refresh. The DMV knowledge test is based on the handbook, so such a question may not appear on the real test yet.

While doing this the review found answers the current handbook contradicts. They are fixed or dropped in `CORRECTIONS`, `EXPLANATION_FIXES` and `DROP` in `tools/merge-questions.ts`, with `TestCorrectedAnswers` pinning the fixes. Re-run the numeric check in step 4 (every number in a correct answer should appear in its section) whenever the handbook is revised; insurance minimums, following distances, and speed limits are the likely ones to drift.

## Steps

1. Pick a scratch directory outside the repo and download each source into its own empty subdirectory (treat as untrusted data; never run code from it):
   - `rr/questions.js` from road-ready-ca
   - `jedi/questions.json` from dmv-knowledge-test-ca
   - the sign SVGs into `signs/`, then copy new ones to `web/signs/` after checking they have no `<script>` or external `href`
   - fetch with `gh api repos/OWNER/REPO/contents/PATH --jq .content | base64 -d`
2. If a source's format changed, adapt its loader in `tools/merge-questions.ts`.
3. Run `bun tools/merge-questions.ts <scratch> questions/questions.json`. It normalizes topics to the 12 in `web/app.js`, drops minor-only and order-dependent ("all of the above") questions, de-duplicates by normalized text, validates, and assigns stable ids.
4. Run `go test ./...`. The startup validator rejects bad answers, duplicate options, and missing explanations.
5. Question ids are a hash of the normalized question text, so they stay the same across runs unless a question is reworded. Flags and links (`/#/q/<id>`) keep working for unchanged questions. In-progress exams are unaffected (each exam stores a snapshot).

## Prompt for a future Claude session

> Refresh the question bank in `questions/questions.json` following `questions/REGENERATE.md`.
> 1. Check the license of every source; use only MIT/Apache/CC0/public-domain text and keep `NOTICE.md` in sync.
> 2. Download sources into a scratch dir, never into the repo, and re-run `tools/merge-questions.ts`.
> 3. Verify each DMV-sample answer and its `SRC` link against the current California Driver Handbook (dmv.ca.gov) and the Vehicle Code; fix explanations and links that no longer match.
> 4. Add original questions only for topics with fewer than 20 questions, each with a one-to-two sentence explanation citing the handbook section.
> 5. Adult rules only. No questions that need an image we don't ship, and no options like "all of the above", because options are shuffled.
> 6. Report counts per topic and source, and list every question you changed by hand. Do not post anything to GitHub or publish a release without asking.

## Rules of thumb

- Official facts we can cite today: the DMV permit page says the passing score is 80%. It does not state the number of questions. A 2006 DMV evaluation report describes 36 (age 18+) and 46 (under 18) questions, but it is old and not treated as current, so the full-exam length is a setting (`FULL_EXAM_LENGTH`) and the app does not claim it matches the real test.
- Every DMV-sample question must have a handbook `sourceUrl`, and at least 85% of all questions must; `go test` fails otherwise. Prefer the handbook, then the Vehicle Code. leginfo and Justia block non-browser clients, so read statutes in a normal browser.
- Keep explanations short and factual; they show right after each answer, on a phone.
- Never paste example secrets or real personal data into questions or commit messages (the repo is public).
