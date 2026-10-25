# Walkthrough

[Documentation](README.md) · [Project home](../README.md) · [简体中文](zh-CN/walkthrough.md)

The video, timestamps, sample-data limits and recording source.

## In this chapter

- [Watch the walkthrough](#watch-the-walkthrough)

## Watch the walkthrough

[![Lester walkthrough: materials, planning, results, cloud tasks and usage](media/lester-walkthrough.gif)](media/lester-walkthrough.mp4)

**[Watch or download the full MP4 — 97 seconds](media/lester-walkthrough.mp4)** · [Still preview](media/lester-walkthrough.png)

Recorded in the production frontend with Chinese UI and English instructional captions, without voiceover. **This is an illustrative task using controlled REST/SSE sample data, not a live model run or generation-speed benchmark.** The video shows the simplified composer and real interface controls; the Agent’s decision to enter Plan is scripted for the example, and queued tasks and current-task clarifications remain pending in this example. Cloud task states, background command counts, elapsed time and token usage are sample records too; reported counts, estimates and missing output remain distinct. The GIF previews results, cloud tasks and usage; select it for the full story. The recording predates the personal assistant and memory; it demonstrates the task workspace, now also reachable at `/app/new`. The recording uses the previous Cloud tasks label; the current entry is **Recent conversations → Task status**.

| Time | Operation |
| --- | --- |
| 00:00 | Add materials with +; choose the model on the right |
| 00:13 | Agent-selected read-only Plan, followed by explicit plan approval |
| 00:25 | Press Enter while running to queue the next task |
| 00:31 | Explicitly add a clarification to the current task |
| 00:39 | Inspect the HTML result in automatically opened file tabs |
| 00:46 | Switch between rendered preview and source |
| 00:53 | Review a retained before-and-after file change |
| 00:59 | Open /recap and inspect accepted context materials |
| 01:09 | Open cross-conversation cloud tasks and inspect background command status |
| 01:13 | Filter to tasks waiting for review without changing the draft |
| 01:18 | Inspect elapsed time and separately reported, estimated and missing usage |
| 01:25 | Leave a draft and close the tab |
| 01:30 | Reopen the conversation; the draft is restored without sending |

The recording starts in a signed-in sample workspace. Follow the [getting-started guide](getting-started.md) for a fresh installation. [Recording source and reproduction instructions](media/README.md) keep the walkthrough up to date with future releases.

---

[← Mobile clients](mobile.md)
