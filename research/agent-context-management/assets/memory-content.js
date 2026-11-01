(function () {
  'use strict';

  window.MemoryLesson = {
    scope: '教学路径：先摘要，再接纳新材料。这是五个框架可能采用的一类流程，不代表它们共享同一套保留规则、触发线或压缩协议。',
    task: '教学任务：修复“登录成功后，刷新又回到登录页”。保留 HttpOnly、Secure 与既有鉴权边界。以下文件和记录来自虚构的小项目。',
    teachingNote: '下面展示的是材料摘录与摘要样例。40K、120K、10K 等容量是缩放后的教学设定，不是这几段文字的实测 token 数，也不表示固定压缩率。',

    before: [
      {
        title: '现象与约束',
        kind: '用户要求',
        text: '登录接口返回成功，但刷新后的 /api/profile 仍返回 401。修复登录保持问题；不能去掉 HttpOnly、Secure，也不能绕过服务端鉴权。'
      },
      {
        title: '网络记录指出路径不匹配',
        kind: '旧工具结果 · #T07',
        text: 'POST /api/auth/login → 200。响应写入 session cookie，Path=/api/auth；随后 GET /api/profile → 401，请求没有携带该 cookie。两次请求都在同一个 HTTPS 站点。'
      },
      {
        title: '代码定位与已失败的尝试',
        kind: '旧文件与对话',
        text: '教学项目 server/auth/cookies.ts 的 SESSION_COOKIE_PATH 被设为 /api/auth。清空站点数据再登录、延长 token 有效期都没有修复；签名校验测试通过。下一步检查 cookie 的路径作用域，而不是再次修改 token 时长。'
      },
      {
        title: '完整复现记录还包含这些细节',
        kind: '资料柜 H · network-login.txt',
        text: '第 2 次复现，请求 ID 为 demo-login-028；测试环境为 Chromium / Linux，地址是 https://app.example.test。原始响应包含 Path=/api/auth; HttpOnly; Secure; SameSite=Lax。请求 ID 可用于重新对应服务端日志。'
      }
    ],

    summary: {
      title: '安装到工作区的关键进展摘要',
      text: '目标：修复登录后刷新失效，保留安全 cookie 属性与鉴权边界。\n已确认：cookie 的 Path=/api/auth 不覆盖 /api/profile，因此刷新请求没有携带 session。\n已排除：清空站点数据和延长 token 有效期无效；签名校验通过。\n下一步：在 server/auth/cookies.ts 把 Path 调整为 /，新增跨路径与刷新回归测试，确认安全属性仍在。\n原始网络记录：资料柜 H / 工具结果 #T07 / network-login.txt。',
      items: [
        { label: '目标', text: '修复刷新失效，保留安全属性和鉴权边界。' },
        { label: '根因', text: 'Path=/api/auth 不覆盖 /api/profile，session 未随刷新请求发送。' },
        { label: '已排除', text: '清空数据、延长有效期无效；签名校验通过。' },
        { label: '下一步与位置', text: '修改 server/auth/cookies.ts 的 Path，并验证跨路径、刷新和安全属性。' }
      ]
    },

    omitted: [
      {
        title: '摘要没有保留请求 ID',
        text: 'demo-login-028、浏览器与第几次采样等细节没有进入短摘要。如果下一步需要把浏览器记录对应到服务端日志，仅凭这份摘要就不够了。'
      }
    ],

    recover: {
      label: '回读原文：找回请求 ID',
      source: '资料柜 H / 工具结果 #T07 / network-login.txt',
      detail: '原文摘录：\n复现：第 2 次\n请求 ID：demo-login-028\n环境：Chromium / Linux，https://app.example.test\n登录：POST /api/auth/login → 200\n响应 cookie 属性：Path=/api/auth; HttpOnly; Secure; SameSite=Lax\n刷新：GET /api/profile → 401；请求没有 session cookie。\n\n这段原文始终保存在 H。回读会再次把所需摘录加入工作集并占用输入预算；它不会自动、免费地回到模型眼前，也不需要重跑登录工具。'
    },

    request: {
      title: '摘要不是在盒子里凭空变出来的',
      input: [
        '摘要任务说明 2K：保留目标、约束、已确认结论、失败尝试、下一步和原文位置。',
        '选出的旧材料 120K：旧工具结果 70K + 较早对话 50K；复制到这次摘要请求。'
      ],
      budget: [
        { label: '输入：说明 2K + 旧材料 120K', value: '122K' },
        { label: '留给候选摘要的输出预算', value: '10K' },
        { label: '这次摘要请求仍未使用的容量', value: '68K' }
      ],
      capacity: 200000,
      inputTokens: 122000,
      outputReserve: 10000,
      free: 68000,
      formula: '122K 输入 + 10K 输出预留 + 68K 空位 = 200K；需要容纳的输入与输出预算共 132K。',
      steps: [
        {
          title: '1 · 选材并复制',
          text: '从当前历史选出 120K 旧材料。主工作区仍是 170K，新的 30K 继续在入口等待；选中和复制都没有释放空间。'
        },
        {
          title: '2 · 发出单独的摘要请求',
          text: '本例给这次请求单独算账：输入 122K，预留输出 10K，放得进 200K 窗口。它不是在主工作区剩余的 10K 空位中处理全部旧材料。'
        },
        {
          title: '3 · 得到候选摘要',
          text: '模型返回一份候选摘要。本例用 10K 表示其大小，并检查关键约束和原文位置是否保留；此时主工作区尚未替换。输出预留是上限安排，不保证每次输出正好 10K。'
        },
        {
          title: '4 · 接受并安装，才释放空间',
          text: '本教学流程接受候选后，用 10K 摘要替换 120K 旧材料，主工作区从 170K 降到 60K。请求失败、候选不可用或覆盖范围已变化，就不安装，不能宣称空间已经释放。'
        }
      ],
      note: '这一支线的请求结构、检查动作与全部数值也是教学设定；真实框架可能使用不同输入、重试与安装条件，或交由 provider 的原生压缩协议处理。摘要请求的 10K 输出预留和主任务的 20K 回复预留属于两次请求，不能相加成同一轮可用容量。'
    },

    mapping: [
      {
        symbol: 'P',
        title: '当前规则与能力',
        meaning: 'system / developer 指令、仓库规则、工具定义与 Skills，有各自来源和更新周期。',
        story: '蓝色规则来自 P；紫色用户要求是任务输入，不会因为画在同一张桌上就变成系统规则。'
      },
      {
        symbol: 'H',
        title: '持久历史与证据',
        meaning: '保存的消息、事件、工具结果及外置原文；保存下来不等于每轮全部发送。',
        story: '左侧资料柜。摘要替换后，#T07 网络原文仍在，可以按位置回读。'
      },
      {
        symbol: 'V',
        title: '活动工作集',
        meaning: '按历史、checkpoint、保留边界和投影规则重建的模型可见材料。',
        story: '桌上的历史色块和摘要展示 V。为了直观计量，这张桌也画了 P 和新输入；整盒是教学合并视图。'
      },
      {
        symbol: 'R',
        title: '本轮实际发送包',
        meaning: '把规则、工作集和新输入组装，再经过 provider 转换、schema、媒体处理与输出设置，才得到 wire request。',
        story: '模型实际接收的是 R。这张容量图没有逐项展开最终发送包，不能据它推算任一真实请求的精确 token 数。'
      },
      {
        symbol: 'C',
        title: 'Provider 缓存',
        meaning: '复用前缀计算，以降低重复处理成本；是否命中及费用由 provider 决定。',
        story: '主动画没有画缓存命中。缓存不为 200K 窗口额外扩容，缓存中的输入仍可能占上下文容量。'
      }
    ],

    highlights: {
      4: '准备保留：cookie 路径根因、失败尝试、安全约束和下一步。主工作区仍是 170K。',
      5: '摘要留下了 Path=/api/auth 的根因与修复位置；请求 ID 被省略，需要时回读 #T07。',
      6: '加入新测试材料后，可以继续修改 cookie 路径并验证刷新；摘要让旧结论仍可用。',
      7: '回复进入下一轮也占空间。若后来需要请求 ID，按摘要中的原文位置回读。'
    },

    glossary: [
      { term: 'wire request', text: '最终实际发送给模型服务的请求包。' },
      { term: 'checkpoint', text: '标记哪些历史已被摘要覆盖、从哪里继续重建的持久边界记录；具体结构依框架而异。' },
      { term: 'opaque', text: '客户端不能解释内部内容的压缩项，不能用它的外部字符长度代替内部 token 大小。' }
    ]
  };
}());
