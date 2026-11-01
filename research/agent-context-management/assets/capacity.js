(function () {
  'use strict';
  var palette={rules:'#577dba',user:'#9d7ec4',tool:'#dc9b46',assistant:'#8b9aab',summary:'#24997f',other:'#9aa6ad'};
  var names={rules:'规则与工具定义',user:'用户 / 输入内容',tool:'文件与工具结果',assistant:'助手与调用历史',summary:'摘要',other:'其他已知内容'};
  var sequence=['rules','user','assistant','tool','summary','other'];
  function mount(container) {
    var target=document.createElement('div');target.className='ca-capacity';
    target.innerHTML='<p class="ca-capacity-title">把本步工作集放到容量图里</p><div class="ca-capacity-svg"></div><div class="ca-capacity-legend"></div><p class="ca-capacity-note"></p><button class="ca-content-toggle" type="button" aria-expanded="false">展开对象明细</button>';
    var request=container.querySelector('.ca-lane-request');request.insertBefore(target,request.querySelector('.ca-blocks'));request.classList.add('ca-content-collapsed');
    var button=target.querySelector('button');button.addEventListener('click',function(){var c=request.classList.toggle('ca-content-collapsed');button.setAttribute('aria-expanded',String(!c));button.textContent=c?'展开对象明细':'收起对象明细';});
    var prior=null,frame=0,uid='capacity-'+Math.random().toString(36).slice(2),svg=target.querySelector('.ca-capacity-svg');
    function fmt(n){return Math.round(n).toLocaleString('en-US');}
    function paint(values,used,limit,unknown,unassigned){
      var out='<svg viewBox="0 0 800 190" role="img" aria-label="当前上下文容量分布"><defs><pattern id="'+uid+'" width="9" height="9" patternUnits="userSpaceOnUse" patternTransform="rotate(45)"><rect width="9" height="9" fill="#ece5f3"/><line x1="0" y1="0" x2="0" y2="9" stroke="#ad95c3" stroke-width="3"/></pattern></defs>';
      for(var r=0;r<4;r++)out+='<rect x="1" y="'+(r*44+5)+'" width="798" height="36" rx="4" fill="#eff3f2"/>';
      var offset=0,rowLimit=limit/4;
      function chunk(amount,fill){
        var remaining=Math.max(0,Math.min(amount,limit-offset));
        while(remaining>.01){var row=Math.min(3,Math.floor((offset+.0001)/rowLimit)),inRow=offset-row*rowLimit,take=Math.min(remaining,rowLimit-inRow);if(take<.001)break;out+='<rect x="'+(1+inRow/rowLimit*798).toFixed(2)+'" y="'+(row*44+5)+'" width="'+(take/rowLimit*798).toFixed(2)+'" height="36" fill="'+fill+'" stroke="white" stroke-width="1.5"/>';offset+=take;remaining-=take;}
      }
      sequence.forEach(function(k){chunk(values[k]||0,palette[k]);});
      if(unknown||unassigned>0)chunk(unknown?limit-offset:unassigned,'url(#'+uid+')');
      for(var r=0;r<4;r++)for(var c=1;c<10;c++)out+='<line x1="'+(1+c*79.8)+'" y1="'+(r*44+5)+'" x2="'+(1+c*79.8)+'" y2="'+(r*44+41)+'" stroke="white" stroke-width="1" opacity=".55"/>';
      out+='</svg>';svg.innerHTML=out;
    }
    function render(step,skip){
      if(frame)cancelAnimationFrame(frame);var limit=typeof step.window==='number'&&step.window>0?step.window:200000;
      var values={};sequence.forEach(function(k){values[k]=0;});var missing=false;
      (step.request||[]).forEach(function(b){if(typeof b.tokens!=='number'){missing=true;return;}var k=b.kind==='system'||b.kind==='schema'||b.kind==='rule'?'rules':palette[b.kind]?b.kind:'other';values[k]+=b.tokens;});
      var known=sequence.reduce(function(n,k){return n+values[k];},0),used=typeof step.usage==='number'?step.usage:null;
      var unknown=used===null,unassigned=used===null?0:Math.max(0,used-known);
      var note=unknown?'斜纹部分大小未确定，不是空闲；当前只能确认已知部分至少 '+fmt(known)+' token。':used===null?'本步没有完整用量，容量图仅展示有数字的部分；不能据此认定剩余容量。':'四行合计 '+fmt(limit)+' token。已用 '+fmt(used)+'，'+(used>limit?'超出窗口 '+fmt(used-limit)+'，超出部分尚无可用空间。':'未用 '+fmt(limit-used)+'。未用空间尚未扣除该分支可能要求的输出预留。');
      if(unassigned>0)note+=' 斜纹为总量中未拆分的部分，不代表已测出原生压缩项自身大小。';
      target.querySelector('.ca-capacity-note').textContent=note;
      target.querySelector('.ca-capacity-legend').innerHTML=sequence.filter(function(k){return values[k]>0;}).map(function(k){return '<span><i style="background:'+palette[k]+'"></i>'+names[k]+' '+fmt(values[k])+'</span>';}).join('')+(unknown||unassigned>0?'<span><i class="ca-capacity-unknown"></i>未拆分 / 未知</span>':'');
      var reduce=matchMedia('(prefers-reduced-motion: reduce)').matches;
      if(!prior||skip||reduce||unknown||prior.unknown||prior.limit!==limit){paint(values,used,limit,unknown,unassigned);}
      else {var start=performance.now(),from=prior.values;var tick=function(now){var t=Math.min(1,(now-start)/1100),e=1-Math.pow(1-t,3),v={};sequence.forEach(function(k){v[k]=from[k]+(values[k]-from[k])*e;});paint(v,used,limit,unknown,unassigned);if(t<1)frame=requestAnimationFrame(tick);};frame=requestAnimationFrame(tick);}
      prior={values:values,unknown:unknown,limit:limit};
    }
    return {render:render,destroy:function(){if(frame)cancelAnimationFrame(frame);target.remove();}};
  }
  window.ContextCapacity={mount:mount};
})();
