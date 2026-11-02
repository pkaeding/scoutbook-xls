# scoutbook-xls

## Overview

`scoutbook-xls` generates an XLSX spreadsheet of a Cub Scout den's progress through rank requirements and adventures, by pulling data from the unofficial API at `advancements.scouting.org`.

Caveats worth knowing up front:

- It uses an unofficial, undocumented API. It can break whenever Scouting.org changes things.
- Authentication is a JWT you paste in after copying it out of your browser's DevTools.
- That token expires roughly every 8 hours, so expect to refresh it occasionally.

## Install

### Download a release (recommended)

1. Go to the [releases page](https://github.com/pkaeding/scoutbook-xls/releases) and open the latest release.
2. Under **Assets**, download the archive for your platform:

   | Platform | File to download |
   |---|---|
   | macOS, Apple Silicon (M1/M2/M3/M4) | `scoutbook-xls_..._darwin_arm64.tar.gz` |
   | macOS, Intel | `scoutbook-xls_..._darwin_amd64.tar.gz` |
   | Windows, 64-bit | `scoutbook-xls_..._windows_amd64.zip` |
   | Linux, 64-bit | `scoutbook-xls_..._linux_amd64.tar.gz` |

3. Extract the archive:
   - **macOS / Linux**: `tar -xzf scoutbook-xls_*.tar.gz`
   - **Windows**: right-click the `.zip` → **Extract All**

4. The extracted folder contains the `scoutbook-xls` binary (or `scoutbook-xls.exe` on Windows). You can run it from that folder, or move it somewhere on your PATH for convenience (e.g. `/usr/local/bin` on macOS/Linux).

**macOS Gatekeeper note**: because the binary isn't signed through Apple's paid developer program, macOS may block it on first run. If you see a warning, open **System Settings → Privacy & Security**, scroll down, and click **Allow Anyway** next to the `scoutbook-xls` entry.

### Alternatives

If you have Go installed, you can install directly from source:

```
go install github.com/pkaeding/scoutbook-xls@latest
```

Or build from a local clone:

```
git clone https://github.com/pkaeding/scoutbook-xls.git
cd scoutbook-xls
make build
```

## Getting your Scouting.org token

1. In Chrome or Firefox, go to `https://advancements.scouting.org` and sign in.
2. Open DevTools (F12, or Cmd+Option+I on macOS).
3. Click the **Network** tab.
4. Back in the page, click any scout's name or any roster item. This triggers API calls.
5. In the Network panel, find any request to `api.scouting.org` and click it.
6. Under **Headers -> Request Headers**, find the header `Authorization: Bearer eyJ...`. Copy the long string after `Bearer ` (do not include the word `Bearer`).
7. Paste that token into your config file (see below) or pass it via `--token`.

Tokens expire every ~8 hours. If `scoutbook-xls` reports that your token is expired, repeat these steps to grab a fresh one.

## Configuration

Create a `scoutbook-xls.yaml`:

```yaml
# Get this from DevTools (see "Getting your Scouting.org token")
token: eyJhbGciOi...

# Optional - auto-discovered if you're in exactly one Pack
# pack-org-guid: 00000000-0000-0000-0000-000000000000

# Only for import-blackpug: your TROOP's organization GUID (not the Pack's)
# troop-org-guid: 11111111-1111-1111-1111-111111111111

den-type: Webelos
den-number: "1"

# Optional - defaults to "{den-type}-{den-number}-progress.xlsx"
# output: my-pack-progress.xlsx
```

The config file is looked up in this order:

- `./scoutbook-xls.yaml` (current working directory)
- `$HOME/.scoutbook-xls.yaml` (recommended for a user-wide setup)
- `--config /path/to/other.yaml` (explicit override)

Values are resolved with this precedence (highest first):

1. Command-line flags
2. Environment variables: `SCOUTBOOK_TOKEN`, `SCOUTBOOK_PACK_ORG_GUID`, `SCOUTBOOK_TROOP_ORG_GUID`, `SCOUTBOOK_DEN_TYPE`, `SCOUTBOOK_DEN_NUMBER`, `SCOUTBOOK_OUTPUT`
3. Config file
4. Built-in defaults

## Usage

```
# Minimal (reading scoutbook-xls.yaml from cwd or $HOME):
scoutbook-xls

# With flags:
scoutbook-xls --token eyJ... --den-type Webelos --den-number 1

# Custom output path:
scoutbook-xls --den-type Bear --den-number 3 --output bear3.xlsx
```

## Importing merit badges from Black Pug

`scoutbook-xls import-blackpug` records the merit badge requirements from a Black Pug (summer camp) CSV export in Scoutbook, as if you had used the **bulk entry (merit badges)** page: each requirement is dated as in the export, and marked entered and approved today by the leader whose token you supply.

```
# 1. Dry run (the default): reads from Scouting.org, changes nothing.
scoutbook-xls import-blackpug --troop-org-guid <troop-org-guid> camp-export.csv

# Optional: limit to one or more badges, e.g. to check one in Scoutbook first.
scoutbook-xls import-blackpug --badge Leatherwork --badge Pioneering camp-export.csv

# 2. If the report looks right, record it.
scoutbook-xls import-blackpug --troop-org-guid <troop-org-guid> --apply camp-export.csv
```

`--troop-org-guid` is the troop's organization GUID. It is separate from `pack-org-guid`, which the den report uses, so one config file can hold both. (Before this command existed the Pack setting was called `org-guid`; that name is no longer read, and you'll get a warning if it's still set.) It, and `--token`, can also come from the config file or environment. `--apply` is deliberately flag-only.

How it behaves:

- Scouts are matched by **BSA Member ID** against the troop roster; badges by name and the export's `Version` column.
- Requirements that already have a completion date are left alone, so re-running an import is safe.
- It is **all-or-nothing per scout and badge**: if any requirement number in the export can't be matched to that badge's requirement list, nothing is recorded for that scout and badge, and the unmatched numbers are printed. (Black Pug spells numbers many ways — `3a`, `3(a)`, `10.`, `1g[2]`, `2 Option A (4)` — and these are normalized before matching; anything still ambiguous is reported rather than guessed.)
- `Award Requirement` rows (e.g. Kayaking BSA) and rows not marked approved are ignored, with a warning.
- A `Merit Badge` row is not written separately. Recording all of a badge's requirements is what completes it; the command warns if Scouting.org doesn't report the badge as Leader Approved afterwards.
- The command exits non-zero if anything was skipped or failed.

Always review the dry-run output before using `--apply`. The export itself is personal data about minors: don't commit it.

## Output format

The generated XLSX has:

- A **summary sheet** named after the den (for example, `Webelos 1`):
  - First, one row per rank requirement, with each cell showing that scout's percent complete on that requirement.
  - Then, one row per adventure the den has started (any scout above 0%), with each cell showing that scout's percent complete on the adventure.
  - Percentages are stored as numbers (`0.5` = 50%) and formatted with the `0%` cell format.
- A **per-adventure sheet** for each adventure listed on the summary. Rows are that adventure's requirements. Each cell shows the date the scout completed that requirement (blank if not completed). A final `% Complete` row shows each scout's overall percent complete on the adventure.

## Troubleshooting

- **"token expired" / 401**: your JWT is stale. Grab a fresh one from DevTools (see above).
- **"found 0 Pack units"**: your Scouting.org account isn't registered in any Pack. If you believe you are in exactly one Pack and still see this, set `pack-org-guid` manually in the config.
- **"found multiple Packs"**: the error lists the packs it found. Pick one and set its `pack-org-guid` in the config.
- **"no scouts matched den Wolf 3"**: double-check `den-type` and `den-number`. `den-type` is case-sensitive and must be one of `Lion`, `Tiger`, `Wolf`, `Bear`, `Webelos`, or `Arrow of Light`. `den-number` must match the value on the scout's record exactly (quote it in YAML so it stays a string).

## Development

```
make build
make test
```

## Disclaimer

This tool calls an unofficial, undocumented API and could break at any time if Scouting.org changes things. It is not affiliated with or endorsed by the Boy Scouts of America.
