import { test, expect } from "@playwright/test";
import { WorkspaceApi, conversationId, prompt } from "./workspace-api";
test("add menu groups materials, preserves drafts and leaves model and round send on the right",async({page,context},info)=>{
 const api=new WorkspaceApi(),errors:string[]=[];page.on("pageerror",e=>errors.push(e.message));await api.install(context);
 try {
  await page.goto("/app/new");await expect(page).toHaveTitle(/Lester/);await page.getByLabel("邮箱",{exact:true}).fill("browser@example.test");await page.getByLabel("密码",{exact:true}).fill("fixture-password-123");await page.getByRole("button",{name:"登录",exact:true}).click();
  const input=page.getByRole("textbox",{name:"消息输入框",exact:true}),add=page.getByRole("button",{name:"添加内容",exact:true});await input.fill("保留我的草稿");const before=api.mutations.length;
  await expect(page.getByRole("button",{name:"@ 添加引用",exact:true})).toHaveCount(0);await expect(page.getByRole("button",{name:"先规划，再执行",exact:true})).toHaveCount(0);
  await add.click();await expect(page.getByRole("button",{name:"添加附件",exact:true})).toBeVisible();await expect(page.getByRole("button",{name:"选择 Agent",exact:true})).toBeVisible();await page.screenshot({path:info.outputPath("add-menu.png")});
  const chooser=page.waitForEvent("filechooser");await page.getByRole("button",{name:"添加附件",exact:true}).click();await (await chooser).setFiles({name:"brief.txt",mimeType:"text/plain",buffer:Buffer.from("fixture")});await expect(page.locator(".pending-attachments")).toContainText("brief.txt");await expect(input).toHaveValue("保留我的草稿");
  await add.click();await page.keyboard.press("Escape");await expect(add).toBeFocused();await expect(page.locator(".composer-add-menu")).toHaveCount(0);expect(api.mutations).toHaveLength(before);
  await page.getByRole("button",{name:"移除 brief.txt",exact:true}).click();await expect(input).toHaveValue("保留我的草稿");
  const model=page.getByRole("combobox",{name:"当前模型",exact:true}),send=page.getByRole("button",{name:"发送消息",exact:true});
  const mb=await model.boundingBox(),sb=await send.boundingBox(),ab=await add.boundingBox();expect(mb!.x).toBeGreaterThan(ab!.x+ab!.width);expect(sb!.x).toBeGreaterThan(mb!.x);
  expect(await send.evaluate(e=>getComputedStyle(e).borderRadius)).toBe("999px");await input.fill(prompt);await input.press("Enter");await expect(page).toHaveURL(`/app/c/${conversationId}`);await expect(model).toBeDisabled();await expect(page.locator(".composer-motion-ghost")).toHaveCount(0);
  await input.fill("准备下一步");if(info.project.name==="mobile")await page.setViewportSize({width:320,height:740});
  const bounds=await page.locator(".compose-actions").locator("button,select").evaluateAll(elements=>elements.map(element=>{const r=element.getBoundingClientRect();return {x:r.x,right:r.right};}));
  for(const b of bounds){expect(b.x).toBeGreaterThanOrEqual(0);expect(b.right).toBeLessThanOrEqual(page.viewportSize()!.width+1);}
  await page.screenshot({path:info.outputPath("running-composer.png")});expect(api.unexpected).toEqual([]);expect(errors).toEqual([]);
 } finally {await context.close();await api.close();}
});
