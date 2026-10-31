"""Verify complete font repertoires, CSS ranges, variation sequences, axes and hashes."""
import argparse
import hashlib
import json
import re
from pathlib import Path
from fontTools.ttLib import TTFont
from build_fonts import ROOT, describe, variation_sequences

def expand(value):
    points = set()
    for part in value.split(','):
        ends = part.strip()[2:].split('-')
        a, b = int(ends[0],16), int(ends[-1],16)
        points.update(range(a,b+1))
    return points

def verify():
    data = json.loads((ROOT/'font-subsets.json').read_text())
    rows = {r['file']:r for r in data['subsets']}
    families = {}
    for source in data['sources']:
        original = ROOT/source['source']
        assert hashlib.sha256(original.read_bytes()).hexdigest() == source['sha256'], original
        expected, expected_uvs, axes = describe(original)
        actual, actual_uvs = set(), {}
        for filename in source['subsets']:
            file = ROOT/filename
            row = rows[filename]
            assert hashlib.sha256(file.read_bytes()).hexdigest() == row['sha256'], file
            points, variants, subset_axes = describe(file)
            assert points == expand(row['unicode_range']), file
            assert not actual.intersection(points), file
            actual.update(points)
            assert not actual_uvs.keys() & variants.keys(), file
            actual_uvs.update(variants)
            assert axes == subset_axes, (file, axes, subset_axes)
        assert expected == actual, (original,'lost or extra Unicode code points')
        assert expected_uvs.keys() == actual_uvs.keys(), (original,'lost variation sequences')
        assert {k:v is None for k,v in expected_uvs.items()} == {k:v is None for k,v in actual_uvs.items()}, original
        families[source['source']] = {'codepoints':len(actual),'variation_sequences':len(actual_uvs),'subsets':len(source['subsets']),'axes':axes}
    css = (ROOT/'fonts.css').read_text()
    seen = set()
    for face in re.findall(r'@font-face\{([^}]+)\}',css):
        filename = re.search(r"src:url\('\./([^?']+)",face)[1]
        declared = re.search(r'unicode-range:([^}]+)$',face)[1]
        assert expand(declared) == expand(rows[filename]['unicode_range']), filename
        assert 'font-display:swap' in face and "format('woff2')" in face, filename
        seen.add(filename)
    assert seen == set(rows), 'Every generated shard must be reachable from CSS'
    brand = (ROOT/'brand.css').read_text()
    assert '.ttf' not in brand+css, 'Runtime CSS must not reference the recovery TTF files'
    result = {'ok':True,'sources':families,'subsets':len(rows),'total_subset_bytes':sum(r['bytes'] for r in rows.values()),
              'largest_subset_bytes':max(r['bytes'] for r in rows.values()),'css_font_display':'swap',
              'source_characters_lost':0,'variation_sequences_lost':0,'axes_changed':0}
    return result

if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--report',type=Path)
    args = parser.parse_args()
    result = verify()
    if args.report:
        args.report.write_text(json.dumps(result,indent=2)+'\n',encoding='utf-8')
    print(json.dumps(result,indent=2))
