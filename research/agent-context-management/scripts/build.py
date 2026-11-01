#!/usr/bin/env python3
"""Rebuild the frozen, self-contained research report using the standard library.

Default builds validate the bundled research data, without reading or downloading
third-party code. Pass --source-root to additionally verify existing checkouts.
"""

import argparse
import html
import json
from pathlib import Path, PurePosixPath
import re
import subprocess
import sys
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[1]
DATA = ROOT / 'data'
NOTES = ROOT / 'notes'
ASSETS = ROOT / 'assets'
ORDER = ['claude', 'codex', 'opencode', 'hermes', 'pi']
REPOS = {
    'claude': 'alex000kim/claude-code',
    'codex': 'openai/codex',
    'opencode': 'anomalyco/opencode',
    'hermes': 'NousResearch/hermes-agent',
    'pi': 'badlogic/pi-mono',
}
SNAPSHOTS = {
    'claude': '6aa7e7f8789014d42247a0545ab4e9d4e531b0f2',
    'codex': '61f34c35af77777db4d77d0ea2aeb8547a6b4523',
    'opencode': 'f7d5b6ffc48223e654b20043a500d315e4bc8053',
    'hermes': '944c3a434071f43e64b443d033f3eba30c64434c',
    'pi': 'b9fb2b854b7a19b28a68c3421365fee0a5342ce6',
}
DIRS = {'claude': 'claude-full', **{key: key for key in ORDER[1:]}}


def read_utf8(path):
    return path.read_text(encoding='utf-8')


def load_json(name):
    return json.loads(read_utf8(DATA / name))


def source_path(value):
    if not isinstance(value, str) or not value or any(c in value for c in ('\\', '\x00', '?', '#')):
        raise ValueError(f'invalid source path: {value!r}')
    path = PurePosixPath(value)
    if path.is_absolute() or '..' in path.parts or str(path) != value:
        raise ValueError(f'source path must be a normalized relative path: {value!r}')
    return value


def line_number(value):
    if type(value) is not int or value < 1:
        raise ValueError(f'invalid 1-based source line: {value!r}')
    return value


def source_url(key, url):
    """Validate immutable GitHub links; preserve legacy single-start anchors."""
    if not isinstance(url, str):
        raise ValueError(f'{key}: source URL must be a string')
    parsed = urlsplit(url)
    prefix = f'/{REPOS[key]}/blob/{SNAPSHOTS[key]}/'
    if parsed.scheme != 'https' or parsed.netloc != 'github.com' or parsed.query or not parsed.path.startswith(prefix):
        raise ValueError(f'{key}: source URL must use the frozen repository and commit: {url}')
    path = source_path(parsed.path[len(prefix):])
    anchor = re.fullmatch(r'L([1-9][0-9]*)(?:-L([1-9][0-9]*))?', parsed.fragment)
    if not anchor:
        raise ValueError(f'{key}: source URL needs a valid line anchor: {url}')
    start = int(anchor[1])
    end = int(anchor[2] or anchor[1])
    if end < start:
        raise ValueError(f'{key}: reversed source URL line range: {url}')
    return path, start, end, anchor[2] is not None


def collect_source_links(key, value, locations):
    if isinstance(value, dict):
        for item in value.values():
            collect_source_links(key, item, locations)
    elif isinstance(value, list):
        for item in value:
            collect_source_links(key, item, locations)
    elif isinstance(value, str):
        for url in re.findall(r'https://github\.com/[^\s<>()"\']+/blob/[^\s<>()"\']+', value):
            path, start, end, _ = source_url(key, url)
            locations[key].add((path, start, end))


def validate_data(data):
    evidence = {}
    locations = {key: set() for key in ORDER}
    for key, item in data.items():
        repository = item['repository'].removeprefix('https://github.com/').removesuffix('/')
        commit = item['commit']
        if item['id'] != key or repository != REPOS[key]:
            raise ValueError(f'{key}: framework ID or repository does not match the snapshot')
        if not isinstance(commit, str) or not re.fullmatch(r'[0-9a-f]{40}', commit) or commit != SNAPSHOTS[key]:
            raise ValueError(f'{key}: commit differs from the frozen 2026-10-22 snapshot')
        ids = [rule['id'] for rule in item['rules']]
        if len(ids) != len(set(ids)):
            raise ValueError(f'{key}: duplicate rule ID')
        for rule in item['rules']:
            if not rule.get('evidence'):
                raise ValueError(f'{key}:{rule["id"]}: rule has no source evidence')
            for entry in rule['evidence']:
                path = source_path(entry['path'])
                start = line_number(entry['line'])
                end = line_number(entry.get('end', start))
                if end < start:
                    raise ValueError(f'{key}:{path}: reversed source range {start}-{end}')
                url_path, url_start, url_end, explicit_end = source_url(key, entry['url'])
                if (url_path, url_start) != (path, start) or (explicit_end and url_end != end):
                    raise ValueError(f'{key}:{rule["id"]}: evidence fields disagree with its URL')
                locations[key].add((path, start, end))
                # The original report canonicalized rule URLs, but kept notes and scenes unchanged.
                entry['url'] = f'https://github.com/{REPOS[key]}/blob/{commit}/{path}#L{start}' + (f'-L{end}' if end > start else '')
                evidence[(key, path, start, end)] = {**entry, 'framework': item['name']}
        known_ids = set(ids)
        for phase in item['pipeline']:
            if not set(phase.get('ruleIds', [])).issubset(known_ids):
                raise ValueError(f'{key}: pipeline references an unknown rule')
        collect_source_links(key, item, locations)
    if sum(len(item['rules']) for item in data.values()) != 116 or len(evidence) != 320:
        raise ValueError('frozen report must contain 116 rules and 320 unique evidence locations')
    return evidence, locations


def validate_scenes(frameworks, locations):
    if [item['id'] for item in frameworks] != ORDER:
        raise ValueError('scenes.json must use the five frameworks in report order')
    count = 0
    for framework in frameworks:
        key = framework['id']
        ids = [scene['id'] for scene in framework['scenarios']]
        if len(ids) != len(set(ids)):
            raise ValueError(f'{key}: duplicate scene ID')
        count += len(ids)
        for scene in framework['scenarios']:
            if not scene['steps']:
                raise ValueError(f'{key}:{scene["id"]}: empty scene')
            for step in scene['steps']:
                if not step.get('sources'):
                    raise ValueError(f'{key}:{scene["id"]}: step has no source evidence')
                for entry in step['sources']:
                    path, start, end, _ = source_url(key, entry['url'])
                    locations[key].add((path, start, end))
    if count != 21:
        raise ValueError('frozen report must contain 21 animation scenes')


def validate_sources(source_root, data, locations):
    """Optionally verify existing Git checkouts and actual referenced file ranges."""
    for key in ORDER:
        checkout = (source_root / DIRS[key]).resolve()
        if not checkout.is_dir():
            raise ValueError(f'{key}: missing source checkout: {checkout}')
        result = subprocess.run(['git', '-C', str(checkout), 'rev-parse', 'HEAD'], capture_output=True, text=True, check=False)
        if result.returncode or result.stdout.strip() != data[key]['commit']:
            raise ValueError(f'{key}: source checkout HEAD must equal {data[key]["commit"]}')
        paths = sorted({path for path, _, _ in locations[key]})
        # Dirty cited files would make a commit-only check misleading. Do not execute diff helpers.
        result = subprocess.run(['git', '-C', str(checkout), 'diff', '--no-ext-diff', '--quiet', 'HEAD', '--', *paths], capture_output=True, check=False)
        if result.returncode:
            raise ValueError(f'{key}: cited files differ from the frozen checkout (or git diff failed)')
        line_counts = {}
        for path, start, end in sorted(locations[key]):
            file = (checkout / path).resolve()
            if not file.is_relative_to(checkout) or not file.is_file():
                raise ValueError(f'{key}: missing or out-of-checkout source: {path}')
            if path not in line_counts:
                line_counts[path] = len(read_utf8(file).splitlines())
            if end > line_counts[path]:
                raise ValueError(f'{key}:{path}:{start}-{end}: range exceeds {line_counts[path]} lines')


H=lambda s:html.escape(str(s),quote=True)
def text(v):
    if v is None:return '未单独规定'
    if isinstance(v,list):return '；'.join(text(x) for x in v)
    if isinstance(v,dict):return '；'.join(f'{k}: {text(x)}' for k,x in v.items())
    return str(v)
def rich(v):
    s=H(text(v))
    s=re.sub(r'`([^`]+)`',r'<code>\1</code>',s)
    s=re.sub(r'\*\*([^*]+)\*\*',r'<strong>\1</strong>',s)
    return s
def md(raw):
    out=[];para=[];code=[];table=[];fenced=False;listing=None
    def inline(s):
        s=rich(s)
        return re.sub(r'\[([^\]]+)\]\((https?://[^\s)]+)\)',r'<a href="\2" target="_blank" rel="noopener">\1</a>',s)
    def flush():
        if para:out.append('<p>'+inline(' '.join(para))+'</p>');para.clear()
    def close():
        nonlocal listing
        if listing:out.append('</'+listing+'>');listing=None
    def flush_table():
        if not table:return
        rows=[[x.strip() for x in row.strip().strip('|').split('|')] for row in table]
        if len(rows)>1 and all(re.fullmatch(r':?-{2,}:?',x.replace(' ','')) for x in rows[1]):
            out.append('<div class="table-wrap"><table><thead><tr>'+''.join('<th>'+inline(x)+'</th>' for x in rows[0])+'</tr></thead><tbody>'+''.join('<tr>'+''.join('<td>'+inline(x)+'</td>' for x in row)+'</tr>' for row in rows[2:])+'</tbody></table></div>')
        else:out.append('<pre>'+H('\n'.join(table))+'</pre>')
        table.clear()
    for line in raw.splitlines():
        if line.startswith('```'):
            flush();close()
            if fenced:out.append('<pre><code>'+H('\n'.join(code))+'</code></pre>');code=[]
            fenced=not fenced;continue
        if fenced:code.append(line);continue
        if line.lstrip().startswith('|'):
            flush();close();table.append(line);continue
        flush_table()
        m=re.match(r'^(#{1,6})\s+(.*)',line)
        if m:
            flush();close();level=min(6,len(m[1])+1);out.append(f'<h{level}>{inline(m[2])}</h{level}>');continue
        m=re.match(r'^\s*(?:([-*])|\d+[.)])\s+(.*)',line)
        if m:
            flush();tag='ul' if m[1] else 'ol'
            if listing!=tag:close();listing=tag;out.append('<'+tag+'>')
            out.append('<li>'+inline(m[2])+'</li>');continue
        if not line.strip():flush();close();continue
        if line.startswith('> '):flush();close();out.append('<blockquote>'+inline(line[2:])+'</blockquote>');continue
        para.append(line)
    flush();close();flush_table()
    if code:out.append('<pre>'+H('\n'.join(code))+'</pre>')
    return '\n'.join(out)


def render_report(source_root=None):
    data = {key: load_json(f'{key}.json') for key in ORDER}
    learning = load_json('framework-learning.json')
    evidence, source_locations = validate_data(data)
    for key in ORDER:
        collect_source_links(key, read_utf8(NOTES / f'{key}.md'), source_locations)
    if set(learning) != set(ORDER):
        raise ValueError('framework-learning.json must contain exactly the five frameworks')

    def refs(es):
        return '<div class="source-refs">'+''.join(f'<a href="{H(e["url"])}" target="_blank" rel="noopener">{H(e.get("symbol") or e["path"].split("/")[-1])} <code>L{e["line"]}–{e.get("end",e["line"])}</code></a>' for e in es)+'</div>'
    def rule_html(key,r):
        fields=[('触发条件','condition'),('执行动作','action'),('保留对象','preserve'),('信息代价','loss'),('默认 / 参数','default'),('门控 / 作用域','gate'),('证据边界','certainty')]
        body=''.join(f'<dt>{a}</dt><dd>{rich(r.get(b))}</dd>' for a,b in fields)
        search=' '.join(text(r.get(k,'')) for k in ['id','title','condition','action','preserve','loss','default','gate','certainty'])+' '+ ' '.join(e.get('symbol','') for e in r.get('evidence',[]))
        return f'<details class="rule" id="rule-{key}-{H(r["id"])}" data-search="{H(search)}"><summary><span class="rule-id">{H(r["id"])}</span>{H(r["title"])}</summary><dl class="rule-body">{body}</dl>{refs(r.get("evidence",[]))}</details>'

    abstract='''
<div class="lead"><strong>上下文管理，是在证据、容量、连续性与成本之间，维护一个可重建的工作集。</strong><p>先解释对象和操作，再看动画中的数据变化，最后沿函数和规则追到源码。这里的“分层”是职责模型；它不意味着模型里存在五段独立、互不计费的记忆。</p></div>
<div class="objects">
 <div class="object"><div class="symbol">P</div><b>当前规则与能力</b><p>system / developer 指令、仓库规则、工具定义、Skills。拥有独立来源与更新周期。</p></div>
 <div class="object"><div class="symbol">H</div><b>持久历史与证据</b><p>消息、事件、工具结果、分支、外置大输出。保存下来，不等于每轮全发。</p></div>
 <div class="object"><div class="symbol">V</div><b>活动工作集</b><p>由历史、checkpoint、保留边界和投影规则重建的模型可见内容。</p></div>
 <div class="object"><div class="symbol">R</div><b>本轮 wire request</b><p>再经过 provider 转换、schema、媒体、输出预算与 hook，才是真正发送包。</p></div>
 <div class="object"><div class="symbol">C</div><b>Provider 缓存</b><p>复用前缀计算，减少重复成本。缓存里的输入仍可能占上下文容量。</p></div>
</div>
<p><strong>来源、消息角色与授权要分开：</strong>仓库文件决定作用域；发送时可能成为 system section、developer/user fragment 或 meta reminder；真正的工具权限还由运行时单独执行。文件叫“规则”，不表示它在所有框架里都具有相同 role 或优先级。下方档案分别追踪这三层。</p>
<div class="formal"><div><h3>一组最小的形式化关系</h3><pre>Vₜ = Project(Hₜ, checkpointₜ, keep-boundaryₜ)
Rₜ = Adapt(provider, Assemble(Pₜ, Vₜ, new-inputₜ))
τ̂ₜ = Anchor(usage) + Estimate(new-delta)

P / H / V / R / C 是职责对象，不是所有项目的类名。
某些实现没有 anchor，或把原生压缩项交给 provider。</pre></div><div><h3>为什么只算聊天字数会失效？</h3><p>规则和 schema 本身会占空间；图片、音频、thinking / opaque 项有独立计量；上次 usage 只对应当时的请求。新增工具结果、编辑历史、换模型、换规则或压缩后，旧数字可能不再适用。</p><p>输出预留、压缩触发线、近期保留目标、摘要输出限制是<strong>四个不同预算</strong>。它们不能都画成同一条“剩余 token”线。</p></div></div>
<h3>六类操作：不要把它们都叫压缩</h3>
<div class="operators">
 <div class="operator"><b>BOUND</b><h3>入口限量</h3><p>工具输出先限行 / 字节；大内容外置。损失可能在进入历史之前就发生。</p></div>
 <div class="operator"><b>PROJECT / PRUNE</b><h3>投影与确定性裁剪</h3><p>省略旧结果、去重、替换 stub、排除失败尝试。可不调用模型，但并非总是无损。</p></div>
 <div class="operator"><b>MEASURE</b><h3>观测与估算</h3><p>provider usage、绑定条件、delta、有效窗口。结果必须携带来源与失效条件。</p></div>
 <div class="operator"><b>SUMMARIZE / NATIVE</b><h3>语义摘要或原生压缩</h3><p>LLM 概括历史，或接收 provider replacement window。客户端未必知道内部编码。</p></div>
 <div class="operator"><b>CHECKPOINT / REHYDRATE</b><h3>提交边界与重新装配</h3><p>写摘要和保留 ID，重放当前 system / tools、文件或状态。重新注入也会耗预算。</p></div>
 <div class="operator"><b>RECOVER / FENCE</b><h3>有界恢复与一致性保护</h3><p>超限后缩小、重试、冷却或停止；防止并发旧摘要覆盖新历史、工具副作用重复执行。</p></div>
</div>
<p class="caption">这是本报告提出的统一分析模型。每个框架只映射自己实际具有的操作；图形相同不代表算法相同。</p>
'''

    invariants='''
<div class="state-strip"><span>装配 / 同步规则</span><i>→</i><span>重建工作集</span><i>→</i><span>观测压力</span><i>→</i><span>选择减量路径</span><i>→</i><span>校验 / 提交</span><i>→</i><span>重建并继续</span></div>
<div class="invariants">
<div><b>工具调用与结果是原子关系</b><p>切在 result 中间、只删 call、留下孤立 output 都可能使协议或语义失效。各实现通过边界对齐、归一化或 context_edit 处理；并非全部采用同一方法。</p></div>
<div><b>丢失必须定位到具体阶段</b><p>入口截断、摘要输入截短、摘要遗漏、近期文本化、恢复预算不足都能丢信息。“原始历史还在”只能证明可查，不证明本轮可见。</p></div>
<div><b>摘要不是授权或事实存储</b><p>“已读文件”“测试成功”“用户批准”必须回到原始记录和平台状态；摘要可提供线索，不能重新授予权限或恢复被撤销状态。</p></div>
<div><b>提交必须绑定所总结的那份历史</b><p>异步候选与并发压缩要验证版本 / watermark / lease。结果写入成功后，工作集和计量基线也要一致更新。</p></div>
<div><b>不能把可选路径画成默认流水线</b><p>Claude 微压缩依赖开关；Codex 的元数据可影响 TokenBudget；OpenCode native 依赖能力；Hermes 微压缩另有 cadence；Pi hooks 可以替换摘要。</p></div>
<div><b>阈值不是绝对不超限的证明</b><p>新工具结果会跳增，估算会偏差，保护集合可过大。完整请求边界检查、有界恢复与明确失败，才构成可持续运行的策略。</p></div>
</div>
<h3>四笔预算账</h3><div class="table-wrap"><table><thead><tr><th>预算</th><th>回答什么问题</th><th>常见误读</th></tr></thead><tbody>
<tr><th>模型 / 输入窗口</th><td>provider 实际接受多大的请求与输出？</td><td>把本地 policy / fallback window 当模型能力</td></tr>
<tr><th>压缩触发线</th><td>什么时候值得提前回收？</td><td>把 90% / reserve 当所有分支的最终硬限制</td></tr>
<tr><th>尾部保留目标</th><td>近期哪些内容保持可直接继续？</td><td>把 keepRecentTokens 当整个压缩后请求上限</td></tr>
<tr><th>摘要 / 下一响应输出</th><td>总结过程和下一轮各自留多少生成空间？</td><td>把触发用 reserveTokens 当摘要的 maxTokens</td></tr>
</tbody></table></div>
'''

    matrix_rows=[
    ('规则生命周期','CLAUDE.md 独立发现；compact 后恢复附件 / cache 状态','world-state fragments；replace / remove / diff','instruction baseline + system delta；compact 推进 epoch','分块缓存 system / memory；压缩边界重建','system sections diff + toolsAdded / Removed；checkpoint 快照当前态'),
    ('历史与模型输入','原对话与压缩边界；API editing 可与本地正文不同','rollout / history / replacement window 分开','DB 事件原历史；每个 durable Step 重建','active rows + archive / external history；不同回收采用不同提交','JSONL 树 + projection + context_edit；原记录仍可导出'),
    ('计量绑定','最后 API usage + 后续消息估算，分支会修正','最后实测 total + 后续 model item delta；非 lifetime 花费','同 provider usage anchor + delta；native 重放另要求 exact model','provider-priced prefix fingerprint + delta；部分无 anchor gate 等一次','最新有效 usage + chars/4；projection edit / compaction 使旧 usage 失效'),
    ('触发机制','W − min(maxOutput,20K) − 13K，支持下调','元数据90%基础 + Total / BodyAfterPrefix + 完整窗口','默认 W−max(15%,16K)，小窗口例外 / 显式buffer','50%基础；<512K至少75%；扣输出、floor、cap 与路由修正','contextTokens > W−16,384；按模型 override'),
    ('轻量回收','time-based / cached MC 门控独立','工具输出预算、归一化；remote 尾部保护裁剪','工具入口 2000行/50KiB；recent 再截1250 codepoints','no-LLM prune 与 LLM rolling micro 是两种机制','工具入口截断；context_edit 省略 / 替换与摘要分开'),
    ('文本摘要','full fork；可先采用已有 session memory','unsupported remote 时走 local 编排 LLM','checkpoint summary，尽量送完整前缀复用 cache','head + structured summary + token-selected lean / legacy tail','旧前缀 + split-span prefix；summary 与 kept entry 重建'),
    ('近期保留','full 与 SM 规则不同；恢复附件有独立配额','local user预算20K；remote真实user等另有64K预算','keep15K是选择目标；recent 是文本，不是原消息','lean2.5%窗口/10K–25K，再受share与anchor约束','keep20K目标；不切tool result；大用户跨度可分割'),
    ('原生 / 替代路径','API context management 与内部开关；镜像不完整','remote V2 / metadata可能开启TokenBudget新窗口','native endpoint / trigger；exact provider+model绑定','native / app-server由真正上下文owner压缩','默认coding-agent摘要；extension可取消/替换'),
    ('超限恢复','full摘要请求可重试缩输入；auto失败3次熔断','local删最早配对项；remote按尾工具stub及保护边界','主请求无durable输出才一次恢复；摘要输入70/50/35%阶梯','cooldown、provider-proof bypass、no-progress breaker / 分类fallback','omission持久化→压缩→新run；一次compact-and-retry'),
    ('并发 / 持久提交','主线程与fork隔离，恢复缓存状态；生产开关未知','async候选generation、model/auth pin；安装检查live suffix','DB projector + compaction events + instruction epoch','lease、watermark、covered rows/CAS 与archive提交','append-only条目与当前分支重建；队列/生命周期序列化'),
    ('不可据此保证','full消息估算不含所有prefix；无总量强制后验保证','服务端压缩算法不可由客户端源码证明','summary只验至少一个heading，非严格完整schema','fallback可损失中段；archive恢复要真正检索','summary并非无损；provider输出clamp与压缩预算不同'),
    ]

    def matrix():
        return '<div class="table-wrap"><table><caption>统一职责下的实现差异；具体条件与证据见对应档案</caption><thead><tr><th>维度</th>'+''.join(f'<th>{data[k]["name"]}</th>' for k in ORDER)+'</tr></thead><tbody>'+''.join('<tr><th>'+a+'</th>'+''.join('<td>'+H(v)+'</td>' for v in values)+'</tr>' for a,*values in matrix_rows)+'</tbody></table></div>'

    def dossier(key,d):
        ls=''.join('<tr>'+''.join('<td>'+rich(l.get(field))+'</td>' for field in ['name','source','lifecycle','budget'])+'</tr>' for l in d['layers'])
        pipeline=''.join(f'<li><b>{H(p["phase"])}</b><code>{H(text(p.get("functions")))}</code><div class="io">输入：{rich(p.get("inputs"))}<br>输出：{rich(p.get("outputs"))}</div><div class="source-refs">对应规则：'+ ' · '.join(f'<a href="#rule-{key}-{H(r)}" class="open-rule">{H(r)}</a>' for r in p.get('ruleIds',[]))+'</div></li>' for p in d['pipeline'])
        counts=len(d['rules'])
        corrections=d.get('corrections',[])
        takeaway=''.join(f'<div><small>{H(t["label"])}</small><b>{H(t["title"])}</b><p>{H(t["text"])}</p></div>' for t in learning[key]['takeaways'])
        return f'''<article class="dossier" id="dossier-{key}" role="tabpanel" aria-labelledby="tab-{key}"><div class="dossier-title"><h3>{H(d['name'])}</h3><a class="version" href="https://github.com/{REPOS[key]}/commit/{d['commit']}" target="_blank" rel="noopener">源码快照 {d['commit'][:12]} ↗</a></div><p class="dossier-summary">{H(learning[key]['lead'])}</p><div class="framework-takeaways">{takeaway}</div><p class="scope">{H('Claude Code：社区泄露镜像；未对原始 sourcemap 做认证，生产特性与缺失模块内部算法未知。' if key=='claude' else '以下结论限定于固定源码快照；带门控的能力不代表全部用户的默认行为。')}</p><div class="dossier-actions"><button type="button" class="watch" data-framework="{key}">看 {H(d['name'])} 的动画</button><button type="button" class="reveal-evidence" data-framework="{key}" aria-controls="evidence-{key}">查看 {counts} 条规则与源码</button></div>
<details class="dossier-evidence research-detail" id="evidence-{key}"><summary>展开 {H(d['name'])} 的实现与证据 · {counts} 条规则</summary><p>{rich(d['overview'])}</p>
<h4>① 上下文从哪里来，何时失效？</h4><div class="table-wrap"><table><thead><tr><th>层 / 对象</th><th>来源</th><th>生命周期</th><th>预算与边界</th></tr></thead><tbody>{ls}</tbody></table></div>
<h4>② 沿真实调用链看一次请求</h4><ol class="pipeline">{pipeline}</ol>
<div class="rules-toolbar"><h4>③ 可核查规则 <span class="source-count">{counts}</span></h4><button type="button" class="expand-rules" data-framework="{key}">展开全部规则</button></div>{''.join(rule_html(key,r) for r in d['rules'])}
<details><summary>④ 逐函数研究笔记与额外证据</summary><div class="deep-notes">{md(read_utf8(NOTES / f'{key}.md'))}</div></details>
<details><summary>⑤ 版本限制与上一版修正</summary><div class="deep-notes">{rich(corrections)}<p>{rich(d.get('limitations',[]))}</p></div></details></details></article>'''

    def table_sources():
        return '<div class="table-wrap"><table><thead><tr><th>项目</th><th>快照</th><th>研究规则数</th></tr></thead><tbody>'+''.join(f'<tr><th>{H(d["name"])}</th><td><a href="https://github.com/{REPOS[k]}/commit/{d["commit"]}"><code>{H(d["commit"])}</code></a></td><td>{len(d["rules"])}</td></tr>' for k,d in data.items())+'</tbody></table></div>'

    def section(id,n,title,body):return f'<section id="{id}"><div class="section-head"><span>{n}</span><h2>{title}</h2></div>{body}</section>'
    def chapter_end(conclusion,href,label):return f'<div class="chapter-end"><p><span>本节要点</span>{conclusion}</p><a href="{href}">{label} <span aria-hidden="true">→</span></a></div>'

    scene_data=load_json('scenes.json')
    validate_scenes(scene_data, source_locations)
    if source_root is not None:
        validate_sources(source_root, data, source_locations)
    def pack_scenes(frameworks):
        pools={'blocks':[],'sources':[]};lookup={k:{} for k in pools}
        def intern(pool,obj):
            k=json.dumps(obj,ensure_ascii=False,sort_keys=True,separators=(',',':'))
            if k not in lookup[pool]:lookup[pool][k]=len(pools[pool]);pools[pool].append(obj)
            return lookup[pool][k]
        packed=[]
        for f in frameworks:
            f=json.loads(json.dumps(f))
            for sc in f['scenarios']:
                for step in sc['steps']:
                    for lane in ['archive','request','output']:step[lane]=[intern('blocks',b) for b in step[lane]]
                    step['sources']=[intern('sources',s) for s in step['sources']]
            packed.append(f)
        return {**pools,'frameworks':packed}
    scene_pack=pack_scenes(scene_data)
    animation_css=read_utf8(ASSETS / 'animation.css');animation_js=read_utf8(ASSETS / 'animation.js')
    memory_css=read_utf8(ASSETS / 'memory.css');memory_js=read_utf8(ASSETS / 'memory-content.js')+'\n'+read_utf8(ASSETS / 'memory.js')+'\n'+read_utf8(ASSETS / 'capacity.js')

    overview=read_utf8(ASSETS / 'textbook-overview.html')
    sections=[section('model','01','先看全图：模型每轮究竟能看到什么？',overview+'<details class="research-detail" id="model-details"><summary>深入理解：五类对象、六类操作与形式化关系</summary>'+abstract+'</details>'+chapter_end('保存的原文、这轮选出的材料和真正发送的请求，是三个需要区分的对象。','#lab','下一步：看材料怎样进入和释放'))]
    scene_count=sum(len(f['scenarios']) for f in scene_data)
    guides=[('claude','Claude Code','先做条件清理；需要完整摘要时，普通旧对话由摘要接替，再把必要文件和 Skill 读回。所以释放空间后，恢复材料还会占用一部分。','full'),('codex','Codex','按能力选择本地编排摘要或远程压缩；远程压缩块的内部大小不能凭密文估算。另有条件启用的新窗口模式，不会自动生成摘要。','codex-normal-v2'),('opencode','OpenCode','留下结构化摘要与近期内容的文本版本；近期工具正文也会截短。因此“保留近期”不等于把近期所有原文都装回。','opencode-summary'),('hermes','Hermes','通常保护开头与最近进展，整理中间旧内容。得到摘要只是候选；安全提交后，还要看下一次真实用量是否回到合适范围。','normal'),('pi','Pi','在会话树里记下摘要和保留起点，并保存当前系统与工具状态。旧记录仍在，下一次按新的边界重建模型能看到的部分。','pi-normal-split')]
    guide_html='<div class="memory-frameworks"><h3>五个框架，具体走哪条路径？</h3><p>上面的故事演示“先摘要，再接纳新材料”这一条路径。真实框架还有确定性裁剪、远程压缩、保留边界和失败恢复。选择一个框架，观察哪些内容留下、谁发起整理、何时提交，以及哪里与故事不同。</p>'+''.join(f'<div class="memory-framework-row"><strong>{title}</strong><p>{H(desc)}</p><button type="button" class="guide-scene" data-framework="{key}" data-scenario="{scenario}">看 {title} 的过程</button></div>' for key,title,desc,scenario in guides)+'</div>'
    sections.append(section('lab','02','再看过程：装不下新材料时，会发生什么？',f'''<p class="chapter-lead">规则、任务、文件与对话都占空间。跟着一次“修复登录错误”的教学故事，观察哪些内容进入、哪些被摘要，以及空间何时真正释放。</p><div id="memory-story-root"></div><details class="research-detail"><summary>选择一个框架，对照它与故事的不同</summary>{guide_html}</details><details id="advanced-lab"><summary>源码分镜：查看五个框架的正常与异常路径</summary><p class="scope">以下 {scene_count} 个分镜按固定源码规则构造，包含正常、条件与失败路径。所有大小是教学示例。未知原生项保留未知，不用密文字符数冒充 token；这不是实际模型运行录像。</p><div id="animation-root"></div><p><button id="inspect-framework" type="button">查看当前框架的完整规则与调用链 ↓</button></p></details><noscript><p class="nojs">动画需要 JavaScript。下面的实现档案、调用链与所有源码证据仍可直接阅读。</p></noscript>'''+chapter_end('摘要替换改变的是本轮可见内容；原文仍可查，细节可能遗漏，回读也需要预算。','#implementation','下一步：比较五个框架')))
    tabs=''.join(f'<button id="tab-{k}" type="button" role="tab" aria-selected="{str(i==0).lower()}" aria-controls="dossier-{k}" data-framework="{k}">{H(data[k]["name"])}</button>' for i,k in enumerate(ORDER))
    sections.append(section('implementation','03','比较实现：它们怎样选择、保留和继续？',f'''<p class="chapter-lead">用相同的三个问题读每个框架：何时整理、保留什么、怎样继续。需要核查条件时，再展开完整规则和固定版本的源码。</p><div class="framework-tabs" role="tablist" aria-label="框架实现档案">{tabs}</div><div class="rules-toolbar"><label for="rule-search">查找具体规则 <input type="search" id="rule-search" placeholder="例如：工具、失败、system、usage"></label><span id="search-count" role="status"></span><button type="button" id="jump-rule" hidden>跳到匹配规则</button></div>{''.join(dossier(k,data[k]) for k in ORDER)}'''+chapter_end('相同的“压缩历史”，可能走不同的分支；保留边界、计量有效期和提交条件都要一起看。','#comparison','继续：横向核对差异')))
    sections.append(section('comparison','04','放在一起看：相似操作，哪些边界不同？','''<p class="chapter-lead">比较时先核对三个边界：模型真正收到什么、哪些旧内容能找回、摘要何时被接受。再查各框架的阈值与恢复条件。</p><details class="research-detail"><summary>展开横向规则矩阵 · 11 个比较维度</summary><p class="table-hint">表格较宽，可横向滚动；各单元格的条件以框架档案中的源码证据为准。</p>'''+matrix()+'''</details><div class="comparison-summary"><div><h3>计量要绑定当前请求</h3><p>规则、工具定义和模型改变后，旧用量未必继续有效。原生压缩项的内部大小未知时，应当保留未知。</p></div><div><h3>能继续，也要能安全停下</h3><p>超大结果、摘要失败、并发更新和恢复重启，都需要具体的检查与停止条件。固定比例不能保证任何输入都装得下。</p></div></div>'''+chapter_end('理解共同职责，再比较具体规则；本报告的教学数值不用于给框架排效果名次。','#evidence','查阅研究证据与版本')))

    budget_intro='''<p class="chapter-lead">输入窗口、触发线、近期保留目标、摘要输出预算各回答不同的问题。把它们分开，才能解释为什么“还剩一些 token”仍可能需要整理。</p><details class="research-detail"><summary>展开四笔预算与六条工程不变量</summary>'''+invariants+'</details>'
    sections.append(section('constraints','A','深入理解：四笔预算怎样分开算？',budget_intro))

    lester='''<p>以下是基于现有 Lester 预算与持久 checkpoint 结构提出的设计建议，不表示本次已经修改 agent runtime。</p><div class="path-map"><div><b>已有基础</b><span>完整请求估算、工具 projection、checkpoint 与持久审计已有实现。</span><span>不要重新搭一套互不一致的模型历史。</span></div><div><b>规则状态</b><span>参考 OpenCode / Pi：分开当前规则快照、规则增量与摘要覆盖边界。</span><span>验证压缩后是否恢复当前 system、tools 与有效 Skills。</span></div><div><b>计量有效性</b><span>参考 Hermes / Codex：usage anchor 必须说明绑定对象和失效条件。</span><span>区分实测 / 估算 / 未知；压缩或模型变化后不能继续复用旧值。</span></div><div><b>分级回收</b><span>入口限量 → 工具结果投影 → 摘要；按实际回收收益决定是否改前缀。</span><span>保留 read_tool_result 恢复线索，禁止为找回结果而重放工具。</span></div><div><b>持久一致性</b><span>保留原记录，摘要绑定覆盖范围、保留 ID、版本与 run。</span><span>异步 / 并发旧结果不能覆盖新输入、撤销或规则变化。</span></div><div><b>停止条件</b><span>保护集合过大、无进展、摘要失败、provider 仍拒绝时显式停止。</span><span>不靠无穷压缩保证任务继续，也不偷偷截掉最新用户要求。</span></div></div>
<h3>建议的验证矩阵</h3><div class="table-wrap"><table><thead><tr><th>输入 / 变化</th><th>应保持的性质</th><th>可观察证据</th></tr></thead><tbody><tr><th>一次巨大工具输出</th><td>不拆散调用关系，不反复重跑工具</td><td>wire token、stub、原结果读取位置、工具执行次数</td></tr><tr><th>连续多次摘要</th><td>最新要求覆盖旧摘要，未解决失败不丢</td><td>逐轮工作集 diff、摘要覆盖 / retained ID</td></tr><tr><th>规则 / 模型切换</th><td>新的上下文态与计量绑定一致</td><td>baseline版本、usage来源、native重放分支</td></tr><tr><th>摘要超时 / overload / overflow</th><td>保留记录、有界恢复、不把fallback说成保真</td><td>attempt上限、cooldown、失败事件、归档恢复</td></tr><tr><th>取消 / 并发 / 重启</th><td>过期结果不写入、用户动作不被重放</td><td>版本fence、事务、run状态、原始receipt</td></tr></tbody></table></div>
<p class="caption">源码定位：<a href="https://github.com/biubiuqiu/lester-agent/blob/e58dc942e8cc526a0dd54f46d44a77352a330e9b/backend/internal/toolcontext/budget.go">budget.go</a>、<a href="https://github.com/biubiuqiu/lester-agent/blob/e58dc942e8cc526a0dd54f46d44a77352a330e9b/backend/internal/toolcontext/checkpoint.go">checkpoint.go</a>、<a href="https://github.com/biubiuqiu/lester-agent/blob/e58dc942e8cc526a0dd54f46d44a77352a330e9b/backend/internal/conversation/context_checkpoints.go">context_checkpoints.go</a>。本次没有真实模型 benchmark，不报告虚构的召回率、成本排名或“永不超限”保证。</p>'''
    sections.append(section('lester','B','工程参考：怎样把这些边界映射到 Lester？','<p class="chapter-lead">从预算、规则状态、分级回收和持久一致性四个方向，把研究结论变成可验证的工程约束。</p><details class="research-detail"><summary>展开设计建议与验证矩阵</summary>'+lester+'</details>'))

    all_ev=''.join(f'<li><a href="{H(e["url"])}" target="_blank" rel="noopener"><strong>{H(e["framework"])} · {H(e.get("symbol") or e["path"])}</strong></a><code>{H(e["path"])} : {e["line"]}–{e.get("end",e["line"])}</code></li>' for e in evidence.values())
    method=f'''<p>研究日期：2026-10-22（Asia/Shanghai）。以之前已获取的固定源码快照做第二轮调用链与状态转换分析，不在研究中悄悄漂移到不同版本。共整理 <strong>{sum(len(d['rules']) for d in data.values())} 条规则、{len(evidence)} 处源码定位、{scene_count} 个动画场景</strong>。</p>{table_sources()}
<h3>这次如何提高研究深度</h3><ol><li>从 agent / session 入口向下追踪 request assembly、history projection、provider conversion 与 compaction 回调，而非只看 compact 文件。</li><li>逐条记录条件、动作、保留与丢失、默认值、特性 / 路由门控；将静态实现与运行时默认分开。</li><li>以正常与异常两类以上 trace 检查调用时序，并让动画显示同一份状态转换。</li><li>对照维护者文档与实际代码；发生漂移时明确列出，采用固定源码分支证据。</li></ol>
<div class="note"><strong>可见边界：</strong>Claude Code 使用社区公开泄露镜像，未独立认证 sourcemap 来源；部分被引用的内部模块缺失。Codex / OpenCode / Hermes 的 provider 原生压缩内部算法不能从客户端源码得知。本次静态追踪并检查教学播放器，不安装运行五套 agent，也不将动画数值称为实测。</div>
<h3>本轮纠正了上一版的哪些简化</h3><ul class="revision-list"><li><strong>Claude Code：</strong>微压缩不是默认必经清理；完整摘要后的估算不等于完整请求总量校验。</li><li><strong>Codex：</strong>TokenBudget 的 feature-table 默认与模型元数据产生的有效行为必须区分；本地与远程保留规则不同。</li><li><strong>OpenCode：</strong>recent-context 是有截断的文本再现；规则 epoch 与对话 checkpoint 是两条协同状态流。</li><li><strong>Hermes：</strong>no-LLM prune 与 rolling LLM micro-compaction 不同；当前 usage anchor 已覆盖 provider-priced prefix fingerprint。</li><li><strong>Pi：</strong>system / tools 也有事件流与 checkpoint；压缩预算与 provider 输出 clamp 是两层，不能画成一个门。</li></ul>
<details><summary>展开全部源码索引（{len(evidence)} 处）</summary><ul class="evidence-list">{all_ev}</ul></details>'''
    sections.append(section('evidence','C','回到证据：这些结论来自哪里？',f'<p class="chapter-lead">研究基于 2026-10-22 的固定源码快照，共 {sum(len(d["rules"]) for d in data.values())} 条规则、{len(evidence)} 处源码定位。教学动画演示状态变化，不代表实际模型运行结果。</p>'+table_sources()+'<details class="research-detail"><summary>展开研究方法、已知边界与完整源码索引</summary>'+method.replace(table_sources(),'')+'</details>'))

    nav=[('model','01 · 建立全图'),('lab','02 · 看动画过程'),('implementation','03 · 比较五个框架'),('comparison','04 · 横向核对差异'),('constraints','四笔预算与不变量'),('lester','Lester 工程参考'),('evidence','源码证据与版本')]
    ui_js='''
document.body.classList.add('js-ready');
const menuButton=document.querySelector('.menu-toggle'),studyNav=document.getElementById('study-nav');
function closeMenu(restore=false){document.body.classList.remove('nav-open');menuButton.setAttribute('aria-expanded','false');if(restore)menuButton.focus();}
menuButton.addEventListener('click',()=>{const open=!document.body.classList.contains('nav-open');document.body.classList.toggle('nav-open',open);menuButton.setAttribute('aria-expanded',String(open));});
studyNav.addEventListener('click',e=>{if(e.target.closest('a'))closeMenu();});
document.addEventListener('keydown',e=>{if(e.key==='Escape'&&document.body.classList.contains('nav-open')){e.preventDefault();closeMenu(true);}});
window.memoryStory=MemoryStory.mount(document.getElementById('memory-story-root'));
const memoryStage=document.getElementById('memory-stage'),memoryAlias=document.createElement('span');memoryAlias.id='memory-story';memoryAlias.setAttribute('aria-hidden','true');memoryAlias.style.cssText='position:absolute;top:0;left:0;scroll-margin-top:16px';memoryStage.style.position='relative';memoryStage.prepend(memoryAlias);
function alignMemoryEntry(){if(location.hash==='#memory-story'||location.hash==='#memory-stage')requestAnimationFrame(()=>memoryStage.scrollIntoView({block:'start',behavior:'instant'}));}
alignMemoryEntry();window.addEventListener('load',alignMemoryEntry);window.addEventListener('hashchange',alignMemoryEntry);
const instance=ContextAnimation.mount(document.getElementById('animation-root'),SCENES);
window.researchPlayer=instance;
const tabs=[...document.querySelectorAll('[role=tab][data-framework]')];
function selectDossier(id,scroll=false){tabs.forEach(t=>{const yes=t.dataset.framework===id;t.setAttribute('aria-selected',yes);t.tabIndex=yes?0:-1});document.querySelectorAll('.dossier').forEach(a=>a.hidden=a.id!=='dossier-'+id);filterRules();if(scroll)document.getElementById('implementation').scrollIntoView({behavior:matchMedia('(prefers-reduced-motion:reduce)').matches?'instant':'smooth'});}
tabs.forEach((t,i)=>{t.addEventListener('click',()=>selectDossier(t.dataset.framework));t.addEventListener('keydown',e=>{let j=i;if(e.key==='ArrowRight')j=(i+1)%tabs.length;else if(e.key==='ArrowLeft')j=(i+tabs.length-1)%tabs.length;else if(e.key==='Home')j=0;else if(e.key==='End')j=tabs.length-1;else return;e.preventDefault();selectDossier(tabs[j].dataset.framework);tabs[j].focus()})});
function filterRules(){const q=document.getElementById('rule-search').value.trim().toLocaleLowerCase();let total=0,visible=0;document.querySelectorAll('.rule').forEach(r=>{r.hidden=q&&!r.dataset.search.toLocaleLowerCase().includes(q);if(!r.closest('.dossier').hidden){total++;if(!r.hidden)visible++}});document.getElementById('search-count').textContent=q?`当前框架 ${visible} / ${total} 条匹配`:`当前框架 ${total} 条规则`;const jump=document.getElementById('jump-rule');jump.hidden=!q||!visible;jump.textContent=`查看 ${visible} 条匹配规则 ↓`;if(q){const current=document.querySelector('.dossier:not([hidden]) .dossier-evidence');if(current)current.open=true;}}
function showEvidence(id,scroll=true){selectDossier(id);const d=document.getElementById('evidence-'+id);d.open=true;if(scroll)d.scrollIntoView({behavior:matchMedia('(prefers-reduced-motion:reduce)').matches?'instant':'smooth',block:'start'});}
document.querySelectorAll('.reveal-evidence').forEach(b=>b.addEventListener('click',()=>showEvidence(b.dataset.framework)));
document.getElementById('rule-search').addEventListener('input',filterRules);
function jumpToRule(){const first=document.querySelector('.dossier:not([hidden]) .rule:not([hidden])');if(first){first.closest('.dossier-evidence').open=true;first.open=true;first.scrollIntoView({block:'start',behavior:matchMedia('(prefers-reduced-motion:reduce)').matches?'instant':'smooth'});first.querySelector('summary').focus({preventScroll:true});}}
document.getElementById('jump-rule').addEventListener('click',jumpToRule);
document.getElementById('rule-search').addEventListener('keydown',e=>{if(e.key==='Enter'&&!document.getElementById('jump-rule').hidden){e.preventDefault();jumpToRule();}});
document.querySelectorAll('.expand-rules').forEach(b=>b.addEventListener('click',()=>{const rules=[...document.querySelectorAll('#dossier-'+b.dataset.framework+' .rule:not([hidden])')];const open=rules.some(r=>!r.open);rules.forEach(r=>r.open=open);b.textContent=open?'收起全部规则':'展开全部规则'}));
function revealRule(target){if(!target)return;const dossier=target.closest('.dossier');if(dossier){document.getElementById('rule-search').value='';selectDossier(dossier.id.replace('dossier-',''));}let d=target;while(d&&d!==document.body){if(d.tagName==='DETAILS')d.open=true;d=d.parentElement;}}
document.querySelectorAll('.open-rule').forEach(a=>a.addEventListener('click',()=>revealRule(document.querySelector(a.getAttribute('href')))));
function openAdvanced(framework,scenario){document.getElementById('advanced-lab').open=true;instance.setFramework(framework);if(scenario)instance.setScenario(scenario);document.getElementById('advanced-lab').scrollIntoView({behavior:matchMedia('(prefers-reduced-motion:reduce)').matches?'instant':'smooth'});}
document.querySelectorAll('.watch').forEach(b=>b.addEventListener('click',()=>openAdvanced(b.dataset.framework)));
document.querySelectorAll('.guide-scene').forEach(b=>b.addEventListener('click',()=>openAdvanced(b.dataset.framework,b.dataset.scenario)));
document.getElementById('inspect-framework').addEventListener('click',()=>showEvidence(instance.getState().framework));
document.getElementById('print-report').addEventListener('click',()=>{document.querySelectorAll('details').forEach(d=>d.open=true);window.print()});
selectDossier('claude');
function revealHashRule(){const target=document.getElementById(location.hash.slice(1));if(target&&target.classList.contains('rule')){revealRule(target);requestAnimationFrame(()=>target.scrollIntoView({block:'start',behavior:'instant'}));}}
revealHashRule();window.addEventListener('hashchange',revealHashRule);
if('IntersectionObserver' in window){const o=new IntersectionObserver(es=>{es.forEach(e=>{if(e.isIntersecting){document.querySelectorAll('.rail nav a').forEach(a=>a.removeAttribute('aria-current'));const a=document.querySelector(`.rail nav a[href="#${e.target.id}"]`);if(a){a.setAttribute('aria-current','location');document.querySelector('.mobile-chapter span').textContent=a.textContent;}}})},{rootMargin:'-10% 0px -65% 0px'});document.querySelectorAll('main>section').forEach(s=>o.observe(s))}
'''
    styles='\n'.join([read_utf8(ASSETS / 'report.css'),animation_css,memory_css,read_utf8(ASSETS / 'textbook-overview.css'),read_utf8(ASSETS / 'textbook.css')])
    nav_html='<nav id="study-nav" aria-label="学习与查阅目录">'+''.join('<div class="nav-group"><span class="nav-group-label">'+label+'</span>'+''.join(f'<a href="#{k}">{v}</a>' for k,v in items)+'</div>' for label,items in [('学习主线',nav[:4]),('深入查阅',nav[4:])])+'</nav>'
    rail='<aside class="rail"><div class="brand">Lester / Research</div><button type="button" class="menu-toggle" aria-controls="study-nav" aria-expanded="false">目录 <span aria-hidden="true">☰</span></button><p>上下文管理 · 互动研究</p><div class="mobile-chapter">正在阅读<span>学习总览</span></div>'+nav_html+'<small>源码快照 2026.10.22<br>版式更新 2026.10.25<br>教学演示与静态源码研究</small></aside>'
    hero="""<header class="hero" id="top"><p class="kicker">AGENT CONTEXT · 从原理到源码</p><h1>上下文快满时，<br>Agent 怎样继续工作？</h1><p class="intro">规则、文件和对话越来越多，模型的窗口却是有限的。沿着一张关系图、一个完整故事，理解五个框架怎样选择材料、整理历史，再继续工作。</p><ul class="learning-goals" aria-label="你会理解的三个问题"><li>为什么保存的全部历史，不等于模型本轮看见的内容？</li><li>摘要释放了什么空间，又省略了哪些细节？</li><li>五个框架的触发、保留和恢复规则，分别有什么不同？</li></ul><div class="actions"><a href="#model">从全图开始理解 <span aria-hidden="true">→</span></a><a href="#memory-stage">直接看动画故事</a></div><details class="research-meta"><summary>研究范围、版本与打印</summary><div class="metadata"><span>Claude Code / Codex / OpenCode / Hermes / Pi</span><span>116 条规则 · 320 处源码定位 · 21 个分镜</span><span>源码快照 2026-10-22；教学数值不代表实测</span></div><button id="print-report" type="button">打印 / 保存 PDF</button></details></header><nav class="learning-route" aria-label="建议阅读顺序"><ol><li><a href="#model">建立全图<span>分清原文、材料与请求</span></a></li><li><a href="#lab">看动画过程<span>观察装满与释放</span></a></li><li><a href="#implementation">比较五个框架<span>核对选择与保留</span></a></li><li><a href="#evidence">查阅源码证据<span>追溯条件与版本</span></a></li></ol></nav>"""
    script_data=json.dumps(scene_pack,ensure_ascii=False,separators=(',',':')).replace('</','<\\/')
    script='const SCENE_PACK='+script_data+';\nconst SCENES=SCENE_PACK.frameworks;for(const f of SCENES)for(const sc of f.scenarios)for(const s of sc.steps){for(const lane of ["archive","request","output"])s[lane]=s[lane].map(i=>SCENE_PACK.blocks[i]);s.sources=s.sources.map(i=>SCENE_PACK.sources[i]);}\n'+animation_js+'\n'+memory_js+'\n'+ui_js
    doc='<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="description" content="用关系图、逐步动画和固定版本源码，理解五个Agent框架的上下文装配、预算、压缩与恢复。"><link rel="icon" href="data:,"><title>上下文管理：从抽象到实现 · Lester Research</title><style>'+styles+'</style></head><body class="textbook"><a class="skip" href="#main">跳到正文</a>'+rail+'<main id="main">'+hero+''.join(sections)+'<footer>源码静态研究与确定性教学动画。规则限定于固定版本；动画数值为教学设定。内容、图解和交互均内嵌在此单文件中。</footer></main><script>'+script+'</script></body></html>'

    return doc, json.dumps(list(evidence.values()), ensure_ascii=False, indent=2), {
        'rules': sum(len(d['rules']) for d in data.values()),
        'evidence': len(evidence),
        'scenes': scene_count,
        'source_locations': sum(len(locations) for locations in source_locations.values()),
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true', help='compare generated HTML and evidence index without writing any files')
    parser.add_argument('--source-root', type=Path, metavar='PATH', help='optionally validate existing claude-full/codex/opencode/hermes/pi Git checkouts; never clones or downloads')
    args = parser.parse_args()
    try:
        document, evidence_json, counts = render_report(args.source_root)
        outputs = {ROOT / 'index.html': document.encode('utf-8'), DATA / 'evidence-index.json': evidence_json.encode('utf-8')}
        if args.check:
            mismatches = [str(path.relative_to(ROOT)) for path, expected in outputs.items() if not path.is_file() or path.read_bytes() != expected]
            if mismatches:
                raise ValueError('generated files are missing or stale: ' + ', '.join(mismatches) + '; run scripts/build.py')
        else:
            for path, content in outputs.items():
                path.write_bytes(content)
        print(json.dumps({
            'mode': 'check' if args.check else 'build',
            'status': 'ok',
            'bytes': len(outputs[ROOT / 'index.html']),
            **counts,
            'source_validation': 'checked existing fixed-commit source files' if args.source_root is not None else 'not performed; validated bundled research data and reference structure only',
            'outputs': [str(path.relative_to(ROOT)) for path in outputs],
        }, ensure_ascii=False))
    except (OSError, ValueError, KeyError, TypeError) as error:
        parser.exit(1, f'build error: {error}\n')


if __name__ == '__main__':
    main()
