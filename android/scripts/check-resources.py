"""Check complete native copy and the Release application's transport/backup boundaries."""
from pathlib import Path
import re,hashlib,xml.etree.ElementTree as E
root=Path(__file__).resolve().parents[1]
res=root/'app/src/main/res'
base={node.attrib['name']:node.text for node in E.parse(res/'values/strings.xml').getroot()}
for folder in ('values-zh-rCN','values-ja','values-ko','values-fr','values-es'):
    values={node.attrib['name']:node.text for node in E.parse(res/folder/'strings.xml').getroot()}
    assert values.keys()==base.keys(), f'{folder}: incomplete translations'
    assert all(value and value.strip('" ') for value in values.values()), f'{folder}: empty translation'
for path in (root/'app/src/main/java').rglob('*.kt'):
    for key in re.findall(r'R\.string\.(\w+)',path.read_text()): assert key in base, f'{path}: missing {key}'
ns='{http://schemas.android.com/apk/res/android}'
manifest=E.parse(root/'app/src/main/AndroidManifest.xml').getroot()
assert {p.attrib[ns+'name'] for p in manifest.findall('uses-permission')}=={'android.permission.INTERNET','android.permission.VIBRATE','android.permission.RECORD_AUDIO'}
assert any(f.attrib.get(ns+'name')=='android.hardware.microphone' and f.attrib.get(ns+'required')=='false' for f in manifest.findall('uses-feature'))
application=manifest.find('application');assert application.attrib[ns+'allowBackup']=='false'
release=E.parse(res/'xml/network_security_config.xml').getroot()
assert release.find('base-config').attrib['cleartextTrafficPermitted']=='false'
assert not release.findall('domain-config')
assert [c.attrib['src'] for c in release.findall('.//certificates')]==['system']
for path in (root/'app/src').rglob('*.xml'):E.parse(path)
print(f'Validated six Android locales ({len(base)} strings), manifest, Release HTTPS and backup restrictions.')

assert hashlib.sha256((root/"gradle/wrapper/gradle-wrapper.jar").read_bytes()).hexdigest()=="7d3a4ac4de1c32b59bc6a4eb8ecb8e612ccd0cf1ae1e99f66902da64df296172", "Gradle wrapper differs from the official release"
