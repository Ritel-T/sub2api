"""Build public marks only from the approved 2026-10-31 R path and Cinzel paths."""
import argparse
import hashlib
import json
from pathlib import Path
import xml.etree.ElementTree as ET
import cairosvg

ROOT = Path(__file__).resolve().parents[1]
APPROVED_SHA = '66ac770ac5b1a3c4becfc8797c9963582672021ecdd2a6373939925d04a70642'
STYLE = '<style>:root{color:#27251f}@media(prefers-color-scheme:dark){:root{color:#eeece5}}</style>'

def main(source):
    raw = source.read_bytes()
    if hashlib.sha256(raw).hexdigest() != APPROVED_SHA:
        raise ValueError('Expected the approved ritel-r-vector-dark.svg, byte for byte')
    approved = ET.fromstring(raw)
    paths = approved.findall('{http://www.w3.org/2000/svg}path')
    if len(paths) != 1:
        raise ValueError('Expected one vector path')
    d = paths[0].attrib['d']
    assets = ROOT/'assets'
    def svg(body, box='0 0 860 680', adaptive=True, label='Ritel.SI'):
        return f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="{box}" role="img" aria-label="{label}">{STYLE if adaptive else ""}{body}</svg>\n'
    shape = f'<path fill="currentColor" d="{d}"/>'
    outputs = {'ritel-r.svg':svg(shape),
               'ritel-r-dark.svg':svg(f'<path fill="#eeece5" d="{d}"/>',adaptive=False),
               'ritel-favicon.svg':svg(shape,box='0 -90 860 860')}
    # Existing wordmark is already pure Cinzel outlines, no font or bitmap dependency.
    wordmark = (assets/'ritel-wordmark-cinzel.svg').read_text(encoding='utf-8')
    if '<style>' not in wordmark:
        wordmark = wordmark.replace('><g', '>'+STYLE+'<g',1)
    outputs['ritel-wordmark-cinzel.svg'] = wordmark
    wordroot = ET.fromstring(wordmark)
    group = wordroot.find('{http://www.w3.org/2000/svg}g')
    groupxml = ET.tostring(group,encoding='unicode').replace('ns0:','').replace(':ns0','')
    outputs['ritel-si.svg'] = svg(f'<g transform="translate(0 90) scale(.8)">{shape}</g><g transform="translate(800 0)">{groupxml}</g>',box='0 0 4465 860')
    for name, text in outputs.items():
        (assets/name).write_text(text,encoding='utf-8')
    for size in (16,32):
        cairosvg.svg2png(bytestring=svg(f'<path fill="#27251f" d="{d}"/>',box='0 -90 860 860',adaptive=False).encode(),
                        write_to=str(assets/f'favicon-{size}.png'),output_width=size,output_height=size)
    # Opaque light background provides a legible home-screen icon in either OS theme.
    apple = svg(f'<rect x="0" y="-90" width="860" height="860" fill="#f5f3ee"/><g transform="translate(86 68) scale(.8)"><path fill="#27251f" d="{d}"/></g>',box='0 -90 860 860',adaptive=False)
    cairosvg.svg2png(bytestring=apple.encode(),write_to=str(assets/'apple-touch-icon.png'),output_width=180,output_height=180)
    for name in [*outputs, 'apple-touch-icon.png', 'favicon-16.png', 'favicon-32.png']:
        (ROOT/name).write_bytes((assets/name).read_bytes())
    print(json.dumps({'approved_source_sha256':APPROVED_SHA,'svg_files':list(outputs),'r_path_unchanged':True}))

if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--vector-source',type=Path,required=True)
    main(parser.parse_args().vector_source)
