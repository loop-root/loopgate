**Last updated:** 2026-03-24

# Downloads clutter sample (dev / Haven testing)

**Recorded:** 2026-03-23 (session: Morph/Haven organize flows)

## What was added

Approximately **50 empty placeholder files** were created under **`~/Downloads`** with:

- Prefix: `morph_clutter_*` (easy to find and delete)
- Random-ish suffixes (timestamp index + hex) so names don’t collide
- Mixed extensions (scripts, archives, media, docs, config-ish) — **no real content**, all zero-byte or empty

**Purpose:** Messy Downloads for testing folder organization, host access, and assistant tooling — **not** real user data.

## Script (repo root)

```bash
./random_files_gen.sh -d ~/Downloads          # default: 50 files, prefix morph_clutter_
./random_files_gen.sh -d ~/Desktop -n 20
./random_files_gen.sh -d ~/Desktop -p junk_   # custom prefix for cleanup glob
./random_files_gen.sh -d ~/tmp -r             # dry run (print paths only)
./random_files_gen.sh -h
```

## How to remove them later

From a shell (adjust `DIR` and prefix if you used `-p`):

```bash
# Preview
ls DIR/morph_clutter_* 2>/dev/null | wc -l

# Delete only these samples
rm -f DIR/morph_clutter_*
```

Review the preview before `rm` if anything else might match your naming pattern.

## Safety

- Files are **empty** (no secrets, no payloads).
- Scoped by filename prefix **`morph_clutter_`** so cleanup is deliberate.
