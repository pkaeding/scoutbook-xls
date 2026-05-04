---
name: sanitized-test-data
description: Fixture and test-data conventions for scoutbook-xls. Use when adding or editing fixtures in testdata/fixtures/, writing new tests that reference scouts/orgs/IDs, or verifying no real PII has leaked into the repo. All example data uses Star Trek character names so it's obviously fictional.
---

# Sanitized test-data conventions

The `testdata/fixtures/` directory contains JSON snapshots derived from real Scouting.org API responses. All personally-identifying information has been replaced with fictional stand-ins. Follow these conventions when adding or modifying test data.

## Naming theme

Use **Star Trek characters** for every fabricated person. It's immediately recognizable as fictional, avoids accidentally landing on a real user, and gives us plenty of distinct names. Mix TNG / TOS / VOY / ENT as needed.

If you find yourself reaching for a plausible-sounding made-up name — stop and pick a Star Trek character instead.

## ID ranges

Keep synthetic IDs in reserved, obviously-fake ranges so they don't collide with anything real:

| Field | Range | Notes |
|-------|-------|-------|
| personGuid / userGuid (UUID) | `XXXXXXXX-NNNN-NNNN-NNNN-XXXXXXXXXXXX` where X is a group prefix and N is a sequential counter | One group per "role": logged-in user = `1...`, the canonical scout = `2...`, other scouts = `3...` |
| organizationGuid (Pack) | `44444444-4444-4444-4444-444444444444` | Single value for the primary Pack |
| organizationGuid (Troop) | `55555555-5555-5555-5555-555555555555` | |
| organizationGuid (Council) | `66666666-6666-6666-6666-666666666666` | |
| Second Pack (multi-pack fixture) | `77777777-7777-7777-7777-777777777777` | |
| userId | `1000000x` for the logged-in user, `2000000x` for scouts | 8-digit, starts with `10` or `20` |
| memberId | `13000000x` | 9-digit, starts with `130` |
| personId | `4000000x` | 8-digit, starts with `40` |
| registrationId | `16000010x` | 9-digit, starts with `1600001` |
| Pack unitNumber | `0123` (primary), `0456` (secondary) | |
| Troop unitNumber | `0123` | |
| Council number | `99` | |
| Phone numbers | `555-555-xxxx` (North American reserved "fictional" range) | |
| DOB | 2015-01-01 or later, arbitrary month/day | Keep scouts in the 7–11 age range |

## Places / addresses

- City: `Riverside` (the fictional city used in Star Trek — Kirk's birthplace; keeps the theme)
- State: `CALIFORNIA` / `CA`
- County: `Example`
- ZIP: `90000-0001`
- Street: `100 Main St`, or any `NNN <Generic> St/Rd/Ave` pattern

## Charter / unit names

- Pack charter: `Riverside Example Club`
- Troop charter: `Riverside Lodge #99`
- Full unit name pattern: `Pack 0123 RIVERSIDE EXAMPLE CLUB`

## Rank / adventure IDs — leave alone

`rankId`, `adventureId`, `requirementId`, `versionId`, `denType` values (`Webelos`, `Bear`, etc.), and the rank names / adventure names themselves are **public Scouting.org data, not PII**. Don't sanitize them. Tests should continue to reference the real canonical IDs (e.g. Webelos = rankId 11, My Family adventure = 140).

`denId` is a unit-scoped identifier; sanitize it (use `99999`) so it can't be used to look up the original pack.

## JWT fixture

The project includes `testdata/fixtures/jwt.txt`. Any JWT written there must:

- Be structurally valid (3 `.`-separated segments, base64url-encoded header + payload, any signature).
- Contain a far-future `exp` so expiry-based tests don't break over time.
- Use the fake `pgu`, `uid`, `mid`, `user` values from the ID ranges table above.
- The signature doesn't have to be valid — `scouting.ParseJWT` uses `ParseUnverified`.

To regenerate:

```sh
python3 -c "
import base64, json
def seg(d):
    return base64.urlsafe_b64encode(json.dumps(d, separators=(',', ':')).encode()).rstrip(b'=').decode()
h = seg({'alg': 'HS384', 'typ': 'JWT'})
p = seg({
    'stk': 'FAKE_STK',
    'user': 'jpicard',
    'scope': ['bsa-core'],
    'uid': 10000001,
    'mid': '130000001',
    'pgu': '11111111-1111-1111-1111-111111111111',
    'amr': ['pwd'],
    'iat': 1000000000,
    'exp': 9999999999,
    'aud': 'bsa',
    'iss': 'login-api',
    'sub': 'credentials',
    'jti': 'fakejti',
})
print(h + '.' + p + '.fakesig')" > testdata/fixtures/jwt.txt
```

## Adding a new fixture

1. If copying from a new HAR capture (**never commit the HAR**), apply the full ID-replacement table before committing.
2. Pick a Star Trek name not already in use. Grep the existing fixtures first:
   ```sh
   grep -roh '"fullName": "[^"]*"' testdata/fixtures/ | sort -u
   ```
3. Allocate a new UUID + userId from the next free slot in your role's range.
4. Update any test assertions that reference the new fixture.

## Verifying no real PII leaked

After any fixture edit, run:

```sh
grep -riE '<real first name>|<real last name>|<real town>' testdata/ cmd/ internal/
```

(The scrubbing checklist that was used when the repo was originally sanitized lived in a one-time mapping doc and has been deleted — not kept in the repo so it can't accidentally leak.)

If you see any original value, **do not commit**. Scrub, retest, re-grep.

## HAR files

Raw HAR captures from `advancements.scouting.org` contain extensive PII (addresses, phones, DOBs, guardian info). Never commit one.

`.gitignore` excludes `*.har`. If you capture one for local analysis, keep it outside the repo or delete it when done.

## Test assertion style

When you assert on fake data, make the fake-ness explicit:

- Good: `if got := profile.FullName; got != "Wesley Crusher" { ... }` — obviously synthetic.
- Bad: `if got := profile.FullName; got != "John Smith" { ... }` — plausibly real.

For GUIDs, prefer checking via a named constant (`const canonicalScoutGuid = "22222222-..."`) rather than hard-coded strings — easier to trace which scout a test is about.
