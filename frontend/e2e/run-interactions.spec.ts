import { openConversationRecords } from "./conversation-controls";
import { test as base, expect, type Page } from "@playwright/test";
import { WorkspaceApi, conversationId, prompt } from "./workspace-api";

const prefix = `/api/v1/conversations/${conversationId}/interactions`;
const itemId = "44444444-4444-4444-8444-444444444444";
const digest = "a".repeat(64);
const test = base.extend<{ api: WorkspaceApi }>({
  api: async ({ context, page }, runFixture) => {
    const api = new WorkspaceApi(), errors: string[] = [];
    page.on("pageerror", error => errors.push(error.message));
    await api.install(context);
    try { await runFixture(api); expect(api.unexpected).toEqual([]); expect(errors).toEqual([]); }
    finally { await api.close(); }
  },
});
async function start(page: Page, api: WorkspaceApi) {
  await page.goto("/app");
  await page.getByLabel("邮箱", { exact: true }).fill("browser@example.test");
  await page.getByLabel("密码", { exact: true }).fill("fixture-password-123");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await input.fill(prompt); await input.press("Enter");
  await expect(page).toHaveURL(`/app/c/${conversationId}`);
  await expect.poll(() => api.streamCount).toBeGreaterThan(0);
  return input;
}
const planItem = { id: itemId, run_id: "fixture-run", kind: "plan", request_digest: digest, status: "pending", request: { kind: "plan", title: "先审阅再修改", steps: ["读取当前文件", "完成明确的修改", "运行验证"] } };
const questionItem = { id: itemId, run_id: "fixture-run", kind: "question", request_digest: digest, status: "pending", request: { kind: "question", questions: [{ id: "format", prompt: "希望交付什么形式？", options: [{ id: "html", label: "网页", description: "可直接预览" }, { id: "document", label: "文档" }] }] } };

test("Agent-selected Plan requires explicit review, exact approval survives stale polling and keeps the composer draft", async ({ page, context, api }, info) => {
  let lists = 0, approvals = 0;
  await context.route(`**${prefix}`, route => { lists++; return route.fulfill({ json: { interactions: [planItem], execution_mode: "plan" } }); });
  await context.route(`**${prefix}/${itemId}/respond`, route => {
    approvals++; expect(route.request().postDataJSON()).toEqual({ request_digest: digest, decision: "approve" });
    return route.fulfill({ json: { ...planItem, status: "approved", response: { decision: "approve", request_digest: digest } } });
  });
  const input = await start(page, api);
  expect(api.mutations.find(item => item.path.endsWith("/messages"))?.body.execution_mode ?? "execute").toBe("execute");
  await expect(page.getByRole("button", { name: "批准并执行", exact: true })).toBeVisible();
  await input.fill("批准过程中保留的追问");
  await expect(page.locator(".composer-motion-ghost")).toHaveCount(0);
  await page.screenshot({ path: info.outputPath("pending-plan-review.png") });
  await page.getByRole("button", { name: "批准并执行", exact: true }).click();
  await expect(page.locator(".interaction-receipt>summary")).toHaveText("计划已批准 · 先审阅再修改");
  await expect(page.getByText("Plan · 仅查看与规划", { exact: true })).toHaveCount(0);
  const afterApprove = lists;
  await expect.poll(() => lists).toBeGreaterThan(afterApprove);
  await expect(page.locator(".interaction-receipt>summary")).toHaveText("计划已批准 · 先审阅再修改");
  await expect(input).toHaveValue("批准过程中保留的追问");
  expect(approvals).toBe(1);
  await page.screenshot({ path: info.outputPath("approved-plan-receipt.png") });
});

test("a lost question response confirms by scoped receipt and never resubmits", async ({ page, context, api }, info) => {
  let responses = 0, inspected = 0;
  const answer = { request_digest: digest, answers: { format: { option_id: "html", text: "保留极简风格" } } };
  await context.route(`**${prefix}`, route => route.fulfill({ json: { interactions: [questionItem], execution_mode: "execute" } }));
  await context.route(`**${prefix}/${itemId}/respond`, route => {
    responses++; expect(route.request().postDataJSON()).toEqual(answer);
    return route.fulfill({ status: 503, json: { error: "fixture receipt lost" } });
  });
  await context.route(`**${prefix}/${itemId}`, route => { inspected++; return route.fulfill({ json: { ...questionItem, status: "answered", response: { answers: { format: { text: "保留极简风格", option_id: "html" } }, request_digest: digest } } }); });
  const input = await start(page, api);
  await input.fill("回答时保留的草稿");
  await page.getByRole("radio", { name: "网页 可直接预览" }).check();
  await page.getByRole("textbox", { name: "补充回答", exact: true }).fill("保留极简风格");
  await page.screenshot({ path: info.outputPath("pending-question-review.png") });
  await page.getByRole("button", { name: "提交回答", exact: true }).click();
  await expect(page.locator(".interaction-receipt>summary")).toHaveText("已回答");
  await openConversationRecords(page); await page.locator(".interaction-receipt>summary").click();
  await expect(page.locator(".interaction-content")).toContainText("保留极简风格");
  await expect(page.getByRole("button", { name: "批准并执行", exact: true })).toHaveCount(0);
  await expect(input).toHaveValue("回答时保留的草稿");
  expect(responses).toBe(1); expect(inspected).toBe(1);
});

test("plan revisions require explicit feedback and keep execution read-only", async ({ page, context, api }) => {
  let responded = 0;
  await context.route(`**${prefix}`, route => route.fulfill({ json: { interactions: [planItem], execution_mode: "plan" } }));
  await context.route(`**${prefix}/${itemId}/respond`, route => {
    responded++;
    expect(route.request().postDataJSON()).toEqual({ request_digest: digest, decision: "request_changes", feedback: "先保留旧文件，只生成新文件" });
    return route.fulfill({ json: { ...planItem, status: "changes_requested", response: route.request().postDataJSON() } });
  });
  const input = await start(page, api);
  await expect(page.getByRole("button", { name: "调整计划", exact: true })).toBeDisabled();
  await input.fill("保留聊天草稿");
  await page.getByRole("textbox", { name: "调整意见", exact: true }).fill("先保留旧文件，只生成新文件");
  await page.getByRole("button", { name: "调整计划", exact: true }).click();
  await expect(page.locator(".interaction-receipt>summary")).toHaveText("请调整计划 · 先审阅再修改");
  await expect(page.getByText("Plan · 仅查看与规划", { exact: true })).toBeVisible();
  await expect(input).toHaveValue("保留聊天草稿");
  expect(responded).toBe(1);
});

test("pending answers survive refresh, then cancellation removes all decision controls", async ({ page, context, api }) => {
  let cancelled = false;
  await context.route(`**${prefix}`, route => route.fulfill({ json: { interactions: [{ ...questionItem, status: cancelled ? "cancelled" : "pending" }], execution_mode: "execute" } }));
  await start(page, api);
  await page.getByRole("textbox", { name: "补充回答", exact: true }).fill("刷新后仍保留的回答");
  await page.reload();
  await expect(page.getByRole("textbox", { name: "补充回答", exact: true })).toHaveValue("刷新后仍保留的回答");
  await expect.poll(() => api.streamCount).toBeGreaterThan(0);
  cancelled = true;
  api.failRun("fixture owner interrupted");
  await expect(page.getByRole("button", { name: "提交回答", exact: true })).toHaveCount(0);
  await expect(page.locator(".interaction-receipt>summary")).toHaveText("已取消");
  await openConversationRecords(page); await page.locator(".interaction-receipt>summary").click();
  await expect(page.locator(".interaction-content")).toContainText("此请求已撤销，旧回答或批准不会启动任务。");
});

test("Agent re-entering Plan after an approved scope stays visibly read-only", async ({page,context,api})=>{
 let mode="execute";
 await context.route(`**${prefix}`,route=>route.fulfill({json:{interactions:[{...planItem,status:"approved"}],execution_mode:mode}}));
 const input=await start(page,api);await input.fill("保留后续要求");
 await expect(page.locator(".interaction-mode")).toHaveCount(0);
 mode="plan";api.planModeEvent();await expect(page.locator(".interaction-mode")).toContainText("Plan");
 // A second response with historical approval must not mask the new read-only mode.
 await page.evaluate(()=>document.dispatchEvent(new Event("visibilitychange")));
 await expect(page.locator(".interaction-mode")).toContainText("Plan");await expect(input).toHaveValue("保留后续要求");
 expect(api.mutations.filter(item=>item.path.includes("/respond"))).toHaveLength(0);
});

test("questions dock above the composer, keep multi-select notes across reload and submit only after review",async({page,context,api},info)=>{
 const request={...questionItem,request:{kind:"question",questions:[
  {id:"format",header:"交付形式",prompt:"这次希望交付什么？",options:[{id:"html",label:"网页",description:"直接预览",recommended:true},{id:"document",label:"文档",description:"便于阅读和分享"}]},
  {id:"features",header:"界面细节",prompt:"哪些交互细节需要一起完善？",multi_select:true,options:[{id:"queue",label:"消息队列",description:"工作中安排下一条任务"},{id:"recap",label:"会话回顾",description:"回来看清进展"},{id:"files",label:"文件预览",description:"在旁边查看成果"}]},
  {id:"note",header:"其他要求",prompt:"还有哪些要求？"},
 ]}};
 let answered=false,responses=0;let body:Record<string,unknown>|null=null;
 await context.route(`**${prefix}`,route=>route.fulfill({json:{interactions:[answered?{...request,status:"answered",response:body}:request],execution_mode:"plan"}}));
 await context.route(`**${prefix}/${itemId}/respond`,route=>{responses++;body=route.request().postDataJSON();answered=true;return route.fulfill({json:{...request,status:"answered",response:body}});});
 const input=await start(page,api);await input.fill("回答问题期间保留的聊天草稿");
 const dock=page.locator(".question-dock");await expect(dock).toContainText("这次希望交付什么？");await expect(dock.locator("input:checked")).toHaveCount(0);await expect(dock.getByRole("button",{name:"下一题",exact:true})).toBeDisabled();
 await expect(input).toBeFocused();await input.pressSequentially("123");await expect(dock.getByRole("radio",{name:/网页.*推荐.*直接预览/})).not.toBeChecked();await input.fill("回答问题期间保留的聊天草稿");
 await dock.getByRole("radio",{name:/网页.*推荐.*直接预览/}).focus();await page.keyboard.press("1");await expect(dock.getByRole("radio",{name:/网页.*推荐.*直接预览/})).toBeChecked();expect(responses).toBe(0);
 await dock.getByRole("button",{name:"下一题",exact:true}).click();await dock.getByRole("checkbox",{name:"消息队列 工作中安排下一条任务"}).check();await dock.getByRole("checkbox",{name:"文件预览 在旁边查看成果"}).check();await dock.getByRole("textbox",{name:"补充回答",exact:true}).fill("保留额外要求\n不要丢掉我的文字");
 await page.screenshot({path:info.outputPath("question-multi-select.png")});
 await page.reload();await expect(dock).toContainText("还有哪些要求？");await dock.getByRole("tab",{name:"界面细节",exact:true}).click();await expect(dock.getByRole("checkbox",{name:"消息队列 工作中安排下一条任务"})).toBeChecked();await expect(dock.getByRole("checkbox",{name:"文件预览 在旁边查看成果"})).toBeChecked();await expect(dock.getByRole("textbox",{name:"补充回答",exact:true})).toHaveValue("保留额外要求\n不要丢掉我的文字");
 await dock.getByRole("tab",{name:"其他要求",exact:true}).click();await dock.getByRole("textbox",{name:"补充回答",exact:true}).fill("先解释清楚，等待我的下一步指令。");await dock.getByRole("button",{name:"核对回答",exact:true}).click();await expect(dock).toContainText("消息队列 · 文件预览");await expect(dock).toContainText("保留额外要求");expect(responses).toBe(0);
 await page.screenshot({path:info.outputPath("question-review.png")});
 await expect.poll(async()=>{const box=await dock.boundingBox(),composer=await page.locator(".composer").boundingBox();return Boolean(box&&composer&&box.y+box.height<=composer.y+1);}).toBe(true);
 await expect(page.getByRole("button",{name:"停止生成",exact:true})).toBeInViewport();await expect(input).toBeInViewport();
 if(info.project.name==="mobile"){
   await page.setViewportSize({width:393,height:520});
   const withinViewport=async()=>{for(const locator of [input,page.getByRole("button",{name:"停止生成",exact:true}),dock.getByRole("button",{name:"提交回答",exact:true})]){const visible=await locator.evaluate(element=>{const bounds=element.getBoundingClientRect();return bounds.top>=-1&&bounds.bottom<=innerHeight+1;});if(!visible)return false;}return true;};
   await expect.poll(withinViewport).toBe(true);
   const longDraft="保留我的长草稿，回答问题时仍然可以查看和停止。".repeat(50);await input.fill(longDraft);await expect.poll(()=>input.evaluate(element=>element.scrollHeight>element.clientHeight)).toBe(true);await expect.poll(withinViewport).toBe(true);await expect(input).toHaveValue(longDraft);
   await input.fill("回答问题期间保留的聊天草稿");await page.screenshot({path:info.outputPath("question-short-viewport.png")});
  }
  await dock.getByRole("button",{name:"提交回答",exact:true}).click();await expect(page.locator(".interaction-receipt>summary")).toHaveText("已回答");expect(responses).toBe(1);
 expect(body).toEqual({request_digest:digest,answers:{format:{option_id:"html",text:""},features:{option_id:"",option_ids:["queue","files"],text:"保留额外要求\n不要丢掉我的文字"},note:{text:"先解释清楚，等待我的下一步指令。"}}});
 await expect(input).toHaveValue("回答问题期间保留的聊天草稿");await expect(page.getByText("Plan · 仅查看与规划",{exact:true})).toBeVisible();await expect(page.getByRole("button",{name:"批准并执行",exact:true})).toHaveCount(0);await expect(dock.locator(".question-card")).toHaveCount(0);
});

const schedulePreview = { id: "77777777-7777-4777-8777-777777777777", revision: 0, name: "产品评审清单", prompt: "生成包含目标、范围与验收标准的需求评审清单。", project_id: "product-project", project_name: "产品", model_deployment_id: "fixture-model", model_name: "测试模型", agent_slug: "lester", permission_mode: "standard", enabled: true, cron: "0 9 * * 1", timezone: "Asia/Shanghai", run_at: null, next_run_at: "2026-11-02T01:00:00Z" };
const scheduleItem = { id: itemId, run_id: "fixture-run", kind: "automation", request_digest: digest, status: "pending", request: { kind: "automation", schedule: { action: "create" }, schedule_preview: schedulePreview } };

test("schedule review uses the reviewed zone, preserves drafts and resolves a lost confirmation without replay", async ({ page, context, api }, info) => {
  let posts = 0, reads = 0, sentZone = "";
  page.on("request", request => { if (request.method() === "POST" && /\/messages$/.test(new URL(request.url()).pathname)) sentZone = request.headers()["x-lester-timezone"] || ""; });
  const response = { request_digest: digest, decision: "approve" };
  await context.route(`**${prefix}`, route => route.fulfill({ json: { interactions: [scheduleItem], execution_mode: "execute" } }));
  await context.route(`**${prefix}/${itemId}/respond`, route => { posts++; expect(route.request().postDataJSON()).toEqual(response); return route.fulfill({ status: 503, json: { error: "lost receipt" } }); });
  await context.route(`**${prefix}/${itemId}`, route => { reads++; return route.fulfill({ json: { ...scheduleItem, status: "approved", response } }); });
  const input = await start(page, api);
  const card = page.getByRole("article", { name: "定时任务等待确认", exact: true });
  await expect(card).toBeVisible();
  await expect(card.getByText("每周一 09:00", { exact: false })).toBeVisible();
  await expect(card.getByText("Asia/Shanghai", { exact: true })).toBeVisible();
  await expect(card.getByText("项目 · 产品", { exact: true })).toBeVisible();
  await input.fill("安排后保留的追问");
  await expect(input).toBeFocused();
  await expect(page.getByRole("button", { name: "停止生成", exact: true })).toBeInViewport();
  await expect(input).toBeInViewport();
  await expect(page.locator("form form")).toHaveCount(0);
  await page.screenshot({ path: info.outputPath("schedule-review.png") });
  const bounds = await card.boundingBox(); expect(bounds).not.toBeNull(); expect(bounds!.x).toBeGreaterThanOrEqual(0); expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(page.viewportSize()!.width);
  await card.getByText("任务内容与配置", { exact: true }).click();
  await expect(card.getByText(schedulePreview.prompt, { exact: true })).toBeVisible();
  await expect(card.getByText("默认权限", { exact: true })).toBeVisible();
  await card.getByRole("button", { name: "启用定时任务", exact: true }).click();
  await expect(page.locator(".schedule-receipts summary").first()).toHaveText("定时任务配置已应用 · 产品评审清单");
  await expect(card).toHaveCount(0);
  await page.waitForTimeout(5200); // exercise a stale pending read after confirmation
  await expect(page.getByRole("button", { name: "启用定时任务", exact: true })).toHaveCount(0);
  await expect(input).toHaveValue("安排后保留的追问");
  await page.locator(".schedule-receipts > details > summary").click();
  await expect(page.getByRole("link", { name: "管理定时任务", exact: true })).toHaveAttribute("href", "/app/settings/automations");
  expect(posts).toBe(1); expect(reads).toBe(1);
  expect(sentZone).toBe(await page.evaluate(() => Intl.DateTimeFormat().resolvedOptions().timeZone));
  await page.screenshot({ path: info.outputPath("schedule-applied.png") });
});

for (const [action,label] of [["update","确认修改"],["pause","暂停定时任务"],["resume","恢复定时任务"]] as const) {
 test(`schedule ${action} keeps one compact, explicit confirmation`, async ({page,context,api},info)=>{
  let posts=0;const item={...scheduleItem,request:{...scheduleItem.request,schedule:{action},schedule_preview:{...schedulePreview,revision:3,enabled:action!=="pause"}}};
  await context.route(`**${prefix}`,route=>route.fulfill({json:{interactions:[item],execution_mode:"execute"}}));
  await context.route(`**${prefix}/${itemId}/respond`,route=>{posts++;return route.fulfill({json:{...item,status:"approved",response:route.request().postDataJSON()}});});
  const input=await start(page,api);await input.fill("不要丢这段草稿");
  const card=page.getByRole("article",{name:"定时任务等待确认",exact:true});await expect(card.getByRole("button",{name:label,exact:true})).toBeVisible();
  if(action==="pause")await expect(card.getByText("暂停会取消尚未开始的触发，已启动的任务继续执行。",{exact:true})).toBeVisible();
  await page.screenshot({path:info.outputPath(`schedule-${action}.png`)});await card.getByRole("button",{name:label,exact:true}).click();
  await expect(page.locator(".schedule-receipts > details > summary")).toHaveText("定时任务配置已应用 · 产品评审清单");await expect(input).toHaveValue("不要丢这段草稿");expect(posts).toBe(1);
 });
}

test("expired schedule cannot imply success and can be declined without applying",async({page,context,api})=>{
 const decisions:string[]=[];
 await context.route(`**${prefix}`,route=>route.fulfill({json:{interactions:[scheduleItem],execution_mode:"execute"}}));
 await context.route(`**${prefix}/${itemId}/respond`,route=>{const response=route.request().postDataJSON();decisions.push(response.decision);return response.decision==="approve"?route.fulfill({status:409,json:{error:"stale"}}):route.fulfill({json:{...scheduleItem,status:"rejected",response}});});
 const input=await start(page,api);await input.fill("先保留草稿");const card=page.getByRole("article",{name:"定时任务等待确认",exact:true});await card.getByRole("button",{name:"启用定时任务",exact:true}).click();
 await expect(card.getByRole("alert")).toContainText("配置已变化或时间已过期");await expect(page.getByText("定时任务配置已应用 · 产品评审清单",{exact:true})).toHaveCount(0);
 await card.getByRole("button",{name:"暂不应用",exact:true}).click();await expect(page.locator(".schedule-receipts > details > summary")).toHaveText("未应用定时任务 · 产品评审清单");await expect(input).toHaveValue("先保留草稿");expect(decisions).toEqual(["approve","reject"]);
});

for(const [locale,label] of [["en","Enable schedule"],["ja","定期タスクを有効化"],["ko","예약 작업 활성화"],["fr","Activer la tâche"],["es","Activar tarea programada"]]){
 test(`schedule card is readable in ${locale}`,async({page,context,api},info)=>{
  await start(page,api);const mutations=api.mutations.length;
  await context.addCookies([{name:"lester_locale",value:locale,url:"http://127.0.0.1:13020"}]);
  await context.route(`**${prefix}`,route=>route.fulfill({json:{interactions:[scheduleItem],execution_mode:"execute"}}));
  await page.goto(`/app/c/${conversationId}`);const card=page.locator(".schedule-card");await expect(card.getByRole("button",{name:label,exact:true})).toBeVisible();await expect(card.getByRole("heading",{name:schedulePreview.name,exact:true})).toBeVisible();
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await page.screenshot({path:info.outputPath(`schedule-${locale}.png`)});expect(api.mutations).toHaveLength(mutations);
 });
}

test("schedule adjustment drafts survive reload and request changes without applying", async ({page,context,api})=>{
 let responses=0;
 await context.route(`**${prefix}`,route=>route.fulfill({json:{interactions:[scheduleItem],execution_mode:"execute"}}));
 await context.route(`**${prefix}/${itemId}/respond`,route=>{responses++;expect(route.request().postDataJSON()).toEqual({request_digest:digest,decision:"request_changes",feedback:"改成每周二上午十点"});return route.fulfill({json:{...scheduleItem,status:"changes_requested",response:route.request().postDataJSON()}});});
 const input=await start(page,api);await input.fill("原聊天草稿保留");let card=page.getByRole("article",{name:"定时任务等待确认",exact:true});await card.locator(".schedule-feedback > summary").click();await card.getByRole("textbox",{name:"调整意见",exact:true}).fill("改成每周二上午十点");
 await page.reload();card=page.getByRole("article",{name:"定时任务等待确认",exact:true});await card.locator(".schedule-feedback > summary").click();await expect(card.getByRole("textbox",{name:"调整意见",exact:true})).toHaveValue("改成每周二上午十点");await expect(page.getByRole("textbox",{name:"消息输入框",exact:true})).toHaveValue("原聊天草稿保留");
 await card.getByRole("button",{name:"提交调整",exact:true}).click();await expect(page.locator(".schedule-receipts > details > summary")).toHaveText("请调整定时任务 · 产品评审清单");expect(responses).toBe(1);
});
