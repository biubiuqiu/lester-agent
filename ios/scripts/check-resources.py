"""Check app resources without pretending to build Apple's SDK on Linux."""
import json
import plistlib
import re
import struct
from pathlib import Path
root = Path(__file__).resolve().parents[1]
resources = root / 'Lester/Resources'
locales = ('en', 'zh-Hans', 'ja', 'ko', 'fr', 'es')
keys = None
for locale in locales:
    text = (resources / f'{locale}.lproj/Localizable.strings').read_text()
    pairs = re.findall(r'^("(?:[^"\\]|\\.)*") = ("(?:[^"\\]|\\.)*");$', text, re.M)
    values = {json.loads(k): json.loads(v) for k, v in pairs}
    assert values and len(values) == len(pairs), f'{locale}: invalid or duplicate keys'
    assert all(v.strip() for v in values.values()), f'{locale}: empty translations'
    if keys is None: keys = set(values)
    assert set(values) == keys, f'{locale}: missing native translations'
    purpose = (resources / f'{locale}.lproj/InfoPlist.strings').read_text()
    assert re.search(r'^"NSMicrophoneUsageDescription" = ".+";', purpose, re.M), f'{locale}: missing microphone purpose'
for name in ('Info.plist', 'Info-Debug.plist'):
    with (root / 'Lester' / name).open('rb') as stream: info = plistlib.load(stream)
    assert info['CFBundleURLTypes'][0]['CFBundleURLSchemes'] == ['lester']
    assert 'NSAllowsArbitraryLoads' not in repr(info)
    assert info.get('NSMicrophoneUsageDescription', '').strip()
    assert len(info['UISupportedInterfaceOrientations~ipad']) == 4
    if name == 'Info.plist': assert 'NSAppTransportSecurity' not in info
with (resources / 'PrivacyInfo.xcprivacy').open('rb') as stream:
    assert plistlib.load(stream)['NSPrivacyTracking'] is False
project = (root / 'Lester.xcodeproj/project.pbxproj').read_text()
for source in (root / 'Lester').glob('*.swift'): assert source.name in project, f'unbuilt source {source}'
icon = resources / 'Assets.xcassets/AppIcon.appiconset/AppIcon.png'
content = icon.read_bytes()
assert content[:8] == b'\x89PNG\r\n\x1a\n' and struct.unpack('>II', content[16:24]) == (1024, 1024)
assert content[25] == 2, 'App icon must be opaque RGB'
print(f'Validated {len(locales)} native locales, URL scheme, ATS policies, orientations, privacy manifest and icon.')
