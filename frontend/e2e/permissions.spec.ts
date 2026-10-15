import { test as base, expect, type Page } from "@playwright/test";
import { WorkspaceApi, conversationId } from "./workspace-api";
const prefix = `/api/v1/conversations/${conversationId}/permissions`;
const id = "88888888-8888-4888-8888-888888888888";
const permission = { id, run_id: "fixture-run", tool: "write", request_digest: "d".repeat(64), status: "pending", allow_run: true, summary: { file_path: "index.html", content_bytes: 412 } };
const test=base.extend<{api:WorkspaceApi}>({api:async({context,page},runFixture)=>{const api=new WorkspaceApi(),errors:string[]=[];page.on("pageerror",e=>errors.push(e.message));await api.install(context);try{await runFixture(api);expect(api.unexpected).toEqual([]);expect(errors).toEqual([]);}finally{await api.close();}}});
async function signIn(page:Page){await page.goto("/app");await page.getByLabel("邮箱",{exact:true}).fill("browser@example.test");await page.getByLabel("密码",{exact:true}).fill("fixture-password-123");await page.getByRole("button",{name:"登录",exact:true}).click();await expect(page.getByRole("textbox",{name:"消息输入框",exact:true})).toBeVisible();}
async function send(page:Page){const input=page.getByRole("textbox",{name:"消息输入框",exact:true});await input.fill("创建一个极简网页");await input.press("Enter");await expect(page).toHaveURL(`/app/c/${conversationId}`);return input;}
test("permission choice persists independently of Plan and is captured for the accepted task",async({page,api},info)=>{
 await signIn(page);const input=page.getByRole("textbox",{name:"消息输入框",exact:true});await input.fill("我的草稿");
 await page.getByRole("button",{name:"权限设置",exact:true}).click();
 const menu=page.getByRole("group",{name:"权限设置",exact:true});await expect(menu).toBeVisible();await page.screenshot({path:info.outputPath("permission-menu.png")});
 await menu.getByRole("button").filter({hasText:"完全访问权限"}).click();await expect(input).toHaveValue("我的草稿");
 await page.reload();
 await expect(page.getByRole("button",{name:"权限设置",exact:true})).toContainText("完全访问权限");await expect(page.getByRole("button",{name:"先规划，再执行",exact:true})).toHaveCount(0);
 await send(page);const accepted=api.mutations.find(item=>item.path.endsWith("/messages"))!;expect(accepted.body.permission_mode).toBe("full");expect(accepted.body.execution_mode ?? "execute").toBe("execute");
 await page.getByRole("button",{name:"权限设置",exact:true}).click();await expect(menu).toContainText("权限选择仅用于下一任务");await menu.getByRole("button").filter({hasText:"自动审批"}).click();expect(api.mutations.filter(item=>item.path.endsWith("/messages"))).toHaveLength(1);
});
test("permission approval attaches above the input, confirms a lost receipt once and retains the draft",async({context,page,api},info)=>{
 let responses=0;
 await context.route(`**${prefix}`,r=>r.fulfill({json:{permissions:[permission]}}));
 await context.route(`**${prefix}/${id}/respond`,r=>{responses++;expect(r.request().postDataJSON()).toEqual({request_digest:permission.request_digest,decision:"once"});return r.fulfill({status:503,json:{error:"sample lost receipt"}});});
 await context.route(`**${prefix}/${id}`,r=>r.fulfill({json:{...permission,status:"approved",decision:"once"}}));
 await signIn(page);const input=await send(page);await expect(page.getByRole("button",{name:"允许一次",exact:true})).toBeVisible();await expect.poll(()=>api.streamCount).toBeGreaterThan(0);api.permissionEvent("PERMISSION_REQUESTED",{permission_id:id,tool:"write"});await expect(page.getByText("需要你的确认",{exact:true})).toBeVisible();await input.fill("审批时保留的补充草稿");
 await expect(page.locator(".composer-dock .permission-card")).toContainText("index.html");await expect(page.locator(".thread .permission-card")).toHaveCount(0);
 await expect.poll(async()=>{const b=await input.boundingBox();return b&&b.y+b.height<=page.viewportSize()!.height;}).toBe(true);
 await page.screenshot({path:info.outputPath("permission-approval.png")});
 await page.getByRole("button",{name:"允许一次",exact:true}).click();await expect(page.locator(".permission-card")).toHaveCount(0);await expect(input).toHaveValue("审批时保留的补充草稿");expect(responses).toBe(1);
 await page.locator(".permission-history>summary").click();await expect(page.locator(".permission-history")).toContainText("已允许");
 api.finish();await expect(input).toHaveValue("审批时保留的补充草稿");
});
test("command approval offers no task-wide grant, and rejection never sends the composer draft",async({context,page,api},info)=>{
 let denied=false;
 await context.route(`**${prefix}`,r=>r.fulfill({json:{permissions:[{...permission,tool:"bash",allow_run:false,summary:{command:"rm temporary.txt"},status:denied?"rejected":"pending",...(denied?{decision:"reject"}:{})}]}}));
 await context.route(`**${prefix}/${id}/respond`,r=>{expect(r.request().postDataJSON().decision).toBe("reject");denied=true;return r.fulfill({json:{...permission,tool:"bash",status:"rejected",decision:"reject",allow_run:false,summary:{command:"rm temporary.txt"}}});});
 await signIn(page);const input=await send(page);await input.fill("未发送的草稿");await expect(page.locator(".permission-card")).toContainText("rm temporary.txt");await expect(page.getByRole("button",{name:"本次任务允许此类操作",exact:true})).toHaveCount(0);
 await page.screenshot({path:info.outputPath("permission-command.png")});await page.getByRole("button",{name:"拒绝",exact:true}).click();await expect(page.locator(".permission-card")).toHaveCount(0);await expect(input).toHaveValue("未发送的草稿");expect(api.mutations.filter(item=>item.path.endsWith("/messages"))).toHaveLength(1);
});

test("queued tasks show their captured permissions after the selector changes",async({context,page,api})=>{
 let accepted: Record<string, unknown> | undefined;
 const inputs=`/api/v1/conversations/${conversationId}/permissions/inputs`;
 await context.route(`**${inputs}`,route=>{
  if(route.request().method()==="GET")return route.fulfill({json:{inputs:accepted?[accepted]:[]}});
  const request=route.request().postDataJSON();expect(request.permission_mode).toBe("full");accepted={id:request.id,after_run_id:request.run_id,kind:"next",status:"queued",revision:1,content:request.content,request,accepted_order:1};return route.fulfill({status:202,json:accepted});
 });
 await signIn(page);const input=await send(page);await page.getByRole("button",{name:"权限设置",exact:true}).click();await page.getByRole("group",{name:"权限设置",exact:true}).getByRole("button").filter({hasText:"完全访问权限"}).click();
 await input.fill("接下来的任务");await input.press("Enter");await expect(page.locator(".composer-queue-item")).toHaveCount(1);
 await page.getByRole("button",{name:"权限设置",exact:true}).click();await page.getByRole("group",{name:"权限设置",exact:true}).getByRole("button").filter({hasText:"自动审批"}).click();
 await page.locator(".queue-message summary").click();await expect(page.locator(".queue-permission")).toContainText("完全访问权限");expect(api.mutations.filter(item=>item.path.endsWith("/messages"))).toHaveLength(1);
});

test("an old API cannot silently execute a default-permission task",async({context,page,api})=>{
 let legacyPosts=0;
 await context.route(`**/api/v1/conversations/${conversationId}/permissions/messages`,route=>route.fulfill({status:404,json:{error:"sample old API"}}));
 await context.route(`**/api/v1/conversations/${conversationId}/messages`,route=>{legacyPosts++;return route.fulfill({status:202,json:{run_id:"unchecked-old-run"}});});
 await signIn(page);const input=page.getByRole("textbox",{name:"消息输入框",exact:true});await input.fill("不能静默跳过审批的草稿");await input.press("Enter");
 await expect(page).toHaveURL(`/app/c/${conversationId}`);await expect(page.getByRole("textbox",{name:"消息输入框",exact:true})).toHaveValue("不能静默跳过审批的草稿");await expect(page.getByText(/权限服务不可用/)).toBeVisible();expect(legacyPosts).toBe(0);expect(api.mutations.filter(item=>item.path.endsWith("/messages"))).toHaveLength(0);
});
