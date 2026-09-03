# Typsnittsjakten: Pingit Font Pairings

**Date:** 2026-09-03

## Question

Find 3-5 free, self-hostable font pairings for Pingit (Swedish ping-pong league PWA):

- **(a) Display/condensed** for giant score digits and headers, replacing the Arial Narrow fallback. Must have tabular numerals (OpenType `tnum` feature, OR monospaced-width default digits) and full aa/ao/oo (Swedish) coverage.
- **(b) Body/UI grotesque**, visually close to Inter at weight 800 lowercase (chunky, friendly, rounded-ish). Needs Swedish character coverage, ideally `tnum` too, for league table numerals.

All candidates must be OFL/permissive-licensed and self-hostable (bundled font files, no CDN dependency).

## Verification method

Downloaded the actual `.ttf` files (from `google/fonts` GitHub repo's `ofl/` directory, plus one foundry repo) into a scratch directory, then inspected each with Python fontTools (`TTFont`):

- `getBestCmap()` checked for the 6 Swedish codepoints (U+00E5/E4/F6 lowercase, U+00C5/C4/D6 uppercase).
- `GSUB` feature list checked for the `tnum` tag.
- `hmtx` advance widths of glyphs `0`-`9` compared to see if default (non-tnum) digits are already monospaced.
- `fvar` table checked for variable-font axes (tag, min/default/max).
- For Bebas Neue's "no real lowercase" claim: compared `glyf` table entries for `a` vs `A` etc, and inspected composite-glyph components directly.
- License confirmed by METADATA.pb `license:` field per family, and by reading `OFL.txt` full text for two of them.

This surfaced two important disqualifications from the candidate pool that are worth flagging up front:

| Font | Result |
|---|---|
| Oswald | `tnum` absent from GSUB; default digit widths vary 378-517px. Fails requirement (a) outright despite being widely assumed sports/scoreboard-safe. |
| Anton | `tnum` absent; digit `1` is 677px vs 1012px for all others (proportional, not tabular). Also confirmed genuinely single-weight (METADATA lists only weight 400). Fails requirement (a). |
| Saira Condensed | `tnum` absent in both Black and ExtraBold statics; digit widths vary about 319-494px. Fails requirement (a). |

These three are excluded from the pairings below. Everything in the tables was verified directly, not assumed from documentation.

## Pairings

### 1. Archivo + Hanken Grotesk

| Fact | Archivo (display) | Hanken Grotesk (body) |
|---|---|---|
| License | OFL 1.1 (verified full OFL.txt text) | OFL 1.1 (verified full OFL.txt text) |
| Variable font | Yes - axes `wght` 100-900, `wdth` 62-125% | Yes - axis `wght` 100-900 |
| Swedish coverage | Full | Full |
| `tnum` GSUB feature | Yes | No |
| Default digits monospaced | No (575-577px, needs `tnum` on) | Yes (560px, all 10 digits) |
| Gotchas | Named instance at default position is "Archivo SemiBold" - must set `wght`/`wdth` explicitly for Black+Condensed look. `tnum` must be turned on via CSS `font-feature-settings: "tnum"` or `font-variant-numeric: tabular-nums`. | Digits are tabular by default with no feature flag needed - safer against a dev forgetting to enable `tnum` in the table component. |

- Score digits: Archivo `wght` 800-900, `wdth` about 75 (condensed), `tnum` on.
- Table numerals: Hanken Grotesk default (already tabular).
- Headers: Archivo `wght` 700-800, `wdth` about 85.
- Body text: Hanken Grotesk `wght` 400-500; UI labels at 700-800 for the Inter-800 look.

Rationale: one variable file each covers the whole condensed-display range and the whole body-weight range - smallest total download for a PWA, and Hanken Grotesk's default-tabular digits are a safety net if `tnum` gets missed anywhere in the table UI.

### 2. Barlow Condensed (Black/ExtraBold statics) + Schibsted Grotesk

| Fact | Barlow Condensed (display) | Schibsted Grotesk (body) |
|---|---|---|
| License | OFL 1.1 (METADATA.pb license field) | OFL 1.1 (METADATA.pb license field) |
| Variable font | No reliable one. Google Fonts ships 18 static files (Thin-Black + italics), no `fvar`. The foundry repo (jpt/barlow) has a fonts/gx/BarlowGX.ttf, but it's the un-condensed base family with a legacy pre-OpenType-1.8 GX axis layout (`wght` 22-188, `wdth` 300-500 - non-standard ranges) and no `tnum` feature at all. Treat Barlow Condensed as static-only. | Yes - axis `wght` 400-900 only (no lighter weights below Regular) |
| Swedish coverage | Full | Full |
| `tnum` GSUB feature | Yes (both Black and ExtraBold checked) | Yes |
| Default digits monospaced | No (needs `tnum` on) | No (needs `tnum` on) |
| Gotchas | Must ship two-plus static weight files (e.g. Black + ExtraBold) since there's no working variable build; larger PWA payload than the variable-font pairings. | `wght` axis floors at 400 - can't get a lighter body weight from this one file if you ever want it. |

- Score digits: Barlow Condensed Black, `tnum` on.
- Table numerals: Schibsted Grotesk, `tnum` on.
- Headers: Barlow Condensed ExtraBold.
- Body text: Schibsted Grotesk 400-500; 700-800 for chunky UI labels.

Rationale: Barlow Condensed Black is about as close to a "sport scoreboard" condensed grotesque as Google Fonts has with verified tabular support; Schibsted Grotesk's rounded, slightly quirky grotesque shapes pair well without competing with the display face.

### 3. Bebas Neue + Manrope

| Fact | Bebas Neue (display) | Manrope (body) |
|---|---|---|
| License | OFL 1.1 (METADATA.pb license field) | OFL 1.1 (METADATA.pb license field) |
| Variable font | No - static, single weight/style file | Yes - axis `wght` 200-800 |
| Swedish coverage | Full | Full |
| `tnum` GSUB feature | Yes | Yes |
| Default digits monospaced | Yes (400px, all 10 digits - tabular even with `tnum` off) | No (needs `tnum` on) |
| Gotchas | Confirmed: no real lowercase. Checked `glyf` table directly - lowercase glyphs (a, b, e, s, checked explicitly) are composite glyphs whose only component is the uppercase glyph (e.g. glyph a resolves to a single component referencing glyph A, identical bounding box). Typing lowercase text renders as capitals. Fine for score/header display copy that's naturally all-caps anyway; do not use for anything that needs true lowercase legibility. Single weight only - no bold/black variant exists. | Default instance is "Manrope ExtraLight" - must set `wght` explicitly to reach the 800 "chunky Inter" look. |

- Score digits: Bebas Neue (already tabular by default, no feature flag needed).
- Table numerals: Manrope, `tnum` on.
- Headers: Bebas Neue (all-caps only - fine for short header labels like "SEMIFINAL").
- Body text: Manrope `wght` 400-500; 800 for UI chunk that should echo the Inter-800 reference.

Rationale: Bebas Neue's default-monospaced digits are the most foolproof tabular-numeral guarantee in the whole pool (works even if a dev forgets `font-variant-numeric`), at the cost of losing lowercase - acceptable for score/header use but not a general-purpose display face.

### 4. Fjalla One + Space Grotesk

| Fact | Fjalla One (display) | Space Grotesk (body) |
|---|---|---|
| License | OFL 1.1 (METADATA.pb license field) | OFL 1.1 (METADATA.pb license field) |
| Variable font | No - static, single weight/style file | Yes - axis `wght` 300-700 |
| Swedish coverage | Full | Full |
| `tnum` GSUB feature | Yes | Yes |
| Default digits monospaced | No (826-1090px, needs `tnum` on) | No (needs `tnum` on) |
| Gotchas | Genuinely single-weight (METADATA lists only weight 400) - no bold/black cut exists at all, so headers and score digits share one exact weight; lean on size/color for hierarchy instead of weight. | `wght` axis tops out at 700, not 800/900 - can't push it as dark as Inter 800 with weight alone; may need to compensate with letter-spacing or size for the "chunky" feel. |

- Score digits: Fjalla One, `tnum` on.
- Table numerals: Space Grotesk, `tnum` on.
- Headers: Fjalla One (only weight available).
- Body text: Space Grotesk `wght` 400-500; 700 (its max) for UI emphasis.

Rationale: Fjalla One is a purpose-built condensed display face (already semi-bold-weight by design) that keeps the PWA's font payload tiny (one static file), and Space Grotesk's slightly geometric/quirky grotesque shapes give the pairing a distinct "sporty tech" character rather than a generic corporate-sans look.

## Recommendation

Top pick: Archivo + Hanken Grotesk.

Reasons:
1. Both are single variable-font files covering the entire weight (and, for Archivo, width) range needed - smallest total bytes shipped to the PWA of any pairing here, which matters for a mobile-first self-hosted app.
2. Archivo's `wdth` axis lets the team dial in exactly how condensed the score digits/headers look without needing separate font files per condensed level, and it has verified `tnum` support.
3. Hanken Grotesk's default digits are tabular with zero CSS feature-flag risk - the league table numerals stay aligned even if a developer forgets to set `font-variant-numeric: tabular-nums` somewhere.
4. Both families keep real lowercase and a full weight range, so the same two font files can also flex into other UI needs later (unlike Bebas Neue's caps-only limitation or Fjalla/Anton's single-weight ceiling).

Runner-up: Bebas Neue + Manrope, if the team wants a more emphatically "scoreboard" all-caps display face and is fine with Bebas Neue only ever being used for short all-caps strings (scores, short headers) - its default-monospaced digits are the single safest tabular-numeral guarantee found in this whole investigation.

## Sources

Google Fonts GitHub repo (ofl/ = Open Font License directory; family folders browsed via GitHub API, files fetched via raw.githubusercontent.com):
- https://github.com/google/fonts/tree/main/ofl/archivo - Archivo[wdth,wght].ttf
- https://github.com/google/fonts/tree/main/ofl/barlowcondensed - BarlowCondensed-Black.ttf, BarlowCondensed-ExtraBold.ttf (plus full static weight list checked)
- https://github.com/google/fonts/tree/main/ofl/oswald - Oswald[wght].ttf
- https://github.com/google/fonts/tree/main/ofl/anton - Anton-Regular.ttf
- https://github.com/google/fonts/tree/main/ofl/bebasneue - BebasNeue-Regular.ttf
- https://github.com/google/fonts/tree/main/ofl/fjallaone - FjallaOne-Regular.ttf
- https://github.com/google/fonts/tree/main/ofl/sairacondensed - SairaCondensed-Black.ttf, SairaCondensed-ExtraBold.ttf
- https://github.com/google/fonts/tree/main/ofl/inter - Inter[opsz,wght].ttf
- https://github.com/google/fonts/tree/main/ofl/hankengrotesk - HankenGrotesk[wght].ttf
- https://github.com/google/fonts/tree/main/ofl/schibstedgrotesk - SchibstedGrotesk[wght].ttf
- https://github.com/google/fonts/tree/main/ofl/manrope - Manrope[wght].ttf
- https://github.com/google/fonts/tree/main/ofl/publicsans - PublicSans[wght].ttf
- https://github.com/google/fonts/tree/main/ofl/figtree - Figtree[wght].ttf
- https://github.com/google/fonts/tree/main/ofl/spacegrotesk - SpaceGrotesk[wght].ttf

Foundry repo checked for a Barlow Condensed variable build:
- https://github.com/jpt/barlow/tree/main/fonts/gx - BarlowGX.ttf (found to be non-condensed base family, legacy GX axis format, no tnum; not usable for this pairing)

License text spot-checks (full OFL 1.1 text read):
- https://raw.githubusercontent.com/google/fonts/main/ofl/archivo/OFL.txt
- https://raw.githubusercontent.com/google/fonts/main/ofl/hankengrotesk/OFL.txt

METADATA.pb license field checked for: Anton, Barlow Condensed, Bebas Neue, Manrope, Schibsted Grotesk, Fjalla One (all "OFL").

fontTools verification (all facts in the tables above - Swedish coverage, tnum presence, default digit widths/monospacing, fvar axes - were produced by running the verification script described above against each downloaded file): Anton-Regular.ttf, Archivo.ttf, BarlowCondensed-Black.ttf, BarlowCondensed-ExtraBold.ttf, BarlowGX.ttf, BebasNeue-Regular.ttf, Figtree.ttf, FjallaOne-Regular.ttf, HankenGrotesk.ttf, Inter.ttf, Manrope.ttf, Oswald.ttf, PublicSans.ttf, SairaCondensed-Black.ttf, SairaCondensed-ExtraBold.ttf, SchibstedGrotesk.ttf, SpaceGrotesk.ttf.

Bebas Neue lowercase-glyph claim verified via direct glyf table component inspection on BebasNeue-Regular.ttf (lowercase a/b/e/s each resolve to a single composite component referencing the corresponding uppercase glyph, identical bounding box).
