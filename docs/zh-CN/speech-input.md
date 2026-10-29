# 语音输入

[文档目录](README.md) · [使用指南](usage.md) · [English](../speech-input.md)

语音输入把一段录音转成可编辑草稿，可用于任务输入框和独立个人助理。录音或转写不会自动发送消息、开始任务、加入队列或插入运行中的任务。

## 管理员配置

进入 **管理后台 → 语音输入**。功能默认关闭，配置在全站工作区共享。选择一个服务商，填写模型与密钥，启用并保存：

| 服务 | 默认 API 基础地址 | 默认模型 | 接口协议 |
| --- | --- | --- | --- |
| OpenAI 兼容接口 | `https://api.openai.com/v1` | `gpt-4o-transcribe` | Multipart `/audio/transcriptions`，Bearer |
| Groq Whisper | `https://api.groq.com/openai/v1` | `whisper-large-v3-turbo` | OpenAI 兼容转写 |
| Together Whisper | `https://api.together.xyz/v1` | `openai/whisper-large-v3` | OpenAI 兼容转写 |
| ElevenLabs Scribe | `https://api.elevenlabs.io/v1` | `scribe_v2` | Multipart `/speech-to-text`，`xi-api-key` |
| Deepgram Nova | `https://api.deepgram.com/v1` | `nova-3` | 原始编码音频 `/listen`，`Authorization: Token` |
| Mistral Voxtral | `https://api.mistral.ai/v1` | `voxtral-mini-latest` | Multipart `/audio/transcriptions`，Bearer |

选择服务后会自动填入地址和模型，模型仍可编辑。OpenAI 还提供 `gpt-4o-mini-transcribe`、`whisper-1` 建议，Groq 可选 `whisper-large-v3`，ElevenLabs 可选 `scribe_v1`。这些是配置预设，不是实时模型目录。语言范围、录音格式、账户权限和价格以所选服务与模型为准。保存不代表服务已经验证，需在实际浏览器和部署中用短录音确认。

Groq 和 Together 复用 OpenAI 兼容适配器。兼容聊天接口不代表支持转写：服务需接收 multipart `file`、`model`、可选 `language`、`response_format=json`，并在 JSON 的 `text` 字段返回文字。基础地址不要附加 `/audio/transcriptions`。自定义地址需使用 HTTPS，本地开发仅允许回环地址使用 HTTP。自托管 [Qwen3-ASR 的 vLLM 服务](https://github.com/QwenLM/Qwen3-ASR#deployment-with-vllm) 在配置好 OpenAI 转写接口和音频解码后可使用这一选项；这不等于接入了阿里云 DashScope。API 容器里的 localhost 指向容器自身，不是宿主机。

独立适配器遵循各家的实际协议。ElevenLabs 使用 `model_id` 和 `xi-api-key`，与其文字转语音接口独立。Deepgram 接收带媒体类型的原始编码音频，调用 `/listen?model=nova-3`，未指定语言时显式请求自动检测。Mistral 接收 multipart 音频，不发送 OpenAI 的 `response_format` 字段。Lester 不做音频编解码转换，也不会静默切换服务商或回退到另一个模型。Mistral 的请求协议已由官方 SDK 确认，但现有官方材料尚未确认浏览器 WebM/MP4 录音的接收范围，仍需实际录音验证。Deepgram 自动检测选择主要语言，支持范围以模型和接口为准；界面支持六种语言不代表每个语音模型都支持这六种语言。首次测试多语言录音可以优先选择已明确列出浏览器音频格式的 OpenAI、Groq 或 Together；本次没有进行真实服务的准确率比较。

密钥沿用现有主密钥在服务端加密，不返回浏览器、模型或 Computer。密钥输入留空会保留原值；切换服务商或接口域名后需要重新填写。清除密钥在保存后生效。多人同时修改配置时，需要先读取最新版本再保存，避免覆盖其他管理员的修改。

## 使用语音输入

管理员配置并启用后，输入框会出现“语音输入”。允许麦克风，开始说话，停止后转写；录音时取消会直接丢弃，不上传。识别结果可核对、修改后再明确发送。原有草稿会保留；当前任务正在运行时，发送仍使用原来的排队或补充操作。

网页需使用 HTTPS 或 localhost，并支持相应录音格式。Lester 按当前浏览器能力选择格式，不会假定所有设备都支持 WebM。前端每段录音最多两分钟，服务端音频上限为 10 MiB。时长上限是前端录音控制，不是服务端对任意上传媒体的实际时长检测。

识别语言自动判断，与界面语言、聊天模型分开；所选服务和模型需支持实际说话的语言。转写是单独配置的输入能力，不属于 Agent 工具或 Skill，音频不会进入沙箱。首版在录音完成后转写，不包含实时语音对话、朗读或后台麦克风。

离开页面、切换账号或进入后台会停止录音；迟到的结果不能写入另一个账号或会话。拒绝麦克风权限后仍可正常打字。iPhone、iPad 和 Android App 需要安装包含麦克风权限处理的新版客户端；生成文件预览与其他来源不能获得该权限。

## 隐私与结果恢复

Lester 不在服务端持久保存录音。音频会发给管理员配置的服务商，服务商仍遵循其账户与数据保留政策；此功能不承诺服务商端零保留。短期回执让原用户在对应工作区内恢复五分钟内的转写文字，过期后不可查询。后续转写请求会清理过期的私有文字，并删除超过一天的请求记录；这段时间保留请求身份，避免迟到的重试被当作新调用。确认发送后的文字沿用现有聊天记录规则；未发送的识别文字沿用草稿机制。

上传响应丢失时，只读取原请求回执，不自动重新上传或重复调用可能已执行的服务商请求。结果未知会明确提示。检查状态不会开始任务，也不会发送恢复出的文字。配置错误、服务不可用、没有识别出内容或回执过期，都应显示真实状态，不当成转写成功。

## 升级与验证

先备份数据库、停止 API 写入，按编号补齐迁移至 `000036_conversation_reads.up.sql`，再一起重建 **API 和 Web**。保留原有加密密钥和数据卷。新 Compose 初始化 001–036；已有数据卷需显式执行迁移。App 需重新构建以获得麦克风权限处理。配置语音不需要创建新的 Computer 或更新沙箱运行时。

035 只扩展允许的服务协议。回滚 035 前需先选择 OpenAI 兼容或 ElevenLabs 配置；当前选中 Deepgram/Mistral 时会拒绝回滚，避免丢弃配置。回滚 034 才会移除语音配置和短期转写回执。两者都不删除普通会话、任务文件或已发送消息，也不能撤销已经发出的服务商请求及其费用。

协议与浏览器测试使用有边界的音频和服务商 fixture，验证管理配置、密钥隐藏、录音状态、草稿、结果恢复与原生来源权限；不能据此声称真实服务商访问、识别准确率、设备麦克风质量或商店发布已验证。配置密钥后，需要在实际部署和支持的设备上验证短录音。

## 其他语音接口

上述集成面向录制完成的短音频。其他接口可能需要不同的生命周期或认证方式，把地址填入 OpenAI 兼容字段并不会自动实现它们的协议。

| 接口类型 | 接入时的区别 |
| --- | --- |
| AssemblyAI | 预录音接口需要上传、创建转写任务、轮询状态。独立的同步接口接收 WAV/PCM 短音频，浏览器的 WebM/MP4 录音需另行实现转换链路。这两种协议均未在本次接入。 |
| 签名云接口与实时语音 | 专用签名、地域、音频分帧、WebSocket/gRPC 流或异步任务需要独立适配与恢复规则，不会以不可用选项放入服务列表。 |
| 自托管模型 | 真正提供 OpenAI 兼容转写接口的服务可填写自定义地址。模型权重、解码依赖、算力、网络可达性和语言支持仍由部署方负责。 |

协议研究参考官方来源：[OpenAI 转写 SDK](https://github.com/openai/openai-python/blob/main/src/openai/resources/audio/transcriptions.py) · [Groq 转写 SDK](https://github.com/groq/groq-python/blob/main/src/groq/resources/audio/transcriptions.py) · [Together 转写 SDK](https://github.com/togethercomputer/together-python/blob/main/src/together/resources/audio/transcriptions.py) · [ElevenLabs 语音识别 SDK](https://github.com/elevenlabs/elevenlabs-python/blob/main/src/elevenlabs/speech_to_text/client.py) · [Deepgram SDK 与预录音示例](https://github.com/deepgram/deepgram-python-sdk) · [Deepgram API 定义](https://github.com/deepgram/deepgram-api-specs/blob/main/openapi.yml) · [Deepgram 语音识别说明](https://github.com/deepgram/skills/blob/main/skills/speech-to-text/SKILL.md) · [Mistral 转写 SDK](https://github.com/mistralai/client-python/blob/main/docs/sdks/transcriptions/README.md) · [AssemblyAI SDK](https://github.com/AssemblyAI/assemblyai-python-sdk) · [Qwen3-ASR 部署](https://github.com/QwenLM/Qwen3-ASR#deployment-with-vllm)。
