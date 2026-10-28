# Voice input

[Documentation](README.md) · [User guide](usage.md) · [简体中文](zh-CN/speech-input.md)

Voice input records a short message and turns it into an editable draft. It works in task composers and the separate personal assistant. Recording or transcription never sends a message, starts a task, queues a follow-up or inserts an adjustment.

## Administrator setup

Open **Administration → Voice input**. The service is disabled by default and shared across workspaces. Choose one provider, enter its model and credential, then enable and save:

| Provider | Default API base | Default model |
| --- | --- | --- |
| OpenAI-compatible | `https://api.openai.com/v1` | `gpt-4o-transcribe` |
| ElevenLabs | `https://api.elevenlabs.io/v1` | `scribe_v2` |

The OpenAI-compatible adapter sends multipart audio to `/audio/transcriptions` with Bearer authentication. A compatible chat endpoint does not necessarily support transcription. An administrator can configure a compatible self-hosted service using a permitted API base; do not append `/audio/transcriptions` to the base field.

ElevenLabs has its own speech-to-text API at `/v1/speech-to-text`, using `xi-api-key`, `model_id` and an audio file. This is separate from ElevenLabs text-to-speech. Provider availability, account permissions, billing and recognition quality must be checked with actual recordings; saving configuration is not a successful provider test.

Credentials are encrypted on the server with the existing master key. They are not returned to the browser, models or Computers. A blank credential field preserves the saved key; switching provider or API origin requires a new key. Clearing the key takes effect on save. Concurrent admin edits require refreshing the configuration revision before saving again.

## Use voice input

Once an administrator has configured and enabled the service, the composer exposes **Voice input**. Allow microphone access, speak, then stop to transcribe. Cancel while recording to discard it without uploading. Review and edit the text before explicitly sending it. Existing drafts remain intact; an active task still uses the ordinary queue/adjustment controls when you choose to send.

The browser needs HTTPS or localhost and a supported recording format. Lester selects a format supported by the current browser rather than assuming WebM on every device. Frontend recording is limited to two minutes; the server accepts at most 10 MiB of audio. The duration limit is a client recording control, not a server-side measurement of arbitrary uploaded media.

Recognition language is detected independently of the interface language and the selected chat model. Transcription is a separate configured capability, not an Agent tool or Skill. Audio does not enter the sandbox. This release transcribes completed recordings; it does not provide a real-time voice conversation, spoken replies or a background microphone.

Leaving the page, changing account or going into the background stops capture. Late results cannot be inserted into a different account or conversation. Denying microphone permission leaves ordinary typing available. The iOS/iPadOS and Android apps need a rebuilt client containing the microphone permission support; generated file previews and other origins cannot receive that permission.

## Privacy and recovery

Lester does not persist the recording on the server. Audio is sent to the configured provider, whose retention and account policies still apply; this feature does not promise provider-side zero retention. A short-lived transcription receipt allows the original user/workspace to recover the text for five minutes. Expired receipts cannot be read. Later transcription requests purge expired private text and remove request records older than one day; the remaining request identity prevents a late retry from being treated as a new call during that window. Text that you explicitly send follows the existing chat storage rules; unsent recognized text uses the existing draft storage.

If the upload response is lost, recovery only reads the original request receipt. It never automatically uploads again or repeats a possibly completed provider call. An unknown result remains explicit. Checking status does not submit a task or send the recovered text. Configuration errors, unavailable services, empty recognition and expired receipts must remain visible rather than being reported as success.

## Upgrade and verification

Back up the database and stop API writes. Apply all missing migrations through `000034_speech_input.up.sql`, then rebuild **API and Web** together. Preserve the existing encryption key and volumes. Fresh Compose initializes 001–034; existing volumes need explicit migration. Rebuild native clients to receive microphone permission handling. Speech setup does not require a new Computer or sandbox runtime.

Rollback removes speech configuration and short-lived transcription receipts. It does not delete ordinary conversations, task files or already sent messages, and cannot undo a provider request or its cost.

Protocol and browser tests use bounded audio and provider fixtures. They check administration, secret masking, recording states, drafts, recovery and native origin permissions; they do not prove live provider access, recognition accuracy, device microphone quality or app-store deployment. Validate a short recording on the actual deployment and supported devices after configuring credentials.

Provider references: [OpenAI transcription SDK](https://github.com/openai/openai-python/blob/main/src/openai/resources/audio/transcriptions.py) · [ElevenLabs speech-to-text SDK](https://github.com/elevenlabs/elevenlabs-python/blob/main/src/elevenlabs/speech_to_text/client.py).
