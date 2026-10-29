# Voice input

[Documentation](README.md) · [User guide](usage.md) · [简体中文](zh-CN/speech-input.md)

Voice input records a short message and turns it into an editable draft. It works in task composers and the separate personal assistant. Recording or transcription never sends a message, starts a task, queues a follow-up or inserts an adjustment.

## Administrator setup

Open **Administration → Voice input**. The service is disabled by default and shared across workspaces. Choose one provider, enter its model and credential, then enable and save:

| Service | Default API base | Default model | Protocol |
| --- | --- | --- | --- |
| OpenAI-compatible | `https://api.openai.com/v1` | `gpt-4o-transcribe` | Multipart `/audio/transcriptions`, Bearer |
| Groq Whisper | `https://api.groq.com/openai/v1` | `whisper-large-v3-turbo` | OpenAI-compatible transcription |
| Together Whisper | `https://api.together.xyz/v1` | `openai/whisper-large-v3` | OpenAI-compatible transcription |
| ElevenLabs Scribe | `https://api.elevenlabs.io/v1` | `scribe_v2` | Multipart `/speech-to-text`, `xi-api-key` |
| Deepgram Nova | `https://api.deepgram.com/v1` | `nova-3` | Encoded audio `/listen`, `Authorization: Token` |
| Mistral Voxtral | `https://api.mistral.ai/v1` | `voxtral-mini-latest` | Multipart `/audio/transcriptions`, Bearer |

Selecting a service fills its API base and model; the model remains editable. OpenAI suggestions include `gpt-4o-mini-transcribe` and `whisper-1`; Groq also offers `whisper-large-v3`, and ElevenLabs offers `scribe_v1`. Suggestions are configuration defaults, not a live model catalogue. Supported languages, recording formats, account access and pricing depend on the selected service and model. Saving is not a successful provider test; check a short recording on your actual browser and deployment.

Groq and Together reuse the OpenAI-compatible adapter. A compatible chat endpoint does not necessarily support transcription: it must accept multipart `file`, `model`, optional `language` and `response_format=json`, and return a JSON `text` field. Enter the API base without `/audio/transcriptions`. Custom bases require HTTPS, except loopback HTTP for local development. Self-hosted [Qwen3-ASR with vLLM](https://github.com/QwenLM/Qwen3-ASR#deployment-with-vllm) can use this option when its OpenAI transcription endpoint and audio decoder are configured; this is not an Alibaba Cloud DashScope integration. From an API container, localhost refers to that container, not the host machine.

The native adapters preserve the providers' different protocols. ElevenLabs uses `model_id` and `xi-api-key`, independently of its text-to-speech API. Deepgram receives the original encoded audio with its media type, uses `/listen?model=nova-3` and requests automatic language detection when no language is supplied. Mistral accepts multipart audio but does not receive OpenAI's `response_format` field. Lester does not convert audio between codecs, silently change providers or fall back to another model. Mistral’s request protocol is verified from its official SDK, but acceptance of browser WebM/MP4 recordings could not be confirmed from the available official material and needs a real recording test. Deepgram automatic detection selects the dominant language; its available languages depend on the model and API. Six interface languages do not guarantee six-language speech support. OpenAI, Groq or Together are practical starting points for a multilingual recording test because their documented upload formats include browser audio formats; no live accuracy comparison was performed.

Credentials are encrypted on the server with the existing master key. They are not returned to the browser, models or Computers. A blank credential field preserves the saved key; switching provider or API origin requires a new key. Clearing the key takes effect on save. Concurrent admin edits require refreshing the configuration revision before saving again.

## Use voice input

Once an administrator has configured and enabled the service, the composer exposes **Voice input**. Allow microphone access, speak, then stop to transcribe. Cancel while recording to discard it without uploading. Review and edit the text before explicitly sending it. Existing drafts remain intact; an active task still uses the ordinary queue/adjustment controls when you choose to send.

The browser needs HTTPS or localhost and a supported recording format. Lester selects a format supported by the current browser rather than assuming WebM on every device. Frontend recording is limited to two minutes; the server accepts at most 10 MiB of audio. The duration limit is a client recording control, not a server-side measurement of arbitrary uploaded media.

Recognition language is detected independently of the interface language and the selected chat model; the provider/model must support the language being spoken. Transcription is a separate configured capability, not an Agent tool or Skill. Audio does not enter the sandbox. This release transcribes completed recordings; it does not provide a real-time voice conversation, spoken replies or a background microphone.

Leaving the page, changing account or going into the background stops capture. Late results cannot be inserted into a different account or conversation. Denying microphone permission leaves ordinary typing available. The iOS/iPadOS and Android apps need a rebuilt client containing the microphone permission support; generated file previews and other origins cannot receive that permission.

## Privacy and recovery

Lester does not persist the recording on the server. Audio is sent to the configured provider, whose retention and account policies still apply; this feature does not promise provider-side zero retention. A short-lived transcription receipt allows the original user/workspace to recover the text for five minutes. Expired receipts cannot be read. Later transcription requests purge expired private text and remove request records older than one day; the remaining request identity prevents a late retry from being treated as a new call during that window. Text that you explicitly send follows the existing chat storage rules; unsent recognized text uses the existing draft storage.

If the upload response is lost, recovery only reads the original request receipt. It never automatically uploads again or repeats a possibly completed provider call. An unknown result remains explicit. Checking status does not submit a task or send the recovered text. Configuration errors, unavailable services, empty recognition and expired receipts must remain visible rather than being reported as success.

## Upgrade and verification

Back up the database and stop API writes. Apply all missing migrations through `000036_conversation_reads.up.sql`, then rebuild **API and Web** together. Preserve the existing encryption key and volumes. Fresh Compose initializes 001–036; existing volumes need explicit migration. Rebuild native clients to receive microphone permission handling. Speech setup does not require a new Computer or sandbox runtime.

Migration 035 only extends the allowed provider protocols. Its rollback requires selecting OpenAI-compatible or ElevenLabs settings first; it refuses to discard a selected Deepgram/Mistral configuration. Rolling back 034 removes speech configuration and short-lived transcription receipts. Neither rollback deletes ordinary conversations, task files or already sent messages, or undoes a provider request and its cost.

Protocol and browser tests use bounded audio and provider fixtures. They check administration, secret masking, recording states, drafts, recovery and native origin permissions; they do not prove live provider access, recognition accuracy, device microphone quality or app-store deployment. Validate a short recording on the actual deployment and supported devices after configuring credentials.

## Other speech APIs

The integrations above serve short, completed recordings. Other APIs need a different lifecycle or authentication; entering their URL into an OpenAI-compatible field does not implement those protocols.

| API family | Integration implications |
| --- | --- |
| AssemblyAI | Its prerecorded API uploads audio, creates a transcript job and polls its status. The separate synchronous API accepts WAV/PCM short clips; browser WebM/MP4 recordings need an explicitly implemented conversion path. Neither protocol is included here. |
| Signed cloud APIs and realtime speech | Provider-specific signatures, regions, audio framing, WebSocket/gRPC streams or asynchronous jobs need dedicated adapters and recovery rules. These are not added as nonfunctional provider choices. |
| Self-hosted models | An actual OpenAI-compatible transcription server can use the custom endpoint option. Model weights, decoding dependencies, compute, network reachability and language support remain the operator's responsibility. |

Official sources used to compare the protocols: [OpenAI transcription SDK](https://github.com/openai/openai-python/blob/main/src/openai/resources/audio/transcriptions.py) · [Groq transcription SDK](https://github.com/groq/groq-python/blob/main/src/groq/resources/audio/transcriptions.py) · [Together transcription SDK](https://github.com/togethercomputer/together-python/blob/main/src/together/resources/audio/transcriptions.py) · [ElevenLabs speech-to-text SDK](https://github.com/elevenlabs/elevenlabs-python/blob/main/src/elevenlabs/speech_to_text/client.py) · [Deepgram SDK and prerecorded examples](https://github.com/deepgram/deepgram-python-sdk) · [Deepgram API specification](https://github.com/deepgram/deepgram-api-specs/blob/main/openapi.yml) · [Deepgram speech-to-text guidance](https://github.com/deepgram/skills/blob/main/skills/speech-to-text/SKILL.md) · [Mistral transcription SDK](https://github.com/mistralai/client-python/blob/main/docs/sdks/transcriptions/README.md) · [AssemblyAI SDK](https://github.com/AssemblyAI/assemblyai-python-sdk) · [Qwen3-ASR deployment](https://github.com/QwenLM/Qwen3-ASR#deployment-with-vllm).
