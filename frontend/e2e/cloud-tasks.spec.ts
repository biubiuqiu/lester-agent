import { readFileSync } from "node:fs";
import { join } from "node:path";
import { translator } from "../src/lib/i18n";
import { test as base, expect, type Page } from "@playwright/test";
import { WorkspaceApi, conversationId, prompt } from "./workspace-api";
const test=base.extend<{api:WorkspaceApi}>({api:async({context,page},runFixture)=>{const api=new WorkspaceApi(),errors:string[]=[];page.on("pageerror",e=>errors.push(e.message));await api.install(context);try{await runFixture(api);expect(api.unexpected).toEqual([]);expect(errors).toEqual([]);}finally{await context.close();await api.close();}}});
async function start(page:Page,api:WorkspaceApi){
 await page.goto("/app/new");await page.getByLabel("邮箱",{exact:true}).fill("browser@example.test");await page.getByLabel("密码",{exact:true}).fill("fixture-password-123");await page.getByRole("button",{name:"登录",exact:true}).click();const input=page.getByRole("textbox",{name:"消息输入框",exact:true});await input.fill(prompt);await input.press("Enter");await expect(page).toHaveURL(`/app/c/${conversationId}`);await expect.poll(()=>api.streamCount).toBeGreaterThan(0);api.finish();await expect(page.getByRole("button",{name:"发送消息",exact:true})).toBeVisible();
 // Completion and verified file inventory arrive independently. Settle and
 // dismiss the generated preview before typing or opening another panel.
 await expect(page.locator(".open-file-tabs [role=tab]")).toHaveCount(2);
 await page.locator(".computer-panel .close-files-panel").click();
 return input;
}
const now="2026-10-15T10:00:00Z";
const task=(run_id:string,state:string,title:string)=>({run_id,conversation_id:run_id==="fixture-run"?conversationId:run_id==="another-run"?"33333333-3333-4333-8333-333333333333":"44444444-4444-4444-8444-444444444444",title,project:"个人项目",agent:"Lester",state,started_at:now,completed_at:state==="completed"?now:null,last_activity_at:now,background_commands:state==="completed"?1:0,unknown_commands:0,queued:state==="waiting_plan"?2:0,deliverables:state==="completed"?1:0});
const tasks=[task("fixture-run","completed","已经交付的网页"),task("another-run","waiting_plan","等你审阅的复杂改造"),task("third-run","failed","需要检查的任务")];

test("task status filters, cursor pages and scoped usage are read-only and preserve the composer",async({context,page,api},info)=>{
 let usageReads=0, measured=true;const consoleErrors:string[]=[];page.on("console",m=>{if(m.type()==="error"&&!m.text().includes("401"))consoleErrors.push(m.text());});
 await context.route("**/api/v1/tasks?*",route=>{expect(route.request().method()).toBe("GET");const url=new URL(route.request().url()),filter=url.searchParams.get("filter"),cursor=url.searchParams.get("cursor");const list=filter==="attention"?tasks.slice(1):filter==="active"?tasks.filter(item=>item.state==="waiting_plan"||item.background_commands>0):filter==="finished"?tasks.filter(item=>item.state==="failed"||item.state==="completed"&&item.background_commands===0):cursor?[tasks[2]]:tasks.slice(0,2);return route.fulfill({json:{tasks:list,next_cursor:filter==="all"&&!cursor?"next-page":"",attention_count:2,observed_at:now}});});
 await context.route(`**/api/v1/conversations/${conversationId}/runs/fixture-run/usage`,route=>{expect(route.request().method()).toBe("GET");usageReads++;return route.fulfill({json:measured?{elapsed_seconds:95,requests:3,in_flight:0,actual_input_tokens:200,actual_input_requests:2,actual_output_tokens:20,actual_output_requests:2,estimated_input_tokens:123,estimated_input_requests:1,missing_output_requests:1,observed_at:now}:{elapsed_seconds:null,requests:0,in_flight:0,actual_input_tokens:0,actual_input_requests:0,actual_output_tokens:0,actual_output_requests:0,estimated_input_tokens:0,estimated_input_requests:0,missing_output_requests:0,observed_at:now}});});
 const input=await start(page,api);await input.fill("打开任务面板时保留我的草稿");const before=api.mutations.length;
 if(info.project.name==="mobile")await page.getByRole("button",{name:"打开会话栏",exact:true}).click();
 const trigger=page.getByRole("button",{name:"最近会话",exact:true});await expect(trigger).toContainText("2");await trigger.click();await page.getByRole("button",{name:"查看任务状态",exact:true}).click();const dialog=page.getByRole("dialog",{name:"任务状态",exact:true});await expect(dialog).toContainText("每个会话的最近任务");await expect(dialog.locator(".cloud-task-card")).toHaveCount(2);await expect(dialog).toContainText("1 个会话后台命令运行中");expect(usageReads).toBe(0);
 await dialog.getByRole("button",{name:"加载更多",exact:true}).click();await expect(dialog.locator(".cloud-task-card")).toHaveCount(3);
 await dialog.getByRole("button",{name:"需处理",exact:true}).click();await expect(dialog.locator(".cloud-task-card")).toHaveCount(2);await expect(dialog).toContainText("等待计划审阅");await expect(dialog).toContainText("2 条排队或暂缓任务");
 await dialog.getByRole("button",{name:"全部任务",exact:true}).click();await expect(dialog.locator(".cloud-task-card")).toHaveCount(2);const card=dialog.locator(".cloud-task-card").first();await card.getByText("耗时与模型用量",{exact:true}).click();await expect(card).toContainText("200");await expect(card).toContainText("123");await expect(card).toContainText("未计为零");await expect(card).toContainText("1 分 35 秒");expect(usageReads).toBe(1);
 await page.screenshot({path:info.outputPath("cloud-task-usage.png")});
 if(info.project.name==="mobile"){await page.setViewportSize({width:320,height:740});await expect.poll(()=>dialog.evaluate(e=>e.getBoundingClientRect().right<=innerWidth)).toBe(true);await page.screenshot({path:info.outputPath("cloud-tasks-narrow.png")});}
 measured=false;await dialog.getByRole("button",{name:"刷新任务",exact:true}).click();await expect(card).toContainText("未记录");await expect(card).toContainText("未报告");await expect(card).not.toContainText("NaN");
 await dialog.getByRole("button",{name:"返回最近会话",exact:true}).click();await expect(page.getByRole("dialog",{name:"最近会话",exact:true})).toBeVisible();
 await page.keyboard.press("Escape");await expect(dialog).toHaveCount(0);await expect(trigger).toBeFocused();await expect(input).toHaveValue("打开任务面板时保留我的草稿");expect(api.mutations).toHaveLength(before);
 await trigger.click();await page.getByRole("button",{name:"查看任务状态",exact:true}).click();await page.getByRole("button",{name:"已经交付的网页",exact:false}).click();await expect(dialog).toHaveCount(0);await expect(page).toHaveURL(`/app/c/${conversationId}`);await expect(input).toHaveValue("打开任务面板时保留我的草稿");expect(api.mutations).toHaveLength(before);expect(consoleErrors).toEqual([]);
});

test("failed task reads stay explicit, retry recovers without sending or losing the draft",async({context,page,api},info)=>{
 let fail=true;await context.route("**/api/v1/tasks?*",route=>route.fulfill(fail?{status:503,json:{error:"fixture unavailable"}}:{json:{tasks:[],next_cursor:"",attention_count:0,observed_at:now}}));
 const input=await start(page,api);await input.fill("未发送的恢复说明");const before=api.mutations.length;
 if(info.project.name==="mobile")await page.getByRole("button",{name:"打开会话栏",exact:true}).click();
 await page.getByRole("button",{name:"最近会话",exact:true}).click();await page.getByRole("button",{name:"查看任务状态",exact:true}).click();const dialog=page.getByRole("dialog",{name:"任务状态",exact:true});await expect(dialog.getByRole("alert")).toContainText("任务列表暂不可用");await expect(dialog.getByText("当前筛选下没有任务",{exact:true})).toHaveCount(0);
 fail=false;await dialog.getByRole("button",{name:"重试",exact:true}).click();await expect(dialog).toContainText("当前筛选下没有任务");await page.getByRole("button",{name:"关闭会话面板",exact:true}).click();await expect(input).toHaveValue("未发送的恢复说明");expect(api.mutations).toHaveLength(before);
});

test("recent conversations and task status stay translated and fit the viewport across supported languages",async({context,page,api},info)=>{
 await context.route("**/api/v1/tasks?*",route=>route.fulfill({json:{tasks:[{...tasks[1],title:"保留用户原文的任务"}],next_cursor:"",attention_count:1,observed_at:now}}));
 await start(page,api);const before=api.mutations.length;
 if(info.project.name==="mobile")await page.setViewportSize({width:320,height:740});
 for(const locale of ["en","ja","ko","fr","es"]){
  const t=translator(JSON.parse(readFileSync(join(process.cwd(),`src/lib/i18n/messages/${locale}.json`),"utf8")));
  await context.addCookies([{name:"lester_locale",value:locale,url:"http://127.0.0.1:13020"}]);await page.goto(`/app/c/${conversationId}`);
  await expect(page.getByRole("textbox",{name:t("消息输入框"),exact:true})).toBeVisible();
  // Reload restores the verified file tabs and their selected preview.
  await page.locator(".computer-panel .close-files-panel").click();
  await expect(page.locator(".computer-panel")).toBeHidden();
  if(info.project.name==="mobile")await page.getByRole("button",{name:t("打开会话栏"),exact:true}).click();
  await page.getByRole("button",{name:t("最近会话"),exact:true}).click();
  const recent=page.getByRole("dialog",{name:t("最近会话"),exact:true});
  await recent.getByRole("searchbox",{name:t("搜索会话或项目"),exact:true}).fill("个人项目");
  await expect(recent.locator(".recent-conversation-row")).toHaveCount(1);await expect(recent).toContainText("个人项目");
  await expect.poll(()=>recent.evaluate(e=>e.scrollWidth<=e.clientWidth+1)).toBe(true);
  await expect.poll(()=>recent.evaluate(e=>{
   const title=e.querySelector("h2")!,button=e.querySelector(".recent-task-status")!,range=document.createRange();range.selectNodeContents(title);
   const a=range.getBoundingClientRect(),b=button.getBoundingClientRect();
   return a.bottom<=b.top+1||a.top>=b.bottom-1||a.right<=b.left+1||a.left>=b.right-1;
  })).toBe(true);
  if(locale==="fr")await page.screenshot({path:info.outputPath("recent-conversations-french.png")});
  await page.getByRole("button",{name:t("查看任务状态"),exact:true}).click();const dialog=page.getByRole("dialog",{name:t("任务状态"),exact:true});
  await expect(dialog).toContainText(t("等待计划审阅"));await expect(dialog).toContainText("保留用户原文的任务");await expect(dialog.getByRole("button",{name:t("需处理"),exact:true})).toBeVisible();
  await expect.poll(()=>dialog.evaluate(e=>e.scrollWidth<=e.clientWidth+1)).toBe(true);
  if(locale==="fr")await page.screenshot({path:info.outputPath("cloud-tasks-french.png")});
  await dialog.getByRole("button",{name:t("关闭会话面板"),exact:true}).click();
 }
 expect(api.mutations).toHaveLength(before);
});
