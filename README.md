# CA DMV practice

A small, phone-first practice app for the California Class C written knowledge test.

- Random exams from a bank of about 450 questions: a full practice exam (36 questions by default) or a quick 10. The pass mark is 80%, the figure the DMV publishes.
- The right answer and an explanation appear right after each answer, with a link to the Driver Handbook section it comes from where the handbook states it, also in the "Think this is wrong?" box.
- A "🔥 N in a row" streak during a test, and confetti when a test is passed (more for a perfect score; skipped if the device asks for reduced motion).
- Official illustrations from the Driver Handbook: traffic signals and signs with their questions, and parking, turning, and passing diagrams with the explanation after you answer.
- After a missed question, take a focused test on that question's category (10 questions from that category only).
- Flag any question you think is wrong (with an optional note), review the flagged list, and open any question directly by link (`/#/q/<id>`).
- Per-user history: score over time, a mastery bar across all questions, per-topic accuracy (weakest first), and a review of missed questions.
- Sign in with any OpenID Connect provider (built for Pocket ID). The app keeps only the OIDC subject and display name.
- One static Go binary, SQLite storage, rootless distroless image.

Unofficial study aid. Not affiliated with the California DMV. See [NOTICE.md](NOTICE.md) for question sources and licenses.

## Configuration

All settings are environment variables.

| Variable | Required | Default | Notes |
|---|---|---|---|
| `BASE_URL` | yes | | Public URL, e.g. `https://dmv.example.com`. Used for the OIDC redirect and the same-origin check. `https` turns on `Secure` cookies |
| `OIDC_ISSUER_URL` | yes | | Provider issuer; discovery runs at `/.well-known/openid-configuration` |
| `OIDC_CLIENT_ID` | yes | | |
| `OIDC_CLIENT_SECRET` | no | | Leave empty for a public client (PKCE is always used) |
| `DB_PATH` | no | `/data/ca-dmv.db` | Needs a writable directory (SQLite WAL) |
| `LISTEN_ADDR` | no | `:8080` | |
| `OIDC_LOGOUT_PROVIDER` | no | `false` | Set to `true` to also end the provider session on sign-out (see below) |
| `FULL_EXAM_LENGTH` | no | `36` | Questions in the full practice exam. The DMV does not currently publish the real test's length, so adjust to taste |

In the provider, register the redirect URI `${BASE_URL}/auth/callback` and the scopes `openid profile email`. Access control is whatever your provider enforces (for Pocket ID, restrict the client to a user group); the app has no roles.

**Signing out** ends only this app's session and shows a signed-out screen. The provider session stays, so the user's other apps stay signed in. The "Sign in again" button asks the provider for `prompt=select_account` (when it advertises that value), so the provider asks which account to use and a different user can sign in on the same phone. To also end the provider session, which signs the user out of every app that uses it, set `OIDC_LOGOUT_PROVIDER=true` and register `${BASE_URL}/` as a post-logout redirect URI; the browser is then sent to the provider's `end_session_endpoint` (found through discovery) with the ID token.

## Run

```sh
docker run -d --name ca-dmv -p 8080:8080 -v ca-dmv-data:/data \
  -e BASE_URL=https://dmv.example.com \
  -e OIDC_ISSUER_URL=https://id.example.com \
  -e OIDC_CLIENT_ID=... \
  ghcr.io/mrwulf/ca-dmv:edge
```

Kubernetes notes: run as non-root (uid/gid 65532), `readOnlyRootFilesystem: true` works, mount a PVC at `/data` with `fsGroup: 65532`, use `/healthz` for probes, and keep **one replica** (SQLite). Back up the PVC if you care about history.

Images are published to `ghcr.io/mrwulf/ca-dmv` for `linux/amd64` and `linux/arm64`: `edge` follows `main`, and a `vX.Y.Z` tag publishes `X.Y.Z` and `X.Y`.

## Develop

```sh
go test ./...        # includes a fake-OIDC login test
go vet ./...
```

The web UI lives in `web/` (vanilla JS, no build step, no inline scripts or styles because of the CSP) and is embedded in the binary, so rebuild after editing it.

How it works:

- An exam is created server-side with random questions and shuffled options, and stored as a snapshot. Answers and explanations are only sent to the browser after a question is answered.
- Sessions are random tokens stored hashed in SQLite (30 days, `HttpOnly`, `SameSite=Lax`). Login uses the authorization code flow with PKCE, a nonce, and a state cookie bound to the browser.
- Starting a new exam marks the user's unfinished one as deleted.
- Flags are per user and store the question text, so they stay readable if the bank changes. Question ids are a hash of the question text and are stable across regeneration unless the wording changes.

## Mastery

The My stats screen shows how much of the whole question bank the pupil has mastered. A question is **mastered** when they got it right the last time they saw it (in a finished test that has not been deleted), **missed** when they got it wrong the last time, and **unseen** otherwise. The bar is mastered divided by every question in the bank.

## Streaks and achievements

- **Practice streak** (Home): consecutive calendar days, in the pupil's own time zone, with at least one finished test. Any test counts, including quick and focused ones. The streak stays alive until the end of today, so it only breaks after a full day with no test. Home also shows the last seven days and the longest streak. Deleting a test removes its day from the streak.
- **Results page** shows an achievement when the test earned one, as it stood when the test was taken:
  - 🏆 **New personal best**: a higher percentage than every earlier test of the same length and category, once there are at least two to compare with.
  - 🔥 **Day N of your practice streak**: the first test of a day that makes a streak of 2 or more.
  - 🏅 **Mastery milestone**: the test took overall mastery past 25%, 50%, 75%, or 100%.
  - ⭐ **Every question mastered in a category**: this test completed a category. Fully mastered categories also get a ⭐ on My stats.
- **Confetti** and a **🔥 N in a row** counter during a test are cosmetic and use no stored data.

## Leaderboard

The Leaderboard button on the home screen (`/#/leaderboard`) is open to any signed-in user. It lists everyone who has finished at least one test, sorted by mastery (buttons switch to tests taken, last practiced, or name). Selecting a user shows their progress page: score chart, mastery, topic bars, and a list of their tests with the date, time and percentage.

Only first names are shown, everywhere in the app: the first word of the name the identity provider sends, or the first word of the local part when it only sends an email address. Two people with the same first name are told apart by their results only.

What other users can see: first name, mastery, test counts, and the date, time, topic and score of each finished test. They cannot open individual tests or see answers, flags or notes. Deleted tests are left out. Users are addressed by a one-way hash of their subject id, so the page and API never expose it.

## Deleting a test

There is no delete button, on purpose. To delete a test, sign in, open that test's results page (from the Recent exams list on My stats), and add `/delete` to the end of the address:

```text
https://dmv.example.com/#/results/<test id>/delete
```

The page shows a warning and asks the pupil to type `DELETE` before the button works. Deleting removes the test from the progress graph, topic accuracy, mastery, and pass rate. Only the signed-in user's own tests can be deleted.

Nothing is removed from the database: the test and its answers stay, and the test is only marked deleted (`exams.deleted_at` is set). To restore one, clear that column with SQL. Starting a new test also marks the user's unfinished test deleted this way.

## Updates

Every response carries an `X-App-Build` header and the page embeds the build it was loaded from. When the two differ (a new release is being served), the page reloads itself on its next API call, or when the tab becomes visible again. Reloads are limited to one per 30 seconds so a rollout with old and new pods side by side cannot loop. The first release with this behavior still needs one manual refresh.

## Version

The home screen footer shows the release tag and short git SHA. The image build stamps them in (`edge` for builds of `main`, the tag for releases), and a plain `go build` in a git checkout reports `dev` and the checkout's SHA. The same values are logged at startup.

## Questions

`questions/questions.json` is generated by `tools/merge-questions.ts` from MIT-licensed GitHub banks and the DMV's published sample tests. See [questions/REGENERATE.md](questions/REGENERATE.md) to refresh or extend it, including a ready-to-use prompt for a future Claude session.

## License

MIT for the code. Question content and some images have their own terms, see [NOTICE.md](NOTICE.md). The hand-signal pictures are CC BY-NC 4.0 from the California Driver's Handbook, so this project is for non-commercial use.
