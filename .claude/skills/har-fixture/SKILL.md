---
name: har-fixture
description: Extract response bodies from a HAR file and save them as JSON fixtures for testing. Use when writing tests for the Scouting API client — pull real responses out of a freshly captured HAR instead of hand-crafting payloads.
---

# Turning HAR entries into test fixtures

HAR (HTTP Archive) files are JSON dumps of browser network traffic. They're how we bootstrap new test fixtures from a real session on `advancements.scouting.org`.

**Do not commit HAR files.** They contain extensive PII (addresses, phones, DOBs, guardian info). `.gitignore` excludes `*.har`. Capture outside the repo or delete when done. See the `sanitized-test-data` skill for the sanitization procedure before any captured response becomes a committed fixture.

## Capturing a HAR

1. Open DevTools → Network panel on `advancements.scouting.org`.
2. Clear the panel, click through the views you care about (roster, a scout's profile, an adventure).
3. Right-click anywhere in the request list → "Save all as HAR with content".
4. Save to a path OUTSIDE this repo — e.g. `/tmp/scoutbook.har`.

## Response shape

Every entry under `.log.entries[]` has:

- `.request.method`, `.request.url`, `.request.headers[]`
- `.response.status`, `.response.content.text` — the response body, **as a JSON-encoded string** (so you need two `jq` passes or `fromjson` to see structured data)

OPTIONS preflights are included, so filter to `GET` (or `POST`) requests.

## Find an entry by URL

```sh
jq -r '.log.entries[]
  | select(.request.method == "GET")
  | select(.request.url | contains("<URL_SUBSTRING>"))
  | .response.content.text' \
  /path/to/capture.har | jq '.'
```

## Save a response to a fixture file

```sh
jq -r '.log.entries[]
  | select(.request.url == "https://api.scouting.org/<endpoint>")
  | select(.request.method == "GET")
  | .response.content.text' \
  /path/to/capture.har \
  | jq '.' > /tmp/raw-fixture.json
```

Then **run the sanitization procedure** (see `sanitized-test-data` skill) before the fixture lands in `testdata/fixtures/`. A raw capture must never be committed.

## Quirks

- **Duplicate entries exist** — the user may hit the same endpoint more than once. Payloads are usually identical; first or last is fine.
- **Response bodies are strings, not objects** — the `.text` field is a JSON-encoded string. Use `jq -r ... | jq '.'` or `fromjson` inside one pipeline: `jq '.log.entries[] | ... | .response.content.text | fromjson'`.
- **`x-esb-url` header is base64** — decode with `base64 -d` to see what URL the SPA thought it was fetching.
- **Auth headers are in the HAR** — the JWT is visible as `Authorization: Bearer ...`. That token is personal and revocable; don't share the HAR.

## Fixture naming

Use a descriptive, endpoint-shape-based pattern. Canonical-scout fixtures use `_wesley` (per `sanitized-test-data`). Examples of existing fixtures:

| Purpose | Filename |
|---------|----------|
| Pack roster | `roster_pack0123.json` |
| Canonical scout profile by GUID | `profile_wesley_by_guid.json` |
| Canonical scout profile by userId | `profile_wesley_by_userid.json` |
| Adventures list for one scout | `adventures_wesley.json` |
| Rank requirements for one scout | `rank_webelos_wesley.json` |
| Adventure-specific requirements | `adventure_140_myfamily_wesley.json` |
| Logged-in user profile | `me_profile.json` |

For a new canonical scout, pick a different fictional first name per `sanitized-test-data` conventions.
