import { test, expect } from "@playwright/test";
import { WorkspaceApi } from "./workspace-api";
import type { Automation, AutomationDelivery } from "../src/lib/automation";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { translator } from "../src/lib/i18n";

test("automations create explicit schedules, preserve edits and expose real trigger states",async({context,page},info)=>{
 const api=new WorkspaceApi(),errors:string[]=[];page.on("pageerror",e=>errors.push(e.message));await api.install(context);
 await context.addCookies([{name:"lester_access_token",value:"browser-test-access",url:"http://127.0.0.1:13020"},{name:"lester_locale",value:"zh-CN",url:"http://127.0.0.1:13020"}]);
 let items:Automation[]=[];const deliveries:AutomationDelivery[]=[];let triggers=0,receiptReads=0,saves=0;
 await context.route("**/api/v1/automations",route=>route.fulfill({json:{automations:items}}));
 await context.route(/\/api\/v1\/automations\/[^/?]+(?:\?.*)?$/,async route=>{
  const id=new URL(route.request().url()).pathname.split("/").at(-1)!;
  if(route.request().method()==="PUT"){saves++;const data=route.request().postDataJSON();expect(Object.keys(data).sort()).toEqual(["agent_slug","cron","enabled","kind","model_deployment_id","name","permission_mode","project_id","prompt","revision","run_at","timezone"].sort());const item={...data,id,created_by:"fixture-user",revision:data.revision+1,created_at:new Date().toISOString(),next_run_at:data.enabled?"2026-11-02T01:30:00Z":null} as Automation;items=[item,...items.filter(value=>value.id!==id)];return route.fulfill({json:{automation:item,secret:""}});}
  if(route.request().method()==="DELETE"){items=items.filter(value=>value.id!==id);return route.fulfill({status:204});}
  return route.fulfill({json:items.find(value=>value.id===id)});
 });
 await context.route("**/api/v1/automations/*/deliveries",route=>route.fulfill({json:{deliveries}}));
 await context.route("**/api/v1/automations/*/deliveries/*",route=>{receiptReads++;return route.fulfill({json:deliveries[0]});});
 await context.route("**/api/v1/automations/*/trigger",route=>{triggers++;const body=route.request().postDataJSON();deliveries.push({id:body.id,source:"manual",state:"pending",error_code:"",conversation_id:"new-conversation",has_conversation:false,run_id:null,run_status:"",scheduled_at:null,created_at:new Date().toISOString()});return route.fulfill({status:503,json:{error:"fixture receipt lost after acceptance"}});});
 try{
  await page.goto("/app/settings/automations");await expect(page).toHaveTitle("Lester");await expect(page.getByRole("heading",{name:"自动任务",exact:true})).toBeVisible();await expect(page.getByText("把下一次工作安排好",{exact:true})).toBeVisible();expect(saves).toBe(0);
  await page.getByRole("button",{name:"新建自动任务",exact:true}).click();let dialog=page.getByRole("dialog",{name:"新建自动任务",exact:true});
  await dialog.getByLabel("名称",{exact:true}).fill("每周项目简报");await dialog.getByRole("textbox",{name:"任务内容",exact:true}).fill("整理当前项目进展，输出一份简报。");await dialog.getByRole("combobox",{name:"重复",exact:true}).selectOption("weekly");await dialog.getByLabel("时间",{exact:true}).fill("09:30");await dialog.getByRole("combobox",{name:"星期",exact:true}).selectOption("1");await dialog.getByLabel("时区",{exact:true}).fill("Asia/Shanghai");
  await expect(dialog.getByLabel("启用此自动任务",{exact:true})).not.toBeChecked();await dialog.getByRole("button",{name:"保存",exact:true}).click();await expect(dialog).toHaveCount(0);expect(items[0].cron).toBe("30 9 * * 1");expect(items[0].enabled).toBe(false);expect(items[0].permission_mode).toBe("standard");expect(triggers).toBe(0);
  await page.getByRole("button",{name:"启用自动任务：每周项目简报",exact:true}).click();await expect(page.getByRole("button",{name:"暂停自动任务：每周项目简报",exact:true})).toBeVisible();
  await page.getByLabel("更多操作：每周项目简报",{exact:true}).click();await page.getByRole("button",{name:"编辑",exact:true}).click();dialog=page.getByRole("dialog",{name:"编辑自动任务",exact:true});await expect(dialog.getByRole("textbox",{name:"任务内容",exact:true})).toHaveValue("整理当前项目进展，输出一份简报。");await expect(dialog.getByRole("combobox",{name:"重复",exact:true})).toHaveValue("weekly");await page.screenshot({path:info.outputPath("automation-schedule.png")});await page.keyboard.press("Escape");await expect(dialog).toHaveCount(0);await expect(page.getByLabel("更多操作：每周项目简报",{exact:true})).toBeFocused();await expect(page.getByRole("button",{name:"删除",exact:true})).toHaveCount(0);
  await page.getByRole("button",{name:"立即运行",exact:true}).click();await expect(page.getByText("已加入触发队列",{exact:true})).toBeVisible();await expect(page.getByRole("region",{name:"触发记录",exact:true})).toContainText("等待触发");expect(triggers).toBe(1);expect(receiptReads).toBe(1);
  await page.screenshot({path:info.outputPath("automation-history.png")});await expect.poll(()=>page.locator(".settings-main").evaluate(e=>e.scrollWidth<=e.clientWidth+1)).toBe(true);
  await page.getByRole("button",{name:"暂停自动任务：每周项目简报",exact:true}).click();await expect(page.getByRole("button",{name:"启用自动任务：每周项目简报",exact:true})).toBeVisible();expect(triggers).toBe(1);
  await page.getByLabel("更多操作：每周项目简报",{exact:true}).click();await page.getByRole("button",{name:"删除",exact:true}).click();await page.getByRole("dialog",{name:"删除自动任务",exact:true}).getByRole("button",{name:"确认删除",exact:true}).click();await expect(page.getByText("把下一次工作安排好",{exact:true})).toBeVisible();await expect(page.getByRole("button",{name:"新建自动任务",exact:true})).toBeFocused();
  expect(api.unexpected).toEqual([]);expect(errors).toEqual([]);
 }finally{await api.close();}
});

test("webhook secrets are shown once, rotate explicitly and remain usable in all languages",async({context,page},info)=>{
 const api=new WorkspaceApi(),errors:string[]=[];page.on("pageerror",e=>errors.push(e.message));await api.install(context);await context.addCookies([{name:"lester_access_token",value:"browser-test-access",url:"http://127.0.0.1:13020"},{name:"lester_locale",value:"zh-CN",url:"http://127.0.0.1:13020"}]);
 let item:Automation|null=null,rotations=0,saves=0;
 await context.route("**/api/v1/automations",route=>route.fulfill({json:{automations:item?[item]:[]}}));
 await context.route(/\/api\/v1\/automations\/[^/?]+$/,route=>{const data=route.request().postDataJSON();saves++;item={...data,id:new URL(route.request().url()).pathname.split("/").at(-1)!,created_by:"fixture-user",revision:1,created_at:new Date().toISOString(),next_run_at:null};return route.fulfill({json:{automation:item,secret:"lwh_illustrative-secret-shown-once"}});});
 await context.route("**/api/v1/automations/*/rotate",route=>{rotations++;expect(route.request().postDataJSON()).toEqual({revision:1});item={...item!,revision:2};return route.fulfill({json:{automation:item,secret:"lwh_illustrative-rotated-secret"}});});
 try{
  await page.goto("/app/settings/automations");await page.getByRole("button",{name:"新建自动任务",exact:true}).click();let dialog=page.getByRole("dialog",{name:"新建自动任务",exact:true});await dialog.getByLabel("名称",{exact:true}).fill("Issue 分析");await dialog.getByRole("combobox",{name:"触发方式",exact:true}).selectOption("webhook");await dialog.getByRole("textbox",{name:"任务内容",exact:true}).fill("分析收到的 issue，给出修复建议。");await dialog.getByRole("button",{name:"保存",exact:true}).click();await expect(dialog).toContainText("lwh_illustrative-secret-shown-once");await expect(dialog).toContainText("X-Lester-Event-ID");expect(saves).toBe(1);await page.screenshot({path:info.outputPath("automation-webhook.png")});await dialog.getByRole("button",{name:"完成",exact:true}).click();
  await page.getByLabel("更多操作：Issue 分析",{exact:true}).click();await page.getByRole("button",{name:"编辑",exact:true}).click();dialog=page.getByRole("dialog",{name:"编辑自动任务",exact:true});await expect(dialog).not.toContainText("lwh_illustrative-secret-shown-once");await dialog.getByText("连接说明",{exact:true}).click();await dialog.getByRole("button",{name:"重置密钥",exact:true}).click();expect(rotations).toBe(0);await dialog.getByRole("button",{name:"确认重置密钥",exact:true}).click();await expect(dialog).toContainText("lwh_illustrative-rotated-secret");expect(rotations).toBe(1);await dialog.getByRole("button",{name:"完成",exact:true}).click();
  if(info.project.name==="mobile")await page.setViewportSize({width:320,height:740});
  for(const locale of ["en","ja","ko","fr","es"]){const t=translator(JSON.parse(readFileSync(join(process.cwd(),`src/lib/i18n/messages/${locale}.json`),"utf8")));await context.addCookies([{name:"lester_locale",value:locale,url:"http://127.0.0.1:13020"}]);await page.reload();await expect(page.getByRole("heading",{name:t("自动任务"),exact:true})).toBeVisible();await page.getByLabel(t("更多操作：{0}",["Issue 分析"]),{exact:true}).click();await page.getByRole("button",{name:t("编辑"),exact:true}).click();const editor=page.getByRole("dialog",{name:t("编辑自动任务"),exact:true});await expect(editor.getByLabel(t("名称"),{exact:true})).toHaveValue("Issue 分析");await expect(editor).not.toContainText("lwh_illustrative");await expect.poll(()=>editor.evaluate(e=>e.scrollWidth<=e.clientWidth+1)).toBe(true);if(locale==="fr")await page.screenshot({path:info.outputPath("automation-french.png")});await page.keyboard.press("Escape");}
  expect(rotations).toBe(1);expect(saves).toBe(1);expect(api.unexpected).toEqual([]);expect(errors).toEqual([]);
 }finally{await api.close();}
});

test("automation read failures show retry rather than an empty successful list",async({context,page})=>{
 const api=new WorkspaceApi();await api.install(context);await context.addCookies([{name:"lester_access_token",value:"browser-test-access",url:"http://127.0.0.1:13020"}]);let fail=true;
 await context.route("**/api/v1/automations",route=>route.fulfill(fail?{status:503,json:{error:"fixture unavailable"}}:{json:{automations:[]}}));
 try{await page.goto("/app/settings/automations");await expect(page.locator(".settings-error[role=alert]")).toContainText("自动任务加载失败");await expect(page.getByText("把下一次工作安排好",{exact:true})).toHaveCount(0);fail=false;await page.getByRole("button",{name:"刷新",exact:true}).click();await expect(page.getByText("把下一次工作安排好",{exact:true})).toBeVisible();expect(api.unexpected).toEqual([]);}finally{await api.close();}
});

test("slow list responses cannot revert confirmed enable or resurrect a deleted automation",async({context,page})=>{
 const api=new WorkspaceApi();await api.install(context);await context.addCookies([{name:"lester_access_token",value:"browser-test-access",url:"http://127.0.0.1:13020"}]);
 const item:Automation={id:"77777777-7777-4777-8777-777777777777",created_by:"fixture-user",name:"保持最新状态",kind:"webhook",project_id:"fixture-project",agent_slug:"lester",model_deployment_id:"fixture-model",prompt:"按事件处理",permission_mode:"standard",enabled:false,cron:"",timezone:"UTC",run_at:null,next_run_at:null,revision:1,created_at:new Date().toISOString()};
 let current:Automation|null=item,reads=0;let release:(()=>void)|undefined;let held:Promise<void>|null=null;
 await context.route("**/api/v1/automations",async route=>{reads++;const snapshot=current?[{...current}]:[];if(held){const gate=held;held=null;await gate;}await route.fulfill({json:{automations:snapshot}});});
 await context.route(`**/api/v1/automations/${item.id}?*`,route=>{current=null;return route.fulfill({status:204});});
 await context.route(`**/api/v1/automations/${item.id}`,route=>{const data=route.request().postDataJSON();current={...item,...data,revision:2};return route.fulfill({json:{automation:current,secret:""}});});
 const settle=()=>page.evaluate(()=>new Promise<void>(resolve=>requestAnimationFrame(()=>requestAnimationFrame(()=>resolve()))));
 try{
  await page.goto("/app/settings/automations");await expect(page.getByRole("heading",{name:item.name,exact:true})).toBeVisible();
  held=new Promise<void>(resolve=>{release=resolve;});let before=reads;await page.evaluate(()=>document.dispatchEvent(new Event("visibilitychange")));await expect.poll(()=>reads).toBeGreaterThan(before);
  await page.getByRole("button",{name:`启用自动任务：${item.name}`,exact:true}).click();await expect(page.getByRole("button",{name:`暂停自动任务：${item.name}`,exact:true})).toBeVisible();
  const oldEnable=page.waitForResponse(response=>new URL(response.url()).pathname==="/api/v1/automations");release!();await (await oldEnable).finished();await settle();await expect(page.getByRole("button",{name:`暂停自动任务：${item.name}`,exact:true})).toBeVisible();
  held=new Promise<void>(resolve=>{release=resolve;});before=reads;await page.evaluate(()=>document.dispatchEvent(new Event("visibilitychange")));await expect.poll(()=>reads).toBeGreaterThan(before);
  await page.getByLabel(`更多操作：${item.name}`,{exact:true}).click();await page.getByRole("button",{name:"删除",exact:true}).click();await page.getByRole("button",{name:"确认删除",exact:true}).click();await expect(page.getByText("把下一次工作安排好",{exact:true})).toBeVisible();
  const oldDelete=page.waitForResponse(response=>new URL(response.url()).pathname==="/api/v1/automations");release!();await (await oldDelete).finished();await settle();await expect(page.getByRole("heading",{name:item.name,exact:true})).toHaveCount(0);await expect(page.getByText("把下一次工作安排好",{exact:true})).toBeVisible();expect(api.unexpected).toEqual([]);
 }finally{release?.();await api.close();}
});
