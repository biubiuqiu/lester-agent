import { openConversationOptions, openConversationRecords } from "./conversation-controls";
import { test as base, expect, type Page } from "@playwright/test";
import { WorkspaceApi, conversationId, prompt } from "./workspace-api";
const test = base.extend<{ api: WorkspaceApi }>({ api: async ({ context }, run) => { const api = new WorkspaceApi(); await api.install(context); try { await run(api); expect(api.unexpected).toEqual([]); } finally { await context.close(); await api.close(); } } });
async function start(page: Page) {
 await page.goto("/app"); await page.getByLabel("邮箱",{exact:true}).fill("browser@example.test"); await page.getByLabel("密码",{exact:true}).fill("fixture-password-123"); await page.getByRole("button",{name:"登录",exact:true}).click();
 const input=page.getByRole("textbox",{name:"消息输入框",exact:true});await input.fill(prompt);await input.press("Enter");await expect(page).toHaveURL(`/app/c/${conversationId}`);await expect(page.getByRole("button",{name:"更多会话选项",exact:true})).toBeVisible();return input;
}
test("recap commands and context inspection preserve drafts and never submit or approve",async({page,api},info)=>{
 const input=await start(page);await expect.poll(()=>api.streamCount).toBeGreaterThan(0);
 api.messages[0].metadata={project_rules:{project_id:"fixture-project",project_name:"个人项目",source:"settings",version:2,sha256:"a".repeat(64),content:"已受理的规则快照"}};
 await page.reload();await input.fill("保留这份草稿");const before=api.mutations.length;
 await openConversationOptions(page);await page.getByRole("button",{name:"会话回顾",exact:true}).click();const recap=page.getByRole("region",{name:"会话回顾",exact:true});await expect(recap).toContainText(prompt);await expect(input).toHaveValue("保留这份草稿");
 await page.getByRole("button",{name:"关闭回顾",exact:true}).click();await expect(page.getByRole("button",{name:"更多会话选项",exact:true})).toBeFocused();
 await input.fill("/recap");await input.press("Enter");await expect(recap).toBeVisible();await expect(input).toHaveValue("");expect(api.mutations).toHaveLength(before);
 await openConversationOptions(page);await page.getByRole("button",{name:"上下文概览",exact:true}).click();const context=page.getByRole("region",{name:"上下文概览",exact:true});await expect(context).toContainText("最近消息的材料");await expect(context.locator(".message-context")).toContainText("v2");
 await page.screenshot({path:info.outputPath("recap-context.png")});await page.getByRole("button",{name:"关闭上下文概览",exact:true}).click();
 await openConversationOptions(page);await page.getByRole("button",{name:"会话回顾",exact:true}).click();await page.getByRole("checkbox",{name:"自动回顾",exact:true}).uncheck();await page.reload();await openConversationOptions(page);await page.getByRole("button",{name:"会话回顾",exact:true}).click();await expect(page.getByRole("checkbox",{name:"自动回顾",exact:true})).not.toBeChecked();expect(api.mutations).toHaveLength(before);
});
test("return recap waits for new evidence and never opens over an unsent draft",async({page,api},info)=>{
 const input=await start(page);await expect.poll(()=>api.streamCount).toBeGreaterThan(0);let now=Date.now();await page.clock.setFixedTime(now);await input.fill("离开前未发送的文字");
 const visibility=async(hidden:boolean)=>page.evaluate(value=>{Object.defineProperty(document,"hidden",{configurable:true,value});document.dispatchEvent(new Event("visibilitychange"));},hidden);
 await visibility(true);now+=120001;await page.clock.setFixedTime(now);api.contextEvent("CONTEXT_COMPACTED",{estimated_before:2000,estimated_after:1000});await expect(page.locator(".tool-activity")).toContainText("历史已整理");await visibility(false);
 await expect(page.getByRole("region",{name:"会话回顾",exact:true})).toHaveCount(0);await expect(input).toHaveValue("离开前未发送的文字");await expect(input).toBeFocused();
 await input.fill("");await visibility(true);now+=120001;await page.clock.setFixedTime(now);api.contextEvent("CONTEXT_COMPACTING");await expect(page.locator(".tool-activity")).toContainText("正在整理历史上下文");await visibility(false);
 await expect(page.getByRole("region",{name:"会话回顾",exact:true})).toBeVisible();await expect(input).toBeFocused();await page.screenshot({path:info.outputPath("automatic-recap.png")});
});
test("stopped task recovery only prepares a draft and never replays work",async({page,api})=>{
 const input=await start(page);await input.fill("保留我的补充");api.conversation!.run_status="cancelled";await page.reload();await expect(page.locator(".run-recovery")).toBeVisible();const before=api.mutations.length;
 await page.locator(".run-recovery").getByRole("button",{name:"从当前结果继续",exact:true}).click();await expect(input).toHaveValue(/保留我的补充[\s\S]*请检查当前会话目录/);expect(api.mutations).toHaveLength(before);
});
test("focused reading preserves errors and detailed mode shows every call",async({page,context,api},info)=>{
 await start(page);const make=(id:number,type:string,payload:Record<string,unknown>={})=>({id,type,payload,run_id:"fixture-run",conversation_id:conversationId,created_at:new Date().toISOString()});
 const events=[make(1,"RUN_STARTED"),...Array.from({length:3},(_,i)=>[make(2+i*2,"TOOL_STARTED",{tool:"read",arguments:{file_path:`file-${i}.txt`}}),make(3+i*2,"TOOL_COMPLETED",{tool:"read",result:{content:"fixture"}})]).flat(),make(8,"TOOL_STARTED",{tool:"bash",arguments:{command:"fixture-test"}}),make(9,"TOOL_FAILED",{tool:"bash",error:"fixture failure"})];
 await context.route(`**/api/v1/conversations/${conversationId}/events/history`,route=>route.fulfill({json:{events}}));await page.reload();await expect(page.locator(".conversation-stream .tool-activity.completed")).toHaveCount(0);await openConversationRecords(page);await expect(page.locator(".conversation-records .tool-activity.completed")).toHaveCount(3);await expect(page.locator(".tool-activity.failed")).toBeVisible();
 await openConversationOptions(page);await page.getByRole("button",{name:"显示执行过程",exact:true}).click();await expect(page.locator(".process-group")).toHaveCount(0);await expect(page.locator(".tool-activity")).toHaveCount(4);await page.screenshot({path:info.outputPath("detailed-activity.png")});
 await page.reload();await openConversationOptions(page);await expect(page.getByRole("button",{name:"显示执行过程",exact:true})).toHaveAttribute("aria-pressed","true");expect(api.mutations.filter(item=>item.path.includes("/inputs"))).toHaveLength(0);
});
