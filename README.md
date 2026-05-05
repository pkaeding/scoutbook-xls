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
# org-guid: 00000000-0000-0000-0000-000000000000

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
2. Environment variables: `SCOUTBOOK_TOKEN`, `SCOUTBOOK_ORG_GUID`, `SCOUTBOOK_DEN_TYPE`, `SCOUTBOOK_DEN_NUMBER`, `SCOUTBOOK_OUTPUT`
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

## Output format

The generated XLSX has:

- A **summary sheet** named after the den (for example, `Webelos 1`):
  - First, one row per rank requirement, with each cell showing that scout's percent complete on that requirement.
  - Then, one row per adventure the den has started (any scout above 0%), with each cell showing that scout's percent complete on the adventure.
  - Percentages are stored as numbers (`0.5` = 50%) and formatted with the `0%` cell format.
- A **per-adventure sheet** for each adventure listed on the summary. Rows are that adventure's requirements. Each cell shows the date the scout completed that requirement (blank if not completed). A final `% Complete` row shows each scout's overall percent complete on the adventure.

## Troubleshooting

- **"token expired" / 401**: your JWT is stale. Grab a fresh one from DevTools (see above).
- **"found 0 Pack units"**: your Scouting.org account isn't registered in any Pack. If you believe you are in exactly one Pack and still see this, set `org-guid` manually in the config.
- **"found multiple Packs"**: the error lists the packs it found. Pick one and set its `org-guid` in the config.
- **"no scouts matched den Wolf 3"**: double-check `den-type` and `den-number`. `den-type` is case-sensitive and must be one of `Lion`, `Tiger`, `Wolf`, `Bear`, `Webelos`, or `Arrow of Light`. `den-number` must match the value on the scout's record exactly (quote it in YAML so it stays a string).

## Development

```
make build
make test
```

## Disclaimer

This tool calls an unofficial, undocumented API and could break at any time if Scouting.org changes things. It is not affiliated with or endorsed by the Boy Scouts of America.
