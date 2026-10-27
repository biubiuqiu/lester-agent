import { test as base, expect } from "@playwright/test";
import { WorkspaceApi, conversationId } from "./workspace-api";
import { openConversationRecords } from "./conversation-controls";

const test = base.extend<{ api: WorkspaceApi }>({ api: async ({ context, page }, run) => {
 const api = new WorkspaceApi(), errors: string[] = [];
 page.on("pageerror", error => errors.push(error.message));
 await api.install(context);
 await context.addCookies([{ name: "lester_access_token", value: "browser-test-access", url: "http://127.0.0.1:13020" }]);
 try { await run(api); expect(api.unexpected).toEqual([]); expect(errors).toEqual([]); } finally { await api.close(); }
} });

test("app tool progress names search and read accurately without cluttering the final reply", async ({ page, context, api }, info) => {
 const at = new Date().toISOString();
 api.conversation = { id: conversationId, project_id: "fixture-project", workspace_id: "fixture-workspace", created_by: "22222222-2222-4222-8222-222222222222", agent_slug: "lester", agent_name: "Lester", model_deployment_id: "fixture-model", title: "邮件交付日期", run_id: "fixture-run", run_status: "completed", pinned: false, created_at: at, updated_at: at };
 api.messages = [{ id: "user-source", role: "user", run_id: "fixture-run", seq: 1, content: "在连接的 Gmail 中查找交付日期", created_at: at }, { id: "assistant-result", role: "assistant", run_id: "fixture-run", seq: 8, content: "交付日期是 10 月 30 日，来源 mail1。", created_at: at }];
 const event = (id: number, type: string, payload: Record<string, unknown> = {}) => ({ id, type, payload, run_id: "fixture-run", conversation_id: conversationId, created_at: at });
 const events = [event(1,"RUN_STARTED"), event(2,"TOOL_STARTED",{tool:"list_app_connections",tool_call_id:"list",arguments:"{}"}), event(3,"TOOL_COMPLETED",{tool:"list_app_connections",tool_call_id:"list",result:{connections:[],read_only:true}}), event(4,"TOOL_STARTED",{tool:"read_app",tool_call_id:"search",arguments:JSON.stringify({provider:"gmail",action:"search",query:"newer_than:7d"})}), event(5,"TOOL_COMPLETED",{tool:"read_app",tool_call_id:"search",result:{provider:"gmail",items:[{id:"mail1",resource:"mail1"}],truncated:true}}), event(6,"TOOL_STARTED",{tool:"read_app",tool_call_id:"read",arguments:JSON.stringify({provider:"gmail",action:"read",resource:"mail1"})}), event(7,"TOOL_COMPLETED",{tool:"read_app",tool_call_id:"read",result:{provider:"gmail",items:[{id:"mail1",resource:"mail1",text:"Delivery date: 2026-10-30"}],truncated:false}}), event(8,"RUN_COMPLETED")];
 await context.route(`**/api/v1/conversations/${conversationId}/events/history`, route => route.fulfill({json:{events}}));
 await page.goto(`/app/c/${conversationId}`);
 const input = page.getByRole("textbox",{name:"消息输入框",exact:true}); await input.fill("保留我的追问草稿");
 await expect(page.locator(".conversation-stream")).toContainText("交付日期是 10 月 30 日");
 await expect(page.locator(".conversation-stream .tool-activity")).toHaveCount(0);
 await openConversationRecords(page);
 const records = page.locator(".conversation-records");
 const group = records.locator(".process-group"); if (await group.count()) await group.first().locator(":scope > summary").click();
 await expect(records.locator(".tool-activity > summary")).toHaveText(["已完成：查看应用连接","已完成：搜索应用","已完成：读取应用"]);
 const search = records.locator(".tool-activity").filter({hasText:"已完成：搜索应用"}); await search.locator(":scope > summary").click();
 await expect(search).toContainText("gmail · search · newer_than:7d"); await expect(search).toContainText("仅返回了部分资料。"); await expect(search).not.toContainText("命令输出已截断"); await expect(search.locator("pre")).toHaveCount(0);
 await expect(input).toHaveValue("保留我的追问草稿"); expect(api.mutations).toEqual([]);
 await page.screenshot({path:info.outputPath("app-tool-progress.png"),fullPage:true});
});
