(function () {
  'use strict';
  var instanceCount = 0;
  var kinds = { system: '系统', rule: '规则', schema: '工具定义', user: '用户', tool: '工具结果', assistant: '助手', summary: '摘要', opaque: '不透明项' };
  var statuses = { protected: '保护', kept: '保留', new: '新增', removed: '移出工作集', changed: '改写' };
  var laneNames = { archive: '持久记录', request: '当前模型工作集', output: '本轮发送包' };
  function esc(value) { return String(value == null ? '' : value).replace(/[&<>"']/g, function (c) { return ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]; }); }
  function fmt(n) { return typeof n === 'number' && Number.isFinite(n) ? Math.round(n).toLocaleString('en-US') : '未知'; }
  function prose(value) { return Array.isArray(value) ? value.map(prose).join('；') : value && typeof value === 'object' ? (value.title || value.text || JSON.stringify(value)) : String(value == null ? '' : value); }
  function lines(value) { return Array.isArray(value) ? value.map(prose).join('\n') : prose(value); }
  function safeURL(value) { if (typeof value !== 'string' || !/^https?:\/\//i.test(value.trim())) return ''; try { var u = new URL(value.trim()); return /^https?:$/.test(u.protocol) ? u.href : ''; } catch (_) { return ''; } }
  function array(value) { return Array.isArray(value) ? value : []; }

  function mount(container, frameworks) {
    if (typeof container === 'string') container = document.querySelector(container);
    if (!container || !array(frameworks).length) throw new Error('ContextAnimation.mount requires a container and non-empty framework data.');
    if (container.__contextAnimation) container.__contextAnimation.destroy();
    var uid = 'ca-' + (++instanceCount), fi = 0, si = 0, index = 0, playing = false, speed = 1, timer = null, disposed = false;
    var reducedQuery = window.matchMedia ? window.matchMedia('(prefers-reduced-motion: reduce)') : null;
    var reduced = !!(reducedQuery && reducedQuery.matches), intersecting = true;
    var observers = [], animations = new Set();
    container.classList.add('context-animation');
    container.innerHTML = '<div class="ca-selector-row"><label for="' + uid + '-framework">选择框架<select id="' + uid + '-framework" class="ca-framework"></select></label><label for="' + uid + '-scenario">选择过程<select id="' + uid + '-scenario" class="ca-scenario"></select></label><p class="ca-teaching-label">按规则逐步演示<br><span>数字为教学样本，不是实际模型运行</span></p></div>' +
      '<p class="ca-assumptions"></p>' +
      '<div class="ca-player"><div class="ca-controls"><button type="button" class="ca-play">播放演示</button><button type="button" class="ca-prev" aria-label="上一步">上一步</button><button type="button" class="ca-next" aria-label="下一步">下一步</button><button type="button" class="ca-reset">重置</button><label for="' + uid + '-speed" class="ca-speed-label">速度<select id="' + uid + '-speed" class="ca-speed"><option value="0.65">0.65×</option><option value="1" selected>1×</option><option value="1.5">1.5×</option><option value="2">2×</option></select></label><span class="ca-counter"></span></div>' +
      '<div class="ca-scrub"><label class="ca-sr-only" for="' + uid + '-progress">演示进度，按左右方向键逐步查看</label><input type="range" id="' + uid + '-progress" class="ca-progress" min="0" step="1" value="0"><span class="ca-progress-caption">拖动或点击下方步骤，查看任意中间状态</span></div>' +
      '<div class="ca-timeline" role="group" aria-label="过程步骤"></div>' +
      '<div class="ca-stage-grid"><div class="ca-stage"><div class="ca-budget"><div class="ca-budget-label"><strong>工作集预算</strong><span class="ca-budget-numbers"></span></div><div class="ca-budget-track" aria-hidden="true"><div class="ca-budget-fill"></div><i class="ca-budget-mark"></i></div><div class="ca-budget-legend"><span>青色：本步工作集</span><span>橙线：本场景触发线</span><span class="ca-budget-verdict"></span></div></div>' +
      '<div class="ca-lane ca-lane-archive ca-archive-collapsed"><div class="ca-lane-heading"><b>01　持久记录</b><span>保存下来的记录，不等于每次都发送</span><button type="button" class="ca-archive-toggle" aria-expanded="false">展开记录</button></div><p class="ca-archive-summary"></p><div class="ca-blocks" role="list" aria-label="持久记录中的内容"></div><p class="ca-empty">本步骤不展示持久记录</p></div>' +
      '<div class="ca-projection-note"><span>↓ 选择、投影或重建</span><span>同一内容可以同时存在于多层；不是从磁盘搬走</span></div>' +
      '<div class="ca-lane ca-lane-request"><div class="ca-lane-heading"><b>02　本地模型工作集</b><span>发送前的投影：观察保留、移除与摘要替换</span></div><div class="ca-blocks" role="list" aria-label="本地模型工作集中的内容"></div><p class="ca-empty">本步骤尚未组装工作集</p></div>' +
      '<div class="ca-operations" aria-label="本步变换"></div>' +
      '<div class="ca-lane ca-lane-output"><div class="ca-lane-heading"><b>03　本轮 provider 发送包</b><span>适配器转换后的发送结构示意</span></div><div class="ca-blocks" role="list" aria-label="本轮 provider 发送包中的内容"></div><p class="ca-empty">本步骤尚未向模型发送</p></div>' +
      '<div class="ca-key"><span class="ca-key-protected">保护</span><span class="ca-key-new">新增</span><span class="ca-key-summary">摘要 / 替代</span><span class="ca-key-removed">移出当前工作集</span><span>块面积不代表精确 token 比例</span></div></div>' +
      '<aside class="ca-inspector" aria-label="当前步骤解释"><div class="ca-step-kicker"></div><h3 class="ca-step-title"></h3><p class="ca-step-explain"></p><dl class="ca-step-details"><dt>本步动作</dt><dd class="ca-step-action"></dd><dt>对应规则</dt><dd class="ca-step-rule"></dd></dl><div class="ca-code-wrap"><div class="ca-code-label">实现逻辑 / 伪代码</div><pre class="ca-step-code"></pre></div><div class="ca-sources-wrap"><strong>核对源码</strong><div class="ca-step-sources"></div></div></aside></div></div>' +
      '<div class="ca-live ca-sr-only" aria-live="polite" aria-atomic="true"></div><p class="ca-motion-note" hidden>已遵循“减少动态效果”偏好：使用单步操作，自动播放已关闭。</p>';

    function $(selector) { return container.querySelector(selector); }
    var capacity=window.ContextCapacity?ContextCapacity.mount(container):null;
    var frameworkSelect = $('.ca-framework'), scenarioSelect = $('.ca-scenario'), play = $('.ca-play'), prev = $('.ca-prev'), next = $('.ca-next'), reset = $('.ca-reset'), progress = $('.ca-progress'), timeline = $('.ca-timeline');
    frameworkSelect.innerHTML = frameworks.map(function (f) { return '<option value="' + esc(f.id) + '">' + esc(f.name) + '</option>'; }).join('');
    function framework() { return frameworks[fi]; }
    function scenario() { return array(framework().scenarios)[si] || { id: '', title: '', steps: [] }; }
    function steps() { return array(scenario().steps); }
    function current() { return steps()[index] || {}; }
    function state() { return { framework: framework().id, scenario: scenario().id, step: index, total: steps().length, playing: playing, speed: speed, reducedMotion: reduced }; }
    function bind(el, event, fn) { el.addEventListener(event, fn); observers.push(function () { el.removeEventListener(event, fn); }); }
    function cancelTimer() { if (timer !== null) window.clearTimeout(timer); timer = null; }
    function pause() { playing = false; cancelTimer(); updateControls(); }
    function updateControls() {
      var last = index >= steps().length - 1;
      play.textContent = playing ? '暂停' : last && steps().length > 1 ? '重新播放' : '播放演示';
      play.setAttribute('aria-pressed', String(playing)); play.disabled = reduced || steps().length < 2;
      prev.disabled = index <= 0; next.disabled = last;
      reset.disabled = index === 0 && !playing;
      $('.ca-counter').textContent = (steps().length ? index + 1 : 0) + ' / ' + steps().length;
      progress.max = String(Math.max(0, steps().length - 1)); progress.value = String(index); progress.disabled = steps().length < 2;
      progress.setAttribute('aria-valuetext', '第 ' + (index + 1) + ' 步：' + prose(current().title));
      $('.ca-motion-note').hidden = !reduced;
      container.classList.toggle('ca-playing', playing);
    }
    function schedule() {
      cancelTimer();
      if (!playing || disposed || reduced || !intersecting || document.hidden) return;
      timer = window.setTimeout(function () {
        if (index < steps().length - 1) render(index + 1, true);
        if (index >= steps().length - 1) pause(); else schedule();
      }, Math.max(750, Number(current().duration || 3000) / speed));
    }
    function animate(el, keyframes, options) {
      if (reduced || !el.animate) return null;
      try {
        var a = el.animate(keyframes, Object.assign({ duration: 480 / speed, easing: 'cubic-bezier(.22,.8,.25,1)' }, options || {}));
        animations.add(a); a.finished.then(function () { animations.delete(a); }, function () { animations.delete(a); }); return a;
      } catch (_) { return null; }
    }
    function cardContent(block) {
      var status = statuses[block.status] || '', kind = kinds[block.kind] || (block.kind ? String(block.kind) : '内容');
      var tokens = typeof block.tokens === 'number' ? fmt(block.tokens) + ' t' : block.tokens === null ? 'token 未知' : '';
      return '<div class="ca-block-type">' + esc(kind) + (status ? '<span>' + esc(status) + '</span>' : '') + '</div><div class="ca-block-label">' + esc(block.label || block.id) + '</div>' + (block.detail ? '<div class="ca-block-detail">' + esc(prose(block.detail)) + '</div>' : '') + '<div class="ca-block-tokens">' + esc(tokens) + '</div>';
    }
    function renderLane(name, list, skipMotion) {
      var lane = $('.ca-lane-' + name), box = lane.querySelector('.ca-blocks'), old = new Map(), boxes = new Map();
      Array.from(box.children).forEach(function (node) { old.set(node.dataset.key, node); boxes.set(node.dataset.key, node.getBoundingClientRect()); });
      var keys = new Set(), fragment = document.createDocumentFragment();
      array(list).filter(function (block) { return block && typeof block === 'object'; }).forEach(function (block, j) {
        var key = String(block.id == null ? j : block.id); if (keys.has(key)) key += ':' + j; keys.add(key);
        var node = old.get(key), fresh = !node;
        if (!node) { node = document.createElement('div'); node.dataset.key = key; node.setAttribute('role', 'listitem'); }
        node.className = 'ca-block ca-kind-' + String(block.kind || 'assistant').replace(/[^a-z0-9_-]/gi, '') + ' ca-status-' + String(block.status || 'kept').replace(/[^a-z0-9_-]/gi, '');
        var content = cardContent(block); if (node.innerHTML !== content) node.innerHTML = content;
        var identity = prose(block.label || block.id) + '\nID: ' + key + '\n类型: ' + (kinds[block.kind] || block.kind || '内容') + '\n所在层: ' + laneNames[name] + (statuses[block.status] ? '\n状态: ' + statuses[block.status] : '') + (typeof block.tokens === 'number' ? '\n教学样本: ' + fmt(block.tokens) + ' token' : block.tokens === null ? '\ntoken 未知' : '');
        node.title = identity;
        node.setAttribute('aria-label', identity.replace(/\n/g, '；'));
        node.dataset.fresh = fresh ? 'true' : 'false'; fragment.appendChild(node);
      });
      old.forEach(function (node, key) {
        if (keys.has(key)) return;
        if (!skipMotion && !reduced) {
          var rect = boxes.get(key), parent = lane.getBoundingClientRect(), ghost = node.cloneNode(true);
          ghost.classList.add('ca-ghost'); ghost.removeAttribute('role'); ghost.setAttribute('aria-hidden', 'true');
          ghost.style.cssText = 'position:absolute;left:' + (rect.left - parent.left) + 'px;top:' + (rect.top - parent.top) + 'px;width:' + rect.width + 'px;height:' + rect.height + 'px;pointer-events:none;z-index:4;';
          lane.appendChild(ghost);
          var animation = animate(ghost, [{ opacity: .9, transform: 'translate(0,0) scale(1)' }, { opacity: 0, transform: 'translate(0,-22px) scale(.88)' }], { duration: 520 / speed });
          if (animation) animation.finished.then(function () { ghost.remove(); }, function () { ghost.remove(); }); else ghost.remove();
        }
        node.remove();
      });
      box.appendChild(fragment); lane.querySelector('.ca-empty').hidden = array(list).length > 0;
      if (!skipMotion) Array.from(box.children).forEach(function (node) {
        var before = boxes.get(node.dataset.key), after = node.getBoundingClientRect();
        if (before) {
          var x = before.left - after.left, y = before.top - after.top;
          if (Math.abs(x) > 1 || Math.abs(y) > 1) animate(node, [{ transform: 'translate(' + x + 'px,' + y + 'px)' }, { transform: 'translate(0,0)' }]);
        } else {
          animate(node, [{ opacity: 0, transform: 'translateY(20px) scale(.93)' }, { opacity: 1, transform: 'translateY(0) scale(1)' }]);
        }
      });
    }
    function renderBudget(step) {
      var usage = typeof step.usage === 'number' ? step.usage : null, threshold = typeof step.threshold === 'number' ? step.threshold : null, limit = typeof step.window === 'number' && step.window > 0 ? step.window : null;
      $('.ca-budget-numbers').textContent = '工作集 ' + fmt(usage) + '　/　窗口 ' + fmt(limit) + ' t';
      $('.ca-budget-fill').style.width = usage !== null && limit ? Math.min(100, Math.max(0, usage / limit * 100)) + '%' : '0%';
      $('.ca-budget-fill').classList.toggle('ca-over', usage !== null && limit !== null && usage > limit);
      $('.ca-budget-mark').hidden = !limit || threshold === null;
      $('.ca-budget-mark').style.left = limit && threshold !== null ? Math.min(100, Math.max(0, threshold / limit * 100)) + '%' : '0%';
      $('.ca-budget-verdict').textContent = prose(step.verdict) || (threshold === null ? '本步不作阈值判断' : '触发线 ' + fmt(threshold) + ' t');
      $('.ca-budget').classList.toggle('ca-unknown-budget', usage === null || limit === null);
    }
    function render(newIndex, fromPlayback, skipMotion) {
      if (disposed) return;
      if (!fromPlayback) pause();
      index = Math.max(0, Math.min(Math.max(0, steps().length - 1), Number(newIndex) || 0));
      var step = current();
      $('.ca-archive-summary').textContent = '此分镜保留 ' + array(step.archive).length + ' 个原记录 / 产物对象；工作集回收不会把它们从这里搬走。';
      $('.ca-archive-toggle').textContent = $('.ca-lane-archive').classList.contains('ca-archive-collapsed') ? '展开记录（'+array(step.archive).length+'）' : '收起记录';
      ['archive', 'request', 'output'].forEach(function (name) { renderLane(name, step[name], !!skipMotion); });
      $('.ca-step-kicker').textContent = framework().name + ' · ' + (index + 1) + '/' + steps().length;
      $('.ca-step-title').textContent = prose(step.title);
      $('.ca-step-explain').textContent = prose(step.explain);
      $('.ca-step-action').textContent = prose(step.action) || '观察本步上下文结构';
      $('.ca-step-rule').textContent = prose(step.rule) || '请结合下方源码核对适用条件';
      $('.ca-step-code').textContent = lines(step.code); $('.ca-code-wrap').hidden = !step.code;
      $('.ca-step-sources').innerHTML = array(step.sources).filter(Boolean).map(function (source) { if (typeof source !== 'object') return '<span>' + esc(source) + '</span>'; var url = safeURL(source.url); return url ? '<a href="' + esc(url) + '" target="_blank" rel="noopener">' + esc(source.label || '查看源码') + '<span aria-hidden="true"> ↗</span></a>' : '<span>' + esc(source.label || '') + '</span>'; }).join('');
      $('.ca-sources-wrap').hidden = !array(step.sources).length;
      $('.ca-operations').innerHTML = array(step.operations).map(function (op) { return '<span class="ca-operation ca-op-' + esc(String(op.type || 'change').replace(/[^a-z0-9_-]/gi, '')) + '">' + esc(op.label || [op.from, op.to].filter(Boolean).join(' → ') || op.type) + '</span>'; }).join('');
      $('.ca-operations').hidden = !array(step.operations).length;
      renderBudget(step); if(capacity)capacity.render(step,!!skipMotion); updateControls();
      Array.from(timeline.children).forEach(function (button, j) { button.classList.toggle('ca-active', j === index); button.classList.toggle('ca-past', j < index); if (j === index) button.setAttribute('aria-current', 'step'); else button.removeAttribute('aria-current'); });
      var active = timeline.children[index];
      if (active && (active.offsetLeft < timeline.scrollLeft || active.offsetLeft + active.offsetWidth > timeline.scrollLeft + timeline.clientWidth)) timeline.scrollTo({ left: Math.max(0, active.offsetLeft - 15), behavior: reduced ? 'auto' : 'smooth' });
      if (!fromPlayback) $('.ca-live').textContent = '第 ' + (index + 1) + ' 步，' + prose(step.title) + '。' + prose(step.action);
      container.dispatchEvent(new CustomEvent('contextanimationchange', { detail: state() }));
    }
    function rebuildScenario() {
      pause(); index = 0;
      $('.ca-assumptions').textContent = lines(scenario().assumptions);
      $('.ca-assumptions').hidden = !scenario().assumptions;
      timeline.innerHTML = steps().map(function (step, j) { return '<button type="button" class="ca-timeline-step" data-index="' + j + '" title="' + esc(step.title) + '"><span>' + (j + 1) + '</span><b>' + esc(step.title) + '</b></button>'; }).join('');
      render(0, false, true);
    }
    function rebuildFramework() {
      si = 0;
      scenarioSelect.innerHTML = array(framework().scenarios).map(function (s) { return '<option value="' + esc(s.id) + '">' + esc(s.title) + '</option>'; }).join('');
      scenarioSelect.disabled = array(framework().scenarios).length < 2;
      rebuildScenario();
    }
    bind(frameworkSelect, 'change', function () { fi = Math.max(0, frameworks.findIndex(function (f) { return f.id === frameworkSelect.value; })); rebuildFramework(); });
    bind(scenarioSelect, 'change', function () { si = Math.max(0, array(framework().scenarios).findIndex(function (s) { return s.id === scenarioSelect.value; })); rebuildScenario(); });
    bind($('.ca-archive-toggle'), 'click', function(){var collapsed=$('.ca-lane-archive').classList.toggle('ca-archive-collapsed');$('.ca-archive-toggle').setAttribute('aria-expanded',String(!collapsed));$('.ca-archive-toggle').textContent=collapsed?'展开记录（'+array(current().archive).length+'）':'收起记录';});
    bind(play, 'click', function () { if (playing) { pause(); return; } if (reduced || steps().length < 2) return; if (index >= steps().length - 1) render(0, false); playing = true; updateControls(); schedule(); });
    bind(prev, 'click', function () { render(index - 1, false); }); bind(next, 'click', function () { render(index + 1, false); }); bind(reset, 'click', function () { render(0, false); });
    bind(progress, 'input', function () { render(+progress.value, false); });
    bind($('.ca-speed'), 'change', function (e) { speed = Number(e.target.value) || 1; if (playing) schedule(); });
    bind(timeline, 'click', function (e) { var button = e.target.closest('button[data-index]'); if (button && timeline.contains(button)) render(+button.dataset.index, false); });
    bind(container, 'keydown', function (e) {
      if (!e.altKey || e.ctrlKey || e.metaKey || /^(INPUT|SELECT|TEXTAREA)$/.test(e.target.tagName)) return;
      if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') { e.preventDefault(); render(index + (e.key === 'ArrowRight' ? 1 : -1), false); }
    });
    bind(document, 'visibilitychange', function () { if (document.hidden) pause(); });
    if (reducedQuery) {
      var motionChanged = function (e) { reduced = e.matches; if (reduced) { pause(); animations.forEach(function (a) { a.finish(); }); } updateControls(); };
      if (reducedQuery.addEventListener) { reducedQuery.addEventListener('change', motionChanged); observers.push(function () { reducedQuery.removeEventListener('change', motionChanged); }); }
    }
    if ('IntersectionObserver' in window) {
      var io = new IntersectionObserver(function (entries) { intersecting = entries[0].isIntersecting; if (!intersecting && playing) pause(); }, { threshold: 0 }); io.observe(container); observers.push(function () { io.disconnect(); });
    }
    var api = {
      getState: state,
      setFramework: function (id) { var pos = frameworks.findIndex(function (f) { return f.id === id; }); if (pos < 0) throw new Error('Unknown framework: ' + id); fi = pos; frameworkSelect.value = id; rebuildFramework(); },
      setScenario: function (id) { var pos = array(framework().scenarios).findIndex(function (s) { return s.id === id; }); if (pos < 0) throw new Error('Unknown scenario: ' + id); si = pos; scenarioSelect.value = id; rebuildScenario(); },
      goTo: function (i) { render(i, false); },
      destroy: function () { if (disposed) return; pause(); disposed = true; if(capacity)capacity.destroy(); observers.forEach(function (fn) { fn(); }); animations.forEach(function (a) { a.cancel(); }); animations.clear(); container.innerHTML = ''; container.classList.remove('context-animation'); delete container.__contextAnimation; }
    };
    container.__contextAnimation = api; rebuildFramework(); return api;
  }
  window.ContextAnimation = { mount: mount };
})();
