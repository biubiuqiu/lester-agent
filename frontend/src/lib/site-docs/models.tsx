import type { SiteDocSection } from "./types";
import { T } from "@/components/i18n";
import { Note } from "./shared";
import { repositoryURL } from "../site";

export const sections: SiteDocSection[] = [
{ id: "connection", title: "添加服务连接", content: <>
        <p><T>{"在“设置 → 模型”或首页的“配置第一个模型”中，选择 Provider 并填写访问凭证。已有可用模型时先展示模型列表，点击“添加模型”展开表单；新增服务连接可展开“服务商连接”，再点“添加连接”。连接名称可以留空；自定义服务可按需填写 Endpoint 和高级 JSON 配置。"}</T></p>
        <p><T>{"当前集成包含 OpenAI-compatible、Anthropic-compatible、Azure OpenAI、Vertex Anthropic、Foundry Anthropic 与 Bedrock。具体字段以界面和对应服务要求为准。"}</T></p>
        <Note><T>{"凭证在 Lester 数据库中加密保存。模型请求会发往你选择的服务；自托管 Lester 不意味着外部模型也在你的机器上运行。"}</T></Note>
      </> },
{ id: "deployment", title: "添加模型并设置默认", content: <>
        <p><T>{"保存连接后，添加服务支持的准确 Model ID。显示名称可以留空。首个模型默认作为个人默认模型，你也可以取消该选项或之后修改。"}</T></p>
        <p><T>{"模型名称与实际 Model ID 是不同字段。无法确定 ID 时，查看服务商的模型列表或部署配置，避免仅填写产品显示名称。"}</T></p>
      </> },
{ id: "verify", title: "用真实任务验证", content: <>
        <p><T>{"保存配置只检查本地规则。模型列表的“验证连接”会发送一次短文本请求，可能产生费用；成功只代表文本调用可用，工具、附件和完整任务仍需单独验证。共享模型也可验证，成员无法读取凭证或修改其预算。"}</T></p>
        <p><T>{"展开“上下文预算”可设置已知的模型窗口、输出预留和本地策略。0 表示窗口未知、自动预留或默认 64k 策略。每次请求统计系统提示词、工具定义、消息、参数和图片；供应商用量与估算分开保存，缓存只计算一次。预算不会限制普通回复长度，无法容纳的输入会明确报错。"}</T></p>
        <p><T>{"长会话接近预算时会整理历史，完整聊天记录仍保留。最新指令、未观察的结果和未解决的失败保留原文；后台任务从平台记录补回。Skill 正文按来源和版本去重，文件正文移出请求后必须重读。"}</T></p>
        <p><T>{"摘要是独立的无工具请求，有单独的用量记录；校验失败、取消或没有缩小请求时不会替换旧检查点。每轮最多整理两次，上下文过长只重试一次；无法安全容纳的内容会明确停止，不会自动重放工具或截断最新输入。"}</T></p>
        <p><T>{"回到原项目发送一个小任务，例如“创建一个简洁的 HTML 产品介绍页”。如果返回错误，检查凭证权限、Endpoint、Model ID 和网络连通性。"}</T></p>
      </> },
{ id: "speech", title: "语音服务配置", content: <>
        <p><T>{"管理员在管理后台的“语音输入”配置全站服务，可选择 OpenAI 兼容接口、Groq Whisper、Together Whisper、ElevenLabs Scribe、Deepgram Nova 或 Mistral Voxtral。选择服务时自动填入接口和默认模型，模型仍可编辑。填写对应密钥并启用后，成员才能看到录音入口。"}</T></p>
        <p><T>{"Groq 和 Together 使用 OpenAI 兼容转写协议；自定义服务需提供支持 /audio/transcriptions 的 API 基础地址，不能仅兼容聊天接口。ElevenLabs、Deepgram 和 Mistral 使用各自的转写适配器。密钥只在服务端加密保存，后台不会回显；留空保留原密钥，切换服务商或接口域名时需重新填写。"}</T></p>
        <p><T>{"支持的语言和录音格式以所选服务、模型为准。Lester 不转换音频格式，也不会自动切换服务或回退模型。保存配置不代表服务已验证，需在实际浏览器中用短录音确认格式、权限、余额和网络；识别效果也需自行核对。Mistral 对 WebM/MP4 的支持尚未从官方资料确认，配置后需实际录音验证。"}</T></p>
        <p><T>{"已有部署需备份并停止 API 写入，按顺序补齐迁移 001–037，再一起重建 API 和 Web，保留原加密密钥与数据卷。语音在 API 内调用，不使用 Computer、Skill 或 Agent 工具。App 需包含麦克风权限支持；生成文件预览不能请求录音。回滚 035 前需先切回 OpenAI 兼容或 ElevenLabs 配置；回滚 034 才会移除语音配置与短期回执，不删除任务、文件或普通聊天记录。"}</T></p>
      </> },
{ id: "shared", title: "管理员共享模型", content: <>
        <p><T>{"管理员可在独立管理后台配置共享模型。成员可以选择启用的共享模型，但无法读取其连接配置和凭证。个人默认模型优先于系统默认模型。"}</T></p>
        <p><T>{"注册账号默认是普通成员，首个注册账号不会自动成为管理员。首次管理员配置由部署负责人通过授权数据库会话完成，参见"}</T><a href={`${repositoryURL}/blob/main/docs/accounts-and-security.md#administration`} target="_blank" rel="noopener noreferrer"><T>{"仓库管理说明"}</T></a><T>{"。"}</T></p>
      </> }
];
