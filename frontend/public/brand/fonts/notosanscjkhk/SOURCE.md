# Noto Sans CJK HK

Noto Sans CJK HK 2.004, from the official full language-specific variable font.
This is the full CJK font with Hong Kong default glyphs, rather than the
Hong Kong region subset. Simplified and Traditional Chinese are both covered.
The original SIL Open Font License is included in OFL.txt, and the original
font names and copyright metadata remain in each font file.

- Upstream: https://github.com/notofonts/noto-cjk
- Commit: f8d157532fbfaeda587e826d4cd5b21a49186f7c
- Source: https://raw.githubusercontent.com/notofonts/noto-cjk/f8d157532fbfaeda587e826d4cd5b21a49186f7c/Sans/Variable/TTF/NotoSansCJKhk-VF.ttf
- Original source SHA-256: c485d72ca089ce65a83c483c1ddbf1791828b4c8e3c45ea5eae8063fe2164495

The font is partitioned into WOFF2 files with FontTools 4.67.0 and Brotli 1.2.0.
The UI partition contains the Han characters used by both Chinese locales plus
CJK punctuation and fullwidth forms. The other 11 partitions contain up to
4,096 code points each. CSS unicode-range loads only the partitions needed by
the actual page text. No JavaScript loading or Chinese font preload is needed.

The partitions are disjoint and their union preserves all 44,810 code points
from the source. All 30 Unicode variation sequences, OpenType layout features,
and the 100-900 weight axis are retained. Both Chinese locale dictionaries have
zero missing Han characters. Unicode partitions change download granularity;
they do not narrow the character repertoire.
