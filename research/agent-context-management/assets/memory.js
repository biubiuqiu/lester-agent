(function () {
  'use strict';
  var counter = 0;
  var NS = 'http://www.w3.org/2000/svg';
  var catalog = {
    rules: { name: '做事规则', color: '#4f7d9a', note: '告诉模型怎么做事。压缩旧材料时，这 20K 规则继续保留。' },
    goal: { name: '你的要求', color: '#8b78b4', note: '“修复登录错误”这项任务要求。示例里保留原文 10K。' },
    oldTools: { name: '旧文件与命令结果', color: '#d5a35d', note: '过去读到的原文与命令结果。后面只把旧的 70K 纳入摘要；资料柜的原文不缩小。' },
    history: { name: '较早的对话', color: '#b9a18a', note: '已经发生的讨论和尝试，共 50K。它和旧工具结果一起交给整理机。' },
    recent: { name: '最近的进展', color: '#64a998', note: '马上还要用到的 20K 近期结果。这个例子保持原文，不参与本次摘要。' },
    summary: { name: '关键进展摘要', color: '#16806b', note: '把 120K 旧材料整理成 10K 重点：目标、结论、失败和下一步。可能遗漏细节，需要时再读原文。' },
    incoming: { name: '新读到的结果', color: '#dc8867', note: '门口等待的 30K 新结果。先释放空间，它才进入本轮工作区。' },
    reply: { name: '刚生成的回复', color: '#3d8a91', note: '这条 5K 回复生成以后，会在下一轮成为历史输入；不是同一轮把输出重复计作输入。' }
  };
  function blocks(entries) { return entries.map(function (p) { return { id: p[0], tokens: p[1] }; }); }
  var states = [
    { title: '先放进规则和你的要求', sentence: '模型先知道怎么做事、你想做什么。此时只用了 30K，还有很多空位。', content: blocks([['rules', 20000], ['goal', 10000]]), pending: 0, archive: 1, tip: '想像一张有 40 格的工作桌：每格能放 5K token。模型只能直接看到桌上的内容。', action: '任务：“帮我修复登录错误。”' },
    { title: '读一个文件，结果也会占地方', sentence: '新读到的 40K 文件内容从右侧进入。它不是免费的背景知识，而是工作区里的输入。', content: blocks([['rules', 20000], ['goal', 10000], ['oldTools', 40000]]), pending: 0, archive: 2, tip: '文件原文保存在左边资料柜；本轮要使用的副本占右边模型工作区。保存与看见，是两件事。', action: '读取文件：+40K' },
    { title: '工作几轮以后，桌面快满了', sentence: '对话、旧工具结果和最近进展越积越多，输入来到 170K，只剩 10K 能放新材料。', content: blocks([['rules', 20000], ['goal', 10000], ['oldTools', 70000], ['history', 50000], ['recent', 20000]]), pending: 0, archive: 4, tip: '总窗口是 200K，但右下角 20K 已留给模型生成回复。所以输入最多放 180K。', action: '多轮讨论与工具：再增加 100K' },
    { title: '新结果有 30K，先在门口等一下', sentence: '剩余只有 10K，装不下新的 30K。新材料留在盒子外面，没有偷偷塞进模型输入。', content: blocks([['rules', 20000], ['goal', 10000], ['oldTools', 70000], ['history', 50000], ['recent', 20000]]), pending: 30000, archive: 4, tip: '这就是需要管理上下文的时刻：不能把“总窗口 200K”误当成还有 30K 可以放。', action: '待进入 30K ＞ 空位 10K' },
    { title: '选出旧材料，整理成一份重点', sentence: '只选较早对话 50K 和旧工具结果 70K，共 120K。规则、你的要求和近期进展留在原位。', content: blocks([['rules', 20000], ['goal', 10000], ['oldTools', 70000], ['history', 50000], ['recent', 20000]]), pending: 30000, archive: 4, selected: true, tip: '整理本身通常也需要一次模型处理。这一步是“准备摘要”，还没有释放空间。', action: '50K 旧对话 + 70K 旧结果 → 摘要整理机' },
    { title: '120K 旧材料，收拢成 10K 摘要', sentence: '旧色块离开本轮工作区，进入整理机，变成较小的绿色摘要。输入从 170K 降到 60K，释放 110K。', content: blocks([['rules', 20000], ['goal', 10000], ['recent', 20000], ['summary', 10000]]), pending: 30000, archive: 4, compacted: true, tip: '资料柜里的原文仍是原来的大小。变小的是本轮带给模型的内容，不是把文件做成了无损 ZIP。', action: '保留 50K + 摘要 10K = 输入 60K' },
    { title: '空位够了，再让新结果进入', sentence: '等待的 30K 现在飞入空位。输入变为 90K，还能继续处理任务。', content: blocks([['rules', 20000], ['goal', 10000], ['recent', 20000], ['summary', 10000], ['incoming', 30000]]), pending: 0, archive: 4, compacted: true, tip: '顺序很重要：先把旧材料整理好、释放空间，再把等待的新结果放进来。', action: '60K + 新结果 30K = 输入 90K' },
    { title: '继续工作；下一轮也要算上新回复', sentence: '模型结合规则、要求、近期进展、摘要和新结果继续。生成的 5K 回复，在下一轮加入历史后是 95K。', content: blocks([['rules', 20000], ['goal', 10000], ['recent', 20000], ['summary', 10000], ['incoming', 30000], ['reply', 5000]]), pending: 0, archive: 4, compacted: true, tip: '摘要可能漏细节；需要时再读资料柜原文。本例整理后装得下，若仍然过大，还要继续减量或明确停止。', action: '下一轮输入：90K + 新回复 5K = 95K' }
  ];
  function el(tag, attrs, text) { var n = document.createElementNS(NS, tag); Object.keys(attrs || {}).forEach(function (k) { n.setAttribute(k, attrs[k]); }); if (text != null) n.textContent = text; return n; }
  function esc(s) { return String(s == null ? '' : s).replace(/[&<>"']/g, function (c) { return ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]; }); }
  function kfmt(n) { return (Math.round(n / 100) / 10).toLocaleString('en-US', { maximumFractionDigits: 1 }) + 'K'; }
  function amount(state) { return state.content.reduce(function (v, b) { return v + b.tokens; }, 0); }
  function cellsFor(state) { var pos = 0, list = []; state.content.forEach(function (b) { for (var i = 0; i < b.tokens / 5000; i++) { list.push({ key: b.id + '-' + i, type: b.id, x: 279 + (pos % 10) * 54, y: 167 + Math.floor(pos / 10) * 64 }); pos++; } }); return list; }

  function mount(container) {
    if (typeof container === 'string') container = document.querySelector(container);
    if (!container) throw new Error('MemoryStory.mount needs a container.');
    if (container.__memoryStory) container.__memoryStory.destroy();
    var uid = 'ms-' + (++counter), index = 0, playing = false, moving = false, pausedMotion = false, timer = null, raf = null, generation = 0, disposed = false, currentValue = amount(states[0]), compactLayout = false;
    var active = new Set(), bindings = [], mq = window.matchMedia('(prefers-reduced-motion: reduce)'), reduced = mq.matches, clock = null, movementDone = Promise.resolve(), transitionStart = 0, lessonTab = 'summary', lessonTrigger = null;
    container.classList.add('memory-story');
    container.innerHTML = '<div class="ms-intro"><p><strong>模型眼前的材料，像一块有限工作内存。</strong> 原文另存，只有带入的副本占本轮空间。</p><p class="ms-analogy">以下演示“先摘要，再接纳新材料”这一条路径；五个框架的实际触发与保留规则各有不同。内存只是按 token 计的类比，容量和故事内容均为教学设定。</p></div>' +
      '<div class="ms-stage" id="memory-stage" aria-label="上下文管理动画舞台"><div class="ms-controls"><button type="button" class="ms-play">播放故事</button><button type="button" class="ms-prev">上一步</button><button type="button" class="ms-next">下一步</button><button type="button" class="ms-reset">重看</button><span class="ms-step-count"></span></div>' +
      '<div class="ms-progress" role="group" aria-label="八步上下文故事"></div><div class="ms-scene-copy"><div class="ms-phase-status" role="status"></div><h3 class="ms-title"></h3><p class="ms-sentence"></p></div>' +
      '<div class="ms-readouts"><div><span>本轮输入</span><strong class="ms-input"></strong><small>上限 180K</small></div><div><span>还能放</span><strong class="ms-free"></strong><small>不含回复预留</small></div><div><span>待进入</span><strong class="ms-pending"></strong><small>仍在窗口外</small></div></div>' +
      '<div class="ms-mobile-ports"><button type="button" class="ms-archive-link" aria-expanded="false"><span>原文资料柜 · H</span><strong>原文始终保存</strong></button><div class="ms-mobile-tray"><span>新材料入口</span><strong class="ms-mobile-tray-size">暂无等待材料</strong></div></div><div class="ms-mobile-memory-heading"><strong>本轮材料（合并视图）</strong><span>P + V + 新输入</span></div>' +
      '<div class="ms-mini-key" aria-label="色块图例"></div><div class="ms-visual"><svg class="ms-svg" viewBox="0 0 1120 610" role="img" aria-labelledby="' + uid + '-svgtitle ' + uid + '-svgdesc"><title id="' + uid + '-svgtitle">模型工作内存与原文资料柜</title><desc id="' + uid + '-svgdesc">总共四十格，每格五千 token。最后四格是生成回复的预留空位。新材料进入、旧材料收拢为摘要时，色块会移动，但左侧原文保留。</desc><defs><pattern id="' + uid + '-reserve" width="8" height="8" patternUnits="userSpaceOnUse"><rect width="8" height="8" fill="#eef0f2"/><path d="M-2 2 L2 -2 M0 8 L8 0 M6 10 L10 6" stroke="#c4ccd0" stroke-width="1.5"/></pattern><marker id="' + uid + '-arrow" markerWidth="8" markerHeight="8" refX="7" refY="4" orient="auto"><path d="M0 0 L8 4 L0 8" fill="none" stroke="#a5b8b2" stroke-width="1.5"/></marker></defs>' +
      '<g class="ms-cabinet"><text x="108" y="124" text-anchor="middle" class="ms-svg-heading">原文资料柜</text><text x="108" y="146" text-anchor="middle" class="ms-svg-small">存下来，不等于本轮都带上</text><rect x="25" y="180" width="168" height="250" rx="12" fill="#f3f7f5" stroke="#bfcec8" stroke-width="2"/><g class="ms-shelf-files"></g><rect x="79" y="409" width="60" height="4" rx="2" fill="#b5c7bd"/><text x="108" y="466" text-anchor="middle" class="ms-svg-small ms-original-label">原文不会跟着摘要变小</text></g>' +
      '<path d="M199 289 L248 289" fill="none" stroke="#a5b8b2" stroke-width="2" stroke-dasharray="5 5" marker-end="url(#' + uid + '-arrow)"/><text x="224" y="272" text-anchor="middle" class="ms-svg-micro">按需取用</text>' +
      '<g class="ms-memory"><rect x="256" y="80" width="588" height="398" rx="14" fill="#fff" stroke="#9abcb0" stroke-width="2"/><text x="280" y="112" class="ms-svg-heading">模型工作内存</text><text x="819" y="111" text-anchor="end" class="ms-svg-small">总窗口 200K</text><text x="280" y="139" class="ms-svg-small">40 格 × 每格 5K；颜色表示不同内容</text><g class="ms-grid"></g><g class="ms-cell-layer"></g><text x="281" y="454" class="ms-svg-small">白格：可以装新材料</text><text x="819" y="454" text-anchor="end" class="ms-svg-small">斜纹 4 格：留 20K 生成回复</text></g>' +
      '<g class="ms-tray"><text x="981" y="124" text-anchor="middle" class="ms-svg-heading">新材料入口</text><text x="981" y="146" text-anchor="middle" class="ms-svg-small">先看有没有足够空位</text><rect x="890" y="181" width="180" height="214" rx="13" fill="#fff8f1" stroke="#d9bb9c" stroke-width="2"/><g class="ms-tray-document"><rect x="925" y="205" width="110" height="128" rx="8" fill="#f9e1cc" stroke="#d39f6a" stroke-width="1.5"/><path d="M943 231 H1016 M943 244 H1005 M943 257 H1016" stroke="#d1a476" stroke-width="3" stroke-linecap="round"/><text x="981" y="295" text-anchor="middle" class="ms-tray-size">30K</text></g><text x="981" y="365" text-anchor="middle" class="ms-tray-caption">暂时没有等待材料</text><g class="ms-stop"><circle cx="869" cy="289" r="17" fill="#fff1ea" stroke="#c77753" stroke-width="2"/><path d="M860 289 H878" stroke="#bd6845" stroke-width="3" stroke-linecap="round"/></g></g>' +
      '<g class="ms-compactor"><path class="ms-compact-route" d="M552 478 L552 501" fill="none" stroke="#6c9e8a" stroke-width="2" stroke-dasharray="5 5" marker-end="url(#' + uid + '-arrow)"/><rect x="412" y="505" width="280" height="82" rx="12" fill="#edf7f1" stroke="#96c1aa" stroke-width="2"/><g class="ms-machine-symbol"><rect x="435" y="526" width="38" height="36" rx="6" fill="#cce7d6"/><path d="M442 535 H466 M442 543 H460 M447 551 H461" stroke="#4c8b70" stroke-width="3" stroke-linecap="round"/></g><text x="490" y="537" class="ms-svg-heading ms-machine-title">摘要整理机</text><text x="490" y="560" class="ms-svg-small ms-machine-caption">保留重点，可能遗漏细节</text></g>' +
      '<g class="ms-reply-bubble"><rect x="820" y="455" width="275" height="70" rx="12" fill="#edf7f7" stroke="#9cc4c5"/><text x="838" y="482" class="ms-svg-small">模型：我会结合这些材料继续。</text><text x="838" y="503" class="ms-svg-small">新回复 5K → 下一轮加入历史</text></g><g class="ms-flying-layer"></g></svg></div>' +
      '<p class="ms-window-note">总窗口 <strong>200K</strong> = 材料上限 <strong>180K</strong> + 回复预留 <strong>20K</strong>（斜纹）；白格才是可用空位。<span>图中合并画 P、V 和新输入，实际发送包 R 仍需组装。</span></p><div class="ms-action" aria-live="off"></div><div class="ms-semantic"></div><div class="ms-detail-links"><button type="button" data-lesson="summary" aria-controls="' + uid + '-lesson" aria-expanded="false">看摘要留了什么</button><button type="button" data-lesson="request" aria-controls="' + uid + '-lesson" aria-expanded="false">摘要请求怎么运行</button><button type="button" data-lesson="mapping" aria-controls="' + uid + '-lesson" aria-expanded="false">对应抽象模型</button></div></div>' +
      '<div class="ms-learning" id="' + uid + '-lesson" tabindex="-1" hidden><div class="ms-learning-header"><h4>把容量变化接回具体内容</h4><button type="button" class="ms-close-lesson">收起，回到动画</button></div><div class="ms-learning-tabs" role="group" aria-label="学习详情"><button type="button" data-lesson="summary">摘要前后</button><button type="button" data-lesson="request">摘要请求</button><button type="button" data-lesson="mapping">抽象对应</button></div><div class="ms-learning-body"></div></div>' +
      '<div class="ms-legend" aria-label="本步内存里的内容"></div><div class="ms-explain" role="status"></div><p class="ms-tip"></p><p class="ms-reduced" hidden>已遵循“减少动态效果”偏好：请使用单步，图形状态仍会完整更新。</p><div class="ms-live" aria-live="polite" aria-atomic="true"></div>';
    function $(s) { return container.querySelector(s); }
    function bind(n, e, fn) { n.addEventListener(e, fn); bindings.push(function () { n.removeEventListener(e, fn); }); }
    var svg = $('.ms-svg'), layer = $('.ms-cell-layer'), grid = $('.ms-grid'), shelf = $('.ms-shelf-files'), playButton = $('.ms-play'), prevButton = $('.ms-prev'), nextButton = $('.ms-next'), resetButton = $('.ms-reset');
    for (var j = 0; j < 40; j++) {
      var x = 279 + (j % 10) * 54, y = 167 + Math.floor(j / 10) * 64;
      grid.appendChild(el('rect', { x: x, y: y, width: 48, height: 55, rx: 5, fill: j >= 36 ? 'url(#' + uid + '-reserve)' : '#f9fbfa', stroke: j >= 36 ? '#c3cdcb' : '#dbe7e1', 'stroke-width': 1, 'stroke-dasharray': j >= 36 ? '' : '3 3', 'data-slot': j }));
    }
    ['任务原文', '文件原文', '工具原文', '对话原文'].forEach(function (label, i) {
      var group = el('g', { 'data-file': i, transform: 'translate(40 ' + (195 + i * 48) + ')' });
      group.appendChild(el('rect', { width: 138, height: 37, rx: 5, fill: ['#e9e3f1', '#f5e8d4', '#f5e8d4', '#e6ece7'][i], stroke: '#cad7cf' }));
      group.appendChild(el('text', { x: 12, y: 24, 'class': 'ms-svg-small' }, label)); shelf.appendChild(group);
    });
    $('.ms-progress').innerHTML = states.map(function (s, i) { return '<button type="button" data-step="' + i + '" aria-label="第 ' + (i + 1) + ' 步：' + esc(s.title) + '"><span>' + (i + 1) + '</span></button>'; }).join('');
    function state() { return { step: index, total: states.length, playing: playing, transitioning: moving, paused: pausedMotion, input: amount(states[index]), used: amount(states[index]), displayedInput: currentValue, free: 180000 - amount(states[index]), pending: states[index].pending, reducedMotion: reduced, phase: moving ? pausedMotion ? 'paused' : 'transition' : 'settled', blocks: states[index].content.map(function (b) { return { id: b.id, tokens: b.tokens }; }) }; }
    var movingCopy = [
      ['正在装入规则和要求', '把做事规则与任务要求带到本轮工作区。'],
      ['正在读取文件', '新文件的 40K 副本正在进入；数字与色块会一起更新。'],
      ['正在累计多轮材料', '正在加入对话、工具结果和近期进展，共增加 100K。'],
      ['正在检查入口', '新结果有 30K；当前只有 10K 空位，先留在入口等待。'],
      ['正在选择摘要材料', '标出旧对话 50K 与旧结果 70K；这时还没有释放空间。'],
      ['正在生成并安装摘要', '旧材料进入摘要请求，候选摘要返回后接替它们；原文仍保存在资料柜。'],
      ['正在接纳等待材料', '空位已经足够，等待的 30K 正在进入本轮工作区。'],
      ['正在进入下一轮', '新回复 5K 成为下一轮历史，加入下一轮输入。']
    ];
    function phaseCaption() {
      var s = states[index], phase = $('.ms-phase-status');
      phase.textContent = moving ? (pausedMotion ? '进行中 · 已暂停' : '进行中') + ' · ' + kfmt(transitionStart) + ' → ' + kfmt(amount(s)) : '第 ' + (index + 1) + ' 步 · 已完成';
      $('.ms-title').textContent = moving ? movingCopy[index][0] : s.title;
      $('.ms-sentence').textContent = moving ? movingCopy[index][1] : s.sentence;
      $('.ms-action').textContent = moving ? (pausedMotion ? '已暂停：' : '') + movingCopy[index][0] + ' · ' + kfmt(transitionStart) + ' → ' + kfmt(amount(s)) : s.action;
      $('.ms-machine-title').textContent = moving && index === 5 ? '候选摘要处理中' : s.compacted ? '摘要 10K 已安装' : '摘要请求';
      $('.ms-machine-caption').textContent = moving && index === 5 ? '成功安装后才释放空间' : s.compacted ? '旧原文仍在资料柜' : index === 4 ? '选中 120K，空间未变' : '保留重点，可能遗漏细节';
      $('.ms-compactor').classList.toggle('ms-machine-done', !!s.compacted && !(moving && index === 5));
      var highlights = (window.MemoryLesson || {}).highlights || {};
      var semantic = moving ? index === 5 ? '候选尚未安装：正在整理根因、安全约束、失败尝试和下一步。' : index === 6 ? '正在带入新材料，随后结合已安装的摘要继续。' : index === 7 ? '正在把新回复加入下一轮历史。' : index === 4 ? '选中旧材料；主工作区仍是 170K，空间尚未释放。' : '' : highlights[index] || '';
      $('.ms-semantic').textContent = semantic; $('.ms-semantic').hidden = !semantic;
    }
    function controls() {
      playButton.textContent = moving ? pausedMotion ? '继续' : '暂停' : playing ? '暂停' : index === 0 ? '播放故事' : index === states.length - 1 ? '再看一遍' : '继续播放';
      playButton.disabled = reduced; playButton.setAttribute('aria-pressed', String(playing || moving && !pausedMotion));
      prevButton.disabled = index === 0 || moving; nextButton.disabled = index === states.length - 1 || moving;
      resetButton.disabled = index === 0 && !moving && !playing;
      $('.ms-step-count').textContent = (index + 1) + ' / ' + states.length;
      $('.ms-reduced').hidden = !reduced;
      container.dataset.step = index; container.dataset.phase = moving ? pausedMotion ? 'paused' : 'transition' : 'settled';
      container.classList.toggle('ms-moving', moving); container.classList.toggle('ms-paused', pausedMotion);
      phaseCaption();
      Array.from($('.ms-progress').children).forEach(function (b, i) { b.classList.toggle('ms-current', i === index); b.classList.toggle('ms-past', i < index); b.setAttribute('aria-current', i === index ? 'step' : 'false'); });
    }
    function readout(value) { currentValue = value; $('.ms-input').textContent = kfmt(value); $('.ms-free').textContent = kfmt(180000 - value); }
    function caption(s) {
      $('.ms-tip').textContent = s.tip; $('.ms-pending').textContent = s.pending ? kfmt(s.pending) : '0K';
      $('.ms-explain').textContent = '点下面的色块名称，了解它占多少空间、为什么保留。';
      $('.ms-legend').innerHTML = s.content.map(function (b) { var c = catalog[b.id]; return '<button type="button" data-type="' + b.id + '"><i style="background:' + c.color + '"></i><span>' + c.name + '</span><strong>' + kfmt(b.tokens) + '</strong></button>'; }).join('');
      var shortNames = {rules:'规则',goal:'要求',oldTools:'旧文件/结果',history:'旧对话',recent:'近期',summary:'摘要',incoming:'新结果',reply:'新回复'};
      $('.ms-mini-key').innerHTML = s.content.map(function(b){return '<span><i style="background:'+catalog[b.id].color+'"></i>'+shortNames[b.id]+'</span>';}).join('');
      shelf.querySelectorAll('[data-file]').forEach(function (f, i) { f.style.opacity = i < s.archive ? '1' : '.22'; });
      $('.ms-compactor').classList.toggle('ms-machine-active', index === 4 || index === 5);
      $('.ms-compactor').classList.toggle('ms-machine-done', !!s.compacted);
      $('.ms-machine-title').textContent = s.compacted ? '摘要 10K 已安装' : '摘要请求';
      $('.ms-machine-caption').textContent = s.compacted ? '旧原文仍在资料柜' : index === 4 ? '选中 120K；整理也需要模型处理' : '保留重点，可能遗漏细节';
      $('.ms-reply-bubble').style.opacity = index === 7 ? '1' : '0';
      $('.ms-detail-links').classList.toggle('ms-details-relevant', index === 4 || index === 5);
      phaseCaption();
    }
    function tray(s, incoming) {
      var value = s.pending || incoming || 0;
      var fits = s.pending > 0 && s.pending <= 180000 - currentValue;
      $('.ms-tray-document').style.opacity = value ? '1' : '.13';
      $('.ms-tray-size').textContent = value ? kfmt(value) : '空';
      $('.ms-mobile-tray-size').textContent = value ? kfmt(value) + (s.pending ? fits ? ' · 可接纳' : ' · 等待' : ' · 正在进入') : '暂无等待材料';
      $('.ms-mobile-tray').classList.toggle('ms-tray-waiting', !!s.pending);
      $('.ms-tray-caption').textContent = s.pending ? fits ? '空位够了，下一步进入' : '等待：先整理出足够空位' : incoming ? '复制到本轮工作区' : '暂时没有等待材料';
      $('.ms-stop').style.opacity = s.pending && !fits ? '1' : '0';
    }
    function renderLesson() {
      var data = window.MemoryLesson || {}, body = $('.ms-learning-body'), html = '';
      function para(value) { return '<p>' + esc(value || '') + '</p>'; }
      function facts(items) { return '<ul class="ms-fact-list">' + (items || []).map(function (item) { return '<li><strong>' + esc(item.title || item.label) + '</strong>' + para(item.text) + '</li>'; }).join('') + '</ul>'; }
      if (lessonTab === 'summary') {
        html = '<p class="ms-teaching-note">' + esc(data.teachingNote || '以下是教学故事的内容摘录；展示摘要取舍，不是实测 token 数。') + '</p><p class="ms-task">' + esc(data.task || '任务：修复登录错误。') + '</p>' +
          '<div class="ms-before-after"><section class="ms-before"><h5>摘要前 · 旧材料摘录</h5>' + facts(data.before) + '</section><section class="ms-after"><h5>' + esc((data.summary || {}).title || '摘要后 · 仍然可见的重点') + '</h5>' + ((data.summary || {}).text ? para(data.summary.text) : facts((data.summary || {}).items)) + '</section></div>' +
          '<section class="ms-omitted"><h5>这些细节没有进摘要</h5>' + facts(data.omitted) + '</section><button type="button" class="ms-recover">' + esc((data.recover || {}).label || '回读资料柜原文') + '</button><div class="ms-recovery" hidden><strong>' + esc((data.recover || {}).source || '原文资料柜') + '</strong>' + para((data.recover || {}).detail) + '<p class="ms-teaching-note">这是教学故事中的回读演示；真实 agent 需要再次读取材料，新读入的内容也占窗口空间。</p></div>';
      } else if (lessonTab === 'request') {
        var request = data.request || {};
        html = '<h5>' + esc(request.title || '摘要也是一个独立计算预算的模型请求') + '</h5><p class="ms-teaching-note">以下拆解故事第 5 → 6 步的固定示例；容量对应这两步，不随当前播放位置变化。</p><p class="ms-teaching-note">' + esc(request.note || '下面是本教学路径中的摘要请求；其窗口单独计算，不从主工作区的空位借用预算。') + '</p>' +
          '<div class="ms-request-input"><strong>带入这个摘要请求的材料</strong><ul>' + (request.input || []).map(function (item) { return '<li>' + esc(item) + '</li>'; }).join('') + '</ul></div><div class="ms-request-budget">' + (request.budget || []).map(function (item) { return '<div><span>' + esc(item.label) + '</span><strong>' + esc(item.value) + '</strong></div>'; }).join('') + '</div>' +
          para(request.formula) + '<ol class="ms-request-pipeline">' + (request.steps || []).map(function (item, i) { return '<li><span>' + (i + 1) + '</span><div><strong>' + esc(item.title.replace(/^\d+\s*·\s*/, '')) + '</strong>' + para(item.text) + '</div></li>'; }).join('') + '</ol><p class="ms-commit-note">摘要返回之前，主工作区仍是 170K。先得到候选摘要，成功安装替换后才降到 60K；失败时不能假装已经释放空间。</p>';
      } else {
        html = '<p class="ms-teaching-note">同一件事的两套语言：故事帮助看过程，符号帮助核对实现。</p><div class="ms-mapping-list">' + (data.mapping || []).map(function (item) { return '<section><strong class="ms-symbol">' + esc(item.symbol) + '</strong><div><h5>' + esc(item.title) + '</h5><p>' + esc(item.meaning) + '</p><p class="ms-mapping-story">图中对应：' + esc(item.story) + '</p></div></section>'; }).join('') + '</div><p class="ms-teaching-note">' + esc(data.scope || '这只是先摘要再接纳的一条教学路径，各框架规则见后面的源码分镜。') + '</p>';
      }
      body.innerHTML = html;
      $('.ms-learning-tabs').querySelectorAll('button').forEach(function (button) { button.classList.toggle('ms-selected-tab', button.dataset.lesson === lessonTab); button.setAttribute('aria-pressed', String(button.dataset.lesson === lessonTab)); });
    }
    function openLesson(tab, trigger) {
      pause(); lessonTab = tab || 'summary'; lessonTrigger = trigger || lessonTrigger;
      $('.ms-learning').hidden = false; renderLesson(); $('.ms-archive-link').setAttribute('aria-expanded', 'true');
      $('.ms-detail-links').querySelectorAll('button').forEach(function (button) { button.setAttribute('aria-expanded', 'true'); });
      $('.ms-learning').scrollIntoView({ block: 'start', behavior: reduced ? 'auto' : 'smooth' }); $('.ms-learning').focus({ preventScroll: true });
    }
    function closeLesson() {
      $('.ms-learning').hidden = true;
      $('.ms-archive-link').setAttribute('aria-expanded', 'false');
      $('.ms-detail-links').querySelectorAll('button').forEach(function (button) { button.setAttribute('aria-expanded', 'false'); });
      $('.ms-stage').scrollIntoView({ block: 'start', behavior: reduced ? 'auto' : 'smooth' });
      if (lessonTrigger && lessonTrigger.isConnected) lessonTrigger.focus({ preventScroll: true });
    }
    function cellNode(c) {
      var g = el('g', { 'class': 'ms-cell', 'data-key': c.key, 'data-type': c.type, role: 'button', tabindex: '0', 'aria-label': catalog[c.type].name + '，每格 5K token' });
      g.style.transform = 'translate(' + c.x + 'px,' + c.y + 'px)';
      g.appendChild(el('rect', { width: 48, height: 55, rx: 5, fill: catalog[c.type].color, stroke: '#ffffff', 'stroke-width': 1.5 }));
      g.appendChild(el('path', { d: 'M11 14 H35 M11 22 H29', stroke: '#ffffff', 'stroke-width': 2, opacity: '.3', 'stroke-linecap': 'round' }));
      g.appendChild(el('title', {}, catalog[c.type].name + ' · 一格 5K · ' + catalog[c.type].note));
      return g;
    }
    function animate(n, frames, opt) {
      if (reduced || !n.animate) return null;
      var a = n.animate(frames, Object.assign({ duration: 1700, easing: 'cubic-bezier(.3,.65,.2,1)', fill: 'backwards' }, opt || {}));
      active.add(a); a.finished.then(function () { try { a.commitStyles(); } catch (_) { var last = frames[frames.length - 1]; Object.keys(last).forEach(function (key) { if (key !== 'offset' && key !== 'easing' && key !== 'composite') n.style[key] = last[key]; }); } a.cancel(); active.delete(a); }, function () { active.delete(a); }); return a;
    }
    function stopTimer() { if (timer != null) clearTimeout(timer); timer = null; }
    function cancelMotion() { generation++; active.forEach(function (a) { a.cancel(); }); active.clear(); if (raf != null) cancelAnimationFrame(raf); raf = null; clock = null; layer.querySelectorAll('.ms-cell-ghost').forEach(function (g) { g.remove(); }); moving = false; pausedMotion = false; }
    function schedule() { stopTimer(); if (!playing || disposed || reduced || document.hidden || moving) return; if (index >= states.length - 1) { playing = false; controls(); return; } timer = setTimeout(function () { transition(index + 1, true); }, index === 0 ? 1350 : 1700); }
    function numberMotion(from, to, delay, duration) {
      if (from === to) return;
      clock = animate($('.ms-input'), [{ opacity: 1 }, { opacity: 1 }], { duration: duration + delay, easing: 'linear' });
      var observed = clock;
      function tick() { if (!clock || clock !== observed || disposed) return; var t = Math.max(0, Math.min(1, ((clock.currentTime || 0) - delay) / duration)); readout(from + (to - from) * t); if (t < 1) raf = requestAnimationFrame(tick); }
      raf = requestAnimationFrame(tick);
    }
    function transition(target, fromPlay, opts) {
      opts = opts || {}; target = Math.max(0, Math.min(states.length - 1, Number(target) || 0));
      stopTimer(); if (!fromPlay) playing = false;
      var before = states[index], fromIndex = index, oldCells = cellsFor(before), oldMap = new Map(oldCells.map(function (c) { return [c.key, c]; }));
      cancelMotion(); var token = generation; index = target; var s = states[index], nextCells = cellsFor(s), nextMap = new Map(nextCells.map(function (c) { return [c.key, c]; }));
      var noMotion = reduced || opts.instant || fromIndex === target, prevValue = currentValue; transitionStart = prevValue;
      caption(s); tray(s, index === 1 ? 40000 : index === 2 ? 100000 : 0);
      var nodes = new Map(); layer.querySelectorAll('.ms-cell:not(.ms-cell-ghost)').forEach(function (n) { nodes.set(n.dataset.key, n); });
      var removals = [], additions = [], movements = [];
      nodes.forEach(function (n, key) { if (!nextMap.has(key)) { if (!noMotion) { n.classList.add('ms-cell-ghost'); n.removeAttribute('tabindex'); n.setAttribute('aria-hidden', 'true'); removals.push({ n: n, c: oldMap.get(key) }); } else n.remove(); } });
      nextCells.forEach(function (c) {
        var n = nodes.get(c.key), old = oldMap.get(c.key);
        if (!n) { n = cellNode(c); layer.appendChild(n); additions.push({ n: n, c: c }); }
        n.style.transform = 'translate(' + c.x + 'px,' + c.y + 'px)';
        n.classList.toggle('ms-selected-cell', !!s.selected && (c.type === 'oldTools' || c.type === 'history'));
        if (old && !noMotion && (old.x !== c.x || old.y !== c.y)) movements.push({ n: n, old: old, c: c });
      });
      moving = !noMotion; pausedMotion = false; controls();
      if (noMotion) {
        readout(amount(s)); tray(s); $('.ms-live').textContent = '第 ' + (index + 1) + ' 步：' + s.title + '。' + s.sentence; emit(); schedule(); movementDone = Promise.resolve(state()); return movementDone;
      }
      var duration = 1900;
      removals.forEach(function (r, i) {
        var c = r.c || { x: 550, y: 280 };
        var toMachine = index === 5 || index === 6 || index === 7;
        animate(r.n, [{ transform: 'translate(' + c.x + 'px,' + c.y + 'px) scale(1)', opacity: 1, offset: 0 }, { transform: 'translate(' + (c.x + (550 - c.x) * .35) + 'px,' + (c.y + (515 - c.y) * .15) + 'px) scale(.9)', opacity: 1, offset: .28 }, { transform: 'translate(' + (toMachine ? 530 + (i % 4) * 8 : 100) + 'px,' + (toMachine ? 525 : 250) + 'px) scale(.16)', opacity: .85, offset: .82 }, { transform: 'translate(550px,535px) scale(.05)', opacity: 0, offset: 1 }], { duration: 1500, delay: (i % 7) * 35 });
      });
      movements.forEach(function (m, i) { animate(m.n, [{ transform: 'translate(' + m.old.x + 'px,' + m.old.y + 'px)' }, { transform: 'translate(' + m.c.x + 'px,' + m.c.y + 'px)' }], { duration: 1400, delay: removals.length ? 850 : i * 12 }); });
      additions.forEach(function (a, i) {
        var source = a.c.type === 'summary' ? { x: 535, y: 525 } : a.c.type === 'reply' ? compactLayout ? { x: 780, y: 475 } : { x: 950, y: 460 } : compactLayout ? { x: 780 + (i % 3) * 4, y: 148 + (i % 4) * 6 } : { x: 950 + (i % 3) * 4, y: 236 + (i % 4) * 6 };
        var delay = a.c.type === 'summary' && removals.length ? 1250 + i * 90 : i * Math.min(55, 560 / Math.max(1, additions.length));
        var d = a.c.type === 'summary' ? 1050 : 1500;
        duration = Math.max(duration, delay + d);
        animate(a.n, [{ transform: 'translate(' + source.x + 'px,' + source.y + 'px) scale(.65)', opacity: 0, offset: 0 }, { transform: 'translate(' + (source.x + (a.c.x - source.x) * .28) + 'px,' + (Math.min(source.y, a.c.y) - 36) + 'px) scale(.9)', opacity: 1, offset: .35 }, { transform: 'translate(' + a.c.x + 'px,' + a.c.y + 'px) scale(1)', opacity: 1, offset: 1 }], { duration: d, delay: delay });
      });
      if (index === 3) animate($('.ms-tray-document'), [{ transform: 'translateX(0)' }, { transform: 'translateX(-18px)', offset: .38 }, { transform: 'translateX(0)', offset: .6 }, { transform: 'translateX(-8px)', offset: .78 }, { transform: 'translateX(0)' }], { duration: 1850, easing: 'ease-in-out' });
      if (index === 4) layer.querySelectorAll('.ms-selected-cell').forEach(function (n, i) { animate(n.querySelector('rect'), [{ opacity: 1, strokeWidth: '1.5px' }, { opacity: .55, strokeWidth: '4px', offset: .45 }, { opacity: 1, strokeWidth: '1.5px' }], { duration: 1600, delay: (i % 4) * 70 }); });
      if (index === 5) animate($('.ms-machine-symbol'), [{ transform: 'translateY(0)' }, { transform: 'translateY(-5px)', offset: .25 }, { transform: 'translateY(3px)', offset: .5 }, { transform: 'translateY(-4px)', offset: .75 }, { transform: 'translateY(0)' }], { duration: 1850 });
      if (index === 7) animate($('.ms-reply-bubble'), [{ opacity: 0, transform: 'translateY(12px)' }, { opacity: 1, transform: 'translateY(0)' }], { duration: 1500 });
      numberMotion(prevValue, amount(s), removals.length ? 1000 : 350, removals.length ? 1250 : 1200);
      // A real timeline, also paused by the same player controls, bounds each transition.
      var sentinel = animate($('.ms-action'), [{ opacity: 1 }, { opacity: 1 }], { duration: Math.max(duration, removals.length ? 2450 : 1900), easing: 'linear' });
      var wait = Array.from(active).map(function (a) { return a.finished.catch(function () {}); });
      emit();
      movementDone = Promise.all(wait).then(function () {
        if (disposed || generation !== token) return state();
        layer.querySelectorAll('.ms-cell-ghost').forEach(function (n) { n.remove(); });
        moving = false; pausedMotion = false; clock = null; if (raf != null) cancelAnimationFrame(raf); raf = null;
        readout(amount(s)); tray(s); controls(); $('.ms-live').textContent = '第 ' + (index + 1) + ' 步：' + s.title + '。输入 ' + kfmt(amount(s)) + '，空位 ' + kfmt(180000 - amount(s)) + '。';
        emit(); schedule(); return state();
      });
      return movementDone;
    }
    function emit() { container.dispatchEvent(new CustomEvent('memorystorychange', { detail: state() })); }
    function pause() { stopTimer(); playing = false; if (moving) { active.forEach(function (a) { if (a.playState === 'running') a.pause(); }); pausedMotion = true; } controls(); emit(); }
    function play() {
      if (reduced || disposed) return;
      playing = true;
      if (moving && pausedMotion) { pausedMotion = false; active.forEach(function (a) { if (a.playState === 'paused') a.play(); }); controls(); emit(); return; }
      if (moving) { controls(); return; }
      if (index === states.length - 1) transition(0, true, { instant: true }); else { controls(); schedule(); }
      emit();
    }
    bind(playButton, 'click', function () { if (moving && !pausedMotion || playing) pause(); else { $('.ms-stage').scrollIntoView({ block: 'start', behavior: reduced ? 'auto' : 'smooth' }); play(); } });
    bind(prevButton, 'click', function () { transition(index - 1, false, { instant: true }); }); bind(nextButton, 'click', function () { transition(index + 1, false); }); bind(resetButton, 'click', function () { transition(0, false, { instant: true }); });
    bind($('.ms-progress'), 'click', function (e) { var b = e.target.closest('button[data-step]'); if (b) transition(+b.dataset.step, false, { instant: true }); });
    bind($('.ms-legend'), 'click', function (e) { var b = e.target.closest('button[data-type]'); if (b) { var type = b.dataset.type; $('.ms-explain').textContent = catalog[type].note; layer.querySelectorAll('.ms-cell').forEach(function (n) { n.classList.toggle('ms-inspect-cell', n.dataset.type === type); }); if (type === 'summary') openLesson('summary', b); } });
    bind($('.ms-detail-links'), 'click', function (e) { var b = e.target.closest('button[data-lesson]'); if (b) openLesson(b.dataset.lesson, b); });
    bind($('.ms-learning-tabs'), 'click', function (e) { var b = e.target.closest('button[data-lesson]'); if (b) { lessonTab = b.dataset.lesson; renderLesson(); } });
    bind($('.ms-close-lesson'), 'click', closeLesson);
    bind($('.ms-archive-link'), 'click', function (e) { openLesson('summary', e.currentTarget); });
    bind($('.ms-learning-body'), 'click', function (e) { if (e.target.closest('.ms-recover')) { var panel = $('.ms-recovery'); panel.hidden = !panel.hidden; e.target.closest('.ms-recover').setAttribute('aria-expanded', String(!panel.hidden)); if (!panel.hidden) panel.scrollIntoView({ block: 'nearest', behavior: reduced ? 'auto' : 'smooth' }); } });
    function explainCell(e) { var n = e.target.closest('[data-type]'); if (n && catalog[n.dataset.type]) { $('.ms-explain').textContent = catalog[n.dataset.type].note; if (n.dataset.type === 'summary') openLesson('summary', n); } }
    bind(layer, 'click', explainCell); bind(layer, 'keydown', function (e) { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); explainCell(e); } });
    bind(document, 'visibilitychange', function () { if (document.hidden) pause(); });
    var motionChange = function (e) { reduced = e.matches; if (reduced) { playing = false; stopTimer(); active.forEach(function (a) { a.finish(); }); } controls(); };
    mq.addEventListener('change', motionChange); bindings.push(function () { mq.removeEventListener('change', motionChange); });
    if ('IntersectionObserver' in window) { var observer = new IntersectionObserver(function (entries) { if (!entries[0].isIntersecting && (playing || moving && !pausedMotion)) pause(); }); observer.observe(container); bindings.push(function () { observer.disconnect(); }); }
    function responsive(){compactLayout=container.clientWidth<680;container.dataset.layout=compactLayout?'compact':'wide';svg.setAttribute('viewBox',compactLayout?'250 158 600 445':'0 70 1120 530');}
    if('ResizeObserver' in window){var resize=new ResizeObserver(responsive);resize.observe(container);bindings.push(function(){resize.disconnect();});}else bind(window,'resize',responsive);responsive();
    var api = { getState: state, goTo: function (i, options) { return transition(i, false, options); }, next: function () { return transition(index + 1, false); }, prev: function () { return transition(index - 1, false); }, play: play, pause: pause, destroy: function () { if (disposed) return; stopTimer(); cancelMotion(); disposed = true; bindings.forEach(function (fn) { fn(); }); container.innerHTML = ''; container.classList.remove('memory-story'); delete container.__memoryStory; } };
    container.__memoryStory = api; transition(0, false, { instant: true }); return api;
  }
  window.MemoryStory = { mount: mount };
})();
