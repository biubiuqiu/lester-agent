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
  await page.locator(".interaction-receipt>summary").click();
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
  await page.locator(".interaction-receipt>summary").click();
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
