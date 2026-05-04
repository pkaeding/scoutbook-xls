---
name: scouting-api
description: How to call the unofficial advancements.scouting.org API (api.scouting.org). Use when fetching roster data, scout profiles, adventures, rank requirements, or adventure requirements. Covers auth, the x-esb-url header, rate/retry, expected shapes, and quirks like the polymorphic /personprofile endpoint.
---

# Calling the Scouting.org advancements API

This API powers `https://advancements.scouting.org` and is not publicly documented. Observed behavior below is from the HAR at `advancements.scouting.org_Archive [26-05-02 23-36-02].har`.

## Base URL
`https://api.scouting.org`

## Auth
Every call needs:

- `Authorization: Bearer <JWT>` — the JWT the SPA pulls from `localStorage` / session. Expires every ~8 hours. User supplies via config/flag/env.
- `x-esb-url: <base64(original-page-url)>` — e.g. `base64("https://advancements.scouting.org/roster")`. Any reasonable value of this form is accepted; you can use a constant.
- `Origin: https://advancements.scouting.org`
- `Referer: https://advancements.scouting.org/`

Returning 401 → JWT expired. Surface a clear message telling the user to grab a fresh token (DevTools → Network → any XHR to api.scouting.org → copy `authorization` value).

The JWT payload contains `pgu` (logged-in user's personGuid) and `uid` (numeric userId). Parse without verification — just base64-decode the middle segment.

## Key endpoints

### Discover who I am + what orgs I belong to
`GET /persons/v2/{myPersonGuid}/personprofile`

Returns `organizationPositions[]` with `organizationGuid`, `unitType` (`Pack`/`Troop`/...), `unitNumber`, `organizationName`, plus my `positions[]` in each unit. Use this to auto-discover the Pack's orgGuid.

### Pack roster
`GET /organizations/positions/{orgGuid}`

Returns `{ Positions: [...] }`. Filter `positions[]` where `positionLong == "Youth Member"`, then iterate `personsAssigned[]`. Each entry has `fullName`, `personGuid`, `registrationId`. **No `userId` and no den info here** — you have to go through personprofile for each.

### Scout profile (POLYMORPHIC — same path, different response shape)
`GET /persons/v2/{personGuid-or-userId}/personprofile`

- Called with a **personGuid (UUID)**: returns `profile.userId` and `organizationPositions[]` (with position info for that scout — but NO `currentProgramsAndRanks`).
- Called with a **numeric userId**: returns `currentProgramsAndRanks[]` (with `denType`, `denNumber`, `denId`, `rankId`, `rank`) — but `profile.userId` is `null`.

So to get a scout's den, you need **both** calls: GUID → userId, then userId → den. Do them in parallel across the roster.

### Scout's adventures (all ranks)
`GET /advancements/v2/youth/{userId}/adventures`

Returns a flat array — every adventure across every Cub Scout rank (~68 items). Each item has:
- `adventureId`, `adventureName`, `shortName`, `rankId`, `isRequired` (required for rank vs. elective)
- `percentCompleted` (0..1), `status` (`Started`, `Leader Approved`, `Awarded`, etc.), `dateCompleted`
- `versionId` (needed if you want the adventure's canonical requirement list)

Filter by `rankId` to get just the target rank's adventures.

### Scout's per-adventure requirement detail
`GET /advancements/v2/youth/{userId}/adventures/{adventureId}/requirements`

Returns one adventure with `requirements[]`. Each requirement has:
- `requirementId`, `requirementNumber` (like `"1"`, `"2a"`), `requirementName`, `shortName`
- `isCompleted`, `percentCompleted`, `dateCompleted`, `status`, `leaderApprovedDate`
- Hierarchy: `parentRequirementId`, `childrenRequired` (often blank)

### Scout's rank requirements
`GET /advancements/v2/youth/{userId}/ranks/{rankId}/requirements`

Returns the rank object with `requirements[]`. Each has `requirementNumber`, `name`, `completed`, `percentCompleted`, `dateCompleted`, plus `linkedAdventureId`, `linkedAdventure{}`, or `linkedElectiveAdventures[]`.

Important: when a requirement is "complete N electives", it shows up with `electiveAdventure: true` and `linkedElectiveAdventures[]` listing every elective option for the rank (each with its own `percentCompleted` for that scout). The `percentCompleted` on the requirement itself already reflects progress toward the count. Use that directly for the summary cell.

### Canonical rank/adventure requirement lists (no user context)
- `GET /advancements/v2/ranks?programId=1` — all Cub Scout ranks.
- `GET /advancements/v2/ranks/{rankId}/requirements?versionId={v}` — rank's requirement tree.
- `GET /advancements/v2/adventures/{adventureId}` — adventure metadata.
- `GET /advancements/adventures/{adventureId}/requirements?versionId={v}` — requirement definitions.

Use the youth-specific endpoints when possible — they include completion state.

## Programs and ranks (programId=1 = Cub Scouting)

| rankId | name          | level | denType matches |
|--------|---------------|-------|-----------------|
| 14     | Lion          | 0     | Lion            |
| 13     | Bobcat        | 1     | —               |
| 8      | Tiger         | 2     | Tiger           |
| 9      | Wolf          | 3     | Wolf            |
| 10     | Bear          | 4     | Bear            |
| 11     | Webelos       | 5     | Webelos         |
| 12     | Arrow of Light| 6     | Arrow of Light  |

`denType` on a scout's `currentProgramsAndRanks` is the target rank name. `rankId` in the same object is the rank they're currently earning (not necessarily the highest they've earned — e.g., a Webelos-den scout might still have `rankId: 10` (Bear) if they haven't finished Webelos yet).

## Concurrency and retries
Small pack → ~150 requests for a full report. Cap concurrency at 8; parallel is fine. Retry on 429 and 5xx with exponential backoff (start 500ms, max 3 tries). Everything else is a hard error.

## Don't
- Don't verify the JWT signature — you don't have the secret, and you don't need to.
- Don't paginate — none of these endpoints appear to paginate within a single unit.
- Don't assume scouts in one den share a `rankId` (they usually do, but at rank-advancement boundaries they may not).
