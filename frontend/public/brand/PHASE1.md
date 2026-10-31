# Phase 1 brand asset integration — 2026-10-31

Base: 9b825df094df6da76a3137348c40453cbdb8114a.
Branch: feat/brand-assets-phase1-20261031.
Worktree: /home/ritel/worktrees/brand-assets-phase1-20261031.
Budget correction: the earlier 1.75 MiB font result did NOT meet PLAN phase 0. Its previous “under 2 MiB budget” acceptance is withdrawn. The authoritative gates are total <= 2,000,000 B, fonts <= 1,000,000 B and formal title font <= 2,500 ms.

Only frontend/public/brand is committed. The original worktree was not edited.
Windows design/ritel-si/sync_brand.py and test_brand.py were edited in place and are outside this Git repository. DESIGN.md was not edited. No product destinations were synchronized and nothing was deployed.

## Content

- Complete I.MingCP cmap (60,017 code points) partitioned into one observed-homepage priority bucket plus 512-code-point extended shards; all 16 variation sequences retained.
- Cormorant Garamond, Cinzel, IBM Plex Sans/Mono: WOFF2 ASCII plus observed punctuation, Latin-1 and Unicode-block extensions, preserving full source repertoires and variable axes. TTF recovery sources stay byte-identical.
- Existing full Noto Sans CJK HK repertoire repartitioned from the 11 old large extension files into observed-homepage priority buckets and 512-code-point cold chunks; the old scattered common UI source uses 128-code-point cold chunks. The original WOFF2 recovery set and Chiron fallback stay available.
- fonts.css contains disjoint unicode-range declarations per face, font-display:swap and content-hash URL keys. font-subsets.json records source and subset SHA-256, counts, ranges and axes. font-preloads.json gives Latin preload URLs with crossorigin=anonymous; page owners must select the above-the-fold faces.
- The approved R path is reused exactly (input SHA-256 66ac770ac5b1a3c4becfc8797c9963582672021ecdd2a6373939925d04a70642). Public SVGs are pure vector. Root aliases and assets/ paths contain the same bytes. ritel-r.svg, ritel-si.svg, ritel-favicon.svg and the Cinzel wordmark have internal light/dark media styles; ritel-r-dark.svg remains the fixed light-ink variant for explicit dark surfaces. PNG: 16/32 favicon and opaque 180px apple-touch-icon.
- asset-manifest.json closes recursive CSS/font references, public logo aliases, licenses and font metadata. sync_brand.py now copies that whole set, verifies every destination byte, and never deletes existing product assets.

## Minimal synchronization for the main integrator

After cherry-picking this branch into the chosen integration worktree, use that worktree as --source. Do not synchronize from the old baseline.

~~~sh
python /mnt/d/codex-ws/servers-ws/design/ritel-si/sync_brand.py \
  --source /home/ritel/worktrees/brand-assets-phase1-20261031/frontend/public/brand \
  --source-only

# Explicit targets prevent cross-agent product collisions. Run only once the product owner has finished.
python /mnt/d/codex-ws/servers-ws/design/ritel-si/sync_brand.py \
  --source /home/ritel/worktrees/brand-assets-phase1-20261031/frontend/public/brand \
  --target /mnt/d/codex-ws/servers-ws/site-navigation/brand --write
~~~

Omit --write to independently verify that target. Repeat --target to synchronize an agreed set. The historical no-target defaults are site-navigation/brand, ccs-site/brand and subscription-portal/web/brand. Account UI and the console's integrated worktree must be selected explicitly. --refresh-source refreshes only canonical CSS keys/manifest after an intentional source change; it never synchronizes product directories.

## Verification and limits

The complete-font report is tools/font-verification.json: all generated subsets exactly cover their original cmaps; zero missing/extra source code points, zero lost variation sequences, unchanged axes, CSS ranges and SHA-256 verified. Windows and WSL each pass the 8 sync regression cases. Actual temporary-destination synchronization/readback is also exercised separately.

Local Chrome at DPR 2 captured main/tutorial/sub/account for requested zh-CN, zh-HK and en in light/dark (27 initial captures including baseline and dynamic fixture). APIs returned a deliberate local 503; these are asset-overlay screenshots, not authenticated application or translated-account acceptance. The final partition changes retain the same glyphs; the changed Chinese-main workload and dynamic/icon previews were recaptured. Actual effective locale, font requests and failed resources are recorded in the browser JSON.

Final current-Chinese-homepage acceptance: **total 372,532 B**, **fonts 214,418 B**, title presented in its formal I.MingCP face at **2,412.0 / 2,383.7 / 2,393.4 ms** across three independent cold browser contexts. All three satisfy the exact phase-0 gates (decimal MB). Source homepage SHA-256: c8f362592e34a298ca8727178003d3c1b08257acd25c944618c9132502152bef. The data uses local asset overlay/gzip, CDP network emulation at 1.6 Mbps download, 750 Kbps upload, latency 150 ms, no CPU throttling, 1440x1000 DPR 2. Font readiness plus two animation frames is the presentation timestamp; CDP separately verifies the actual title glyphs use custom I.MingCP-Regular. DevTools-only CSS inspection refetches are excluded from initial page accounting. See tools/budget-verification.json and tools/verify_budget.mjs.

priority-codepoints.json contains 127 observed public homepage code points (including both Chinese locale attributes and visible English/static content); this is a download-priority bucket, never a repertoire trim. The complete original cmap/UVS/axes checks remain mandatory, and arbitrary non-priority dynamic characters still resolve from the cold shards. The aggregate font corpus remains about 50 MiB in the recursive runtime bundle, which is distinct from first-load traffic. This local result is not a production measurement or a replay of the integrator's main 32.14 MB / tutorial 40.34 MB baseline.

The measurement serves the canonical vector at existing root /ritel-r.svg as well as /brand paths. The main integrator must install that root alias and enable text compression as represented by the overlay; leaving an old root bitmap SVG is not covered by the passing total. The observed 4G title margin is about 88 ms at worst; real server/network/CPU conditions need final integrated acceptance.

Browser evidence: C:/Users/ritel/AppData/Local/Temp/ritel-brand-budget-fix-20261031/budget.json and main-zh-CN-4g-{1,2,3}.png. The previous broader asset-overlay captures remain under C:/Users/ritel/AppData/Local/Temp/ritel-brand-phase1-20261031/. No production requests or writes were made by this harness. These temporary evidence paths are not deployment payloads.

The main integrator still owns page HTML preload replacement (including frontend/index.html's existing TTF preload), page stylesheet/icon cache keys, root icon link tags, removal of product-local filter:invert(1), account/sub dynamic authenticated states, and the store cross-domain logo URL after release. Do not preload CJK; use font-preloads.json for exact current Latin URLs. An external img cannot inherit the parent's text color; use the adaptive SVG in a surface with the matching CSS color-scheme or choose the explicit light-ink variant for an intentional dark surface. No live cross-domain storefront availability or production acceptance is claimed.

## Rebuild

Use Python with fonttools[woff], brotli, pillow, cairosvg (the build here used an isolated /tmp/ritel-brand-phase1-venv). Run tools/build_fonts.py --jobs 6; tools/build_marks.py --vector-source /mnt/d/codex-ws/servers-ws/outputs/ritel-r-vector-20261031/ritel-r-vector-dark.svg; then sync_brand.py --source <brand> --refresh-source. No download is needed. tools/verify_fonts.py verifies the result. tools/verify_browser.mjs documents its local harness environment variables; QUICK=1 reuses the recorded baseline and checks only the changed Chinese page and dynamic/icon fixture.

Windows helper SHA-256 at handoff:

- sync_brand.py: 8e6a206e038e9dc52b2c1173b199f67b6d38c2a7004be0d761c9b5eeab834985
- test_brand.py: 46d0f61b5a53fdbba0ad6e32f51f0e80da6c77506f23a128389322ba68b45544
