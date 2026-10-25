# Walkthrough production

The current video records the real production frontend with controlled REST/native HTTP SSE fixtures. A persistent caption identifies the task as **sample data, no live model**. It is an interface walkthrough, not evidence of model performance, backend writes, or queue consumption. Pending adjustment and queued task receipts deliberately remain pending; the recording does not imply that the model applied them. The retained file-review record is sample data too.

The story starts with the consolidated **+** menu, an explicit @ context reference and the model selector on the right. Auto approval is selected for ordinary sample file operations, independently of Plan. A scripted platform event then illustrates the Agent entering read-only Plan; it is not evidence of a live model judging task complexity. The story covers exact plan approval, default Enter-to-queue, explicit current-task clarification, automatic file tabs, HTML preview/source, retained change review, local `/recap`, accepted-context inspection, and closing/reopening a tab with an unfinished draft. It also opens Cloud tasks across sample conversations, filters a task waiting for plan review, shows background command status, and expands elapsed time/model usage. Reported token counts, estimates and missing output remain distinct; these fixture counters are not live measurements or billing. Opening, filtering and inspecting tasks must preserve the draft without sending or approving anything. The illustrative page uses the current “Lester. Make it happen.” direction. The Chinese UI has English instructional captions and no audio. The fourteen-second GIF samples the result preview, cloud task list and usage details; the MP4 contains the whole story.

To reproduce (Node/pnpm, Chromium, Python 3 and system ffmpeg with libx264/libvpx-vp9 required):

```bash
cd frontend
pnpm install --frozen-lockfile
pnpm build
pnpm exec playwright install chromium ffmpeg
pnpm exec playwright test --config playwright.walkthrough.config.ts
cd ..
python3 docs/media/encode-walkthrough.py /tmp/lester-walkthrough/walkthrough-record-the-current-workspace-story
```

Set `RUNNER_TEMP` to choose another output root and `PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH` to use an existing Chromium. The default recording output uses the system temporary directory. Do not run another build or browser server on port 13020 while recording. Recording is opt-in and excluded from the normal `e2e` test directory. An 84px recording-only footer reserves space for subtitles, keeping model, queue and Send/Stop controls visible; it does not change the shipped product. Fixed delays are caption reading time; assertions wait for actual UI state. Two ordered video clips capture closing the original tab and restoring its draft in a new tab using the same browser storage.

The encoder replaces the three tracked media files and normalizes full-range browser captures to limited-range H.264 for consistent browser playback. Check the output duration, `chapters.json`, captions and screenshots before updating the [walkthrough timing table](../walkthrough.md#watch-the-walkthrough). Never describe this fixture recording as a live model run or real generation-time measurement. Keep application credentials, output directories and browser traces out of Git.
