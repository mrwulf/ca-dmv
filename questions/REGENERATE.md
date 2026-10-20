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
- Every DMV-sample question must have a `sourceUrl`; `go test` fails otherwise. Prefer the handbook, then the Vehicle Code. leginfo and Justia block non-browser clients, so read statutes in a normal browser.
- Keep explanations short and factual; they show right after each answer, on a phone.
- Never paste example secrets or real personal data into questions or commit messages (the repo is public).
