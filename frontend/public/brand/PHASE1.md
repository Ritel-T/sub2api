# Phase 1 brand asset integration — 2026-10-31

Base: 9b825df094df6da76a3137348c40453cbdb8114a.
Branch: feat/brand-assets-phase1-20261031.
Worktree: /home/ritel/worktrees/brand-assets-phase1-20261031.
Only frontend/public/brand is committed. The original worktree was not edited.
Windows design/ritel-si/sync_brand.py and test_brand.py were edited in place and are outside this Git repository. DESIGN.md was not edited. No product destinations were synchronized and nothing was deployed.

## Content

- Complete I.MingCP cmap (60,017 code points) partitioned into 512-code-point extended shards plus the existing common UI repertoire; all 16 variation sequences retained.
- Cormorant Garamond, Cinzel, IBM Plex Sans/Mono: WOFF2 Latin/basic and Unicode-block extensions, preserving full source repertoires and variable axes. TTF recovery sources stay byte-identical.
- Existing full Noto Sans CJK HK repertoire repartitioned from the 11 old large extension files into 512-code-point chunks. The original WOFF2 recovery set and Chiron fallback stay available.
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

Final Chinese-main **font bytes**: 1,833,212 (1.75 MiB), under the explicit local 2 MiB budget, versus 30,480,952 on the same local page with original assets (94.0% lower). This is not the main integrator's whole-page baseline of main 32.14 MB / tutorial 40.34 MB. Full font families remain available; users typing many rare characters can legitimately trigger more shards. Total runtime closure is about 50 MiB and is not the page transfer size.

Browser evidence: C:/Users/ritel/AppData/Local/Temp/ritel-brand-phase1-20261031/browser.json and browser-final.json, with adjacent screenshots. No production requests or writes were made by this harness. These temporary evidence paths are not deployment payloads.

The main integrator still owns page HTML preload replacement (including frontend/index.html's existing TTF preload), page stylesheet/icon cache keys, root icon link tags, removal of product-local filter:invert(1), account/sub dynamic authenticated states, and the store cross-domain logo URL after release. Do not preload CJK; use font-preloads.json for exact current Latin URLs. An external img cannot inherit the parent's text color; use the adaptive SVG in a surface with the matching CSS color-scheme or choose the explicit light-ink variant for an intentional dark surface. No live cross-domain storefront availability or production acceptance is claimed.

## Rebuild

Use Python with fonttools[woff], brotli, pillow, cairosvg (the build here used an isolated /tmp/ritel-brand-phase1-venv). Run tools/build_fonts.py --jobs 6; tools/build_marks.py --vector-source /mnt/d/codex-ws/servers-ws/outputs/ritel-r-vector-20261031/ritel-r-vector-dark.svg; then sync_brand.py --source <brand> --refresh-source. No download is needed. tools/verify_fonts.py verifies the result. tools/verify_browser.mjs documents its local harness environment variables; QUICK=1 reuses the recorded baseline and checks only the changed Chinese page and dynamic/icon fixture.

Windows helper SHA-256 at handoff:

- sync_brand.py: fca00db05c00cbbd460c1a5a54eeda9bd53404679338ed9b6bac61b4c4171623
- test_brand.py: 3e965b0169becfd63b900676b6f3364026739e751bcd98d1376359e9987dff32
