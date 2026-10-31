"""Rebuild complete local font subsets. Python: fonttools[woff], brotli.
Run from anywhere: python tools/build_fonts.py --jobs 6 (TTF sources stay intact).
"""
import argparse
import hashlib
import json
from concurrent.futures import ProcessPoolExecutor
from pathlib import Path
from fontTools import subset
from fontTools.ttLib import TTFont

ROOT = Path(__file__).resolve().parents[1]

def sha(raw):
    return hashlib.sha256(raw).hexdigest()

def ranges(points):
    spans = []
    for cp in sorted(points):
        if spans and cp == spans[-1][1] + 1:
            spans[-1][1] = cp
        else:
            spans.append([cp, cp])
    return ','.join(f'U+{a:X}' if a == b else f'U+{a:X}-{b:X}' for a, b in spans)

def variation_sequences(font):
    return {(selector, cp): name for table in font['cmap'].tables if table.format == 14
            for selector, pairs in table.uvsDict.items() for cp, name in pairs}

def describe(file):
    font = TTFont(file, recalcTimestamp=False)
    points = set(font.getBestCmap())
    variants = variation_sequences(font)
    axes = {a.axisTag: [a.minValue, a.defaultValue, a.maxValue] for a in font['fvar'].axes} if 'fvar' in font else {}
    font.close()
    return points, variants, axes

def partition(points, common=None, size=1024):
    groups = []
    if common:
        ui = points & common
        if ui:
            groups.append(('ui', ui))
        points = points - ui
    # Separate Latin/basic punctuation from all remaining Unicode. Keep every code point.
    if common is None:
        latin = {cp for cp in points if cp <= 0xFF}
        groups.append(('latin', latin))
        points = points - latin
        return groups + [(f'unicode-{block:04X}', {cp for cp in points if cp // 128 == block})
                         for block in sorted({cp // 128 for cp in points})]
    rest = sorted(points)
    groups.extend((f'extended-{i // size:03}', set(rest[i:i+size])) for i in range(0, len(rest), size))
    return groups

def make(task):
    source, dest, points = task
    font = TTFont(source, recalcTimestamp=False)
    options = subset.Options()
    options.layout_features = ['*']
    options.name_IDs = ['*']
    options.name_languages = ['*']
    options.name_legacy = True
    sub = subset.Subsetter(options=options)
    sub.populate(unicodes=set(points) | {selector for selector, _ in variation_sequences(font)})
    sub.subset(font)
    font.flavor = 'woff2'
    font.save(dest)
    actual, variants, axes = describe(dest)
    if actual != set(points):
        raise ValueError(f'Coverage mismatch: {dest}')
    return {'file': str(Path(dest).relative_to(ROOT)), 'sha256': sha(Path(dest).read_bytes()),
            'bytes': Path(dest).stat().st_size, 'codepoints': len(actual), 'unicode_range': ranges(actual),
            'uvs': len(variants), 'axes': axes}


def write_preloads():
    manifest = json.loads((ROOT/'font-subsets.json').read_text(encoding='utf-8'))
    entries = []
    for source in manifest['sources']:
        for file in source['subsets']:
            if not file.endswith('-latin.woff2'):
                continue
            raw = (ROOT/file).read_bytes()
            entries.append({'source':source['source'],'href':'/brand/'+file+'?v='+sha(raw)[:16],
                            'as':'font','type':'font/woff2','crossorigin':'anonymous','bytes':len(raw)})
    (ROOT/'font-preloads.json').write_text(json.dumps({'schema':1,'note':'Select only the Latin faces used above the fold; do not preload CJK shards.','fonts':entries},indent=2)+'\n',encoding='utf-8')

def main(jobs):
    previous_file = ROOT/'font-subsets.json'
    previous = json.loads(previous_file.read_text(encoding='utf-8'))['subsets'] if previous_file.exists() else []
    fonts = ROOT / 'fonts'
    common, _, _ = describe(fonts/'notosanscjkhk/NotoSansCJKhk-ui.woff2')
    specs = [
        ('I.MingCP', 'iming/I.MingCP-8.10.ttf', '400', '', common, 512),
        ('Cinzel', 'cinzel/Cinzel-variable.ttf', '400 900', '', None, 1024),
        ('Cormorant Garamond', 'cormorantgaramond/CormorantGaramond-variable.ttf', '300 700', '', None, 1024),
        ('IBM Plex Sans', 'ibmplexsans/IBMPlexSans-variable.ttf', '100 700', 'font-stretch:85% 100%;', None, 1024),
        ('IBM Plex Mono', 'ibmplexmono/IBMPlexMono-Regular.ttf', '400', '', None, 1024),
        ('IBM Plex Mono', 'ibmplexmono/IBMPlexMono-Medium.ttf', '500', '', None, 1024),
    ]
    # Repartition the existing complete Noto recovery set without needing a new download.
    noto = sorted((fonts/'notosanscjkhk').glob('NotoSansCJKhk-*.woff2'))
    for p in noto:
        specs.append(('Noto Sans CJK HK', str(p.relative_to(fonts)), '100 900', '', None if p.stem.endswith('-ui') else set(), 512))
    tasks, metadata, faces = [], [], []
    for family, filename, weight, stretch, seed, size in specs:
        source = fonts/filename
        points, variants, axes = describe(source)
        # The existing common UI repertoire is already compact; do not duplicate Latin there.
        groups = [('ui', points)] if family == 'Noto Sans CJK HK' and source.stem.endswith('-ui') else partition(points, seed, size)
        rows = []
        output = source.parent/'woff2'
        output.mkdir(exist_ok=True)
        for label, selected in groups:
            if not selected:
                continue
            dest = output / f'{source.stem}-{label}.woff2'
            tasks.append((str(source), str(dest), sorted(selected)))
            rows.append(str(dest.relative_to(ROOT)))
            faces.append((family, weight, stretch, str(dest.relative_to(ROOT)), ranges(selected)))
        metadata.append({'source':str(source.relative_to(ROOT)), 'sha256':sha(source.read_bytes()),
                         'codepoints':len(points), 'uvs':len(variants), 'axes':axes, 'subsets':rows})
    with ProcessPoolExecutor(max_workers=jobs) as pool:
        results = list(pool.map(make, tasks))
    current_files = {r['file'] for r in results}
    for old in previous:
        if old['file'] not in current_files:
            stale = ROOT/old['file']
            if stale.exists():
                if sha(stale.read_bytes()) != old['sha256']:
                    raise ValueError(f'Edited obsolete generated shard: {stale}')
                stale.unlink()
    css = ['/* Complete source repertoires; disjoint ranges per face; generated by tools/build_fonts.py. */']
    for family, weight, stretch, file, unicode_range in faces:
        raw = (ROOT/file).read_bytes()
        css.append(f"@font-face{{font-family:'{family}';src:url('./{file}?v={sha(raw)[:16]}') format('woff2');font-style:normal;font-weight:{weight};{stretch}font-display:swap;unicode-range:{unicode_range}}}")
    (ROOT/'fonts.css').write_text('\n'.join(css)+'\n', encoding='utf-8')
    manifest = {'schema':1, 'generator':'tools/build_fonts.py', 'sources':metadata, 'subsets':results}
    (ROOT/'font-subsets.json').write_text(json.dumps(manifest,ensure_ascii=False,indent=2)+'\n', encoding='utf-8')
    write_preloads()
    print(json.dumps({'subsets':len(results), 'bytes':sum(r['bytes'] for r in results),
                      'largest':max(r['bytes'] for r in results)}))

if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--jobs', type=int, default=6)
    main(parser.parse_args().jobs)
