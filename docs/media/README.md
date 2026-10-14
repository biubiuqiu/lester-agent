# Walkthrough production

The current video records the real production frontend with controlled REST/native HTTP SSE fixtures. A persistent caption identifies the task as **sample data, no live model**. It is an interface walkthrough, not evidence of model performance, backend writes, or queue consumption. Pending adjustment and queued task receipts deliberately remain pending; the recording does not imply that the model applied them. The retained file-review record is sample data too.

The story covers Plan approval, default Enter-to-queue, explicit current-task clarification, automatic file tabs, HTML preview/source, retained change review, and closing/reopening a tab with an unfinished draft. The Chinese UI has English instructional captions and no audio. The GIF is a short preview/source teaser; the MP4 contains the whole story.

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

Set `RUNNER_TEMP` to choose another output root and `PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH` to use an existing Chromium. The default recording output uses the system temporary directory. Do not run another build or browser server on port 13020 while recording. Recording is opt-in and excluded from the normal `e2e` test directory. Fixed delays are caption reading time; assertions wait for actual UI state. Two ordered video clips capture closing the original tab and restoring its draft in a new tab using the same browser storage.

The encoder replaces the three tracked media files. Check the output duration, `chapters.json`, captions and screenshots before updating the README timing table. Never describe this fixture recording as a live model run or real generation-time measurement. Keep application credentials, output directories and browser traces out of Git.
