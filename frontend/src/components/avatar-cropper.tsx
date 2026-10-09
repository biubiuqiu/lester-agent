"use client";

import { useEffect, useRef, useState, type PointerEvent } from "react";
import { RotateCw, Undo2, X } from "lucide-react";
import { cropGeometry, cropSize, drawAvatarCrop, initialCrop, type Crop } from "@/lib/avatar-crop";

export function AvatarCropper({ file, onCancel, onSave }: { file: File; onCancel: () => void; onSave: (file: File) => Promise<void> }) {
  const dialog = useRef<HTMLDialogElement>(null);
  const canvas = useRef<HTMLCanvasElement>(null);
  const [image, setImage] = useState<ImageBitmap | null>(null);
  const [crop, setCrop] = useState<Crop>(initialCrop);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const pending = useRef(false);
  const pointers = useRef(new Map<number, { x: number; y: number }>());
  useEffect(() => {
    const node = dialog.current;
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    node?.showModal();
    return () => { node?.close(); previous?.focus(); };
  }, []);
  useEffect(() => {
    let active = true;
    let bitmap: ImageBitmap | undefined;
    // Decode a frozen bitmap, including the first GIF frame and EXIF orientation.
    // Preview and export then use the same pixels even after a long edit.
    void createImageBitmap(file).then(photo => {
      if (!active) { photo.close(); return; }
      bitmap = photo;
      if (photo.width > 4096 || photo.height > 4096 || photo.width * photo.height > 12000000) {
        photo.close(); bitmap = undefined;
        setError("图片尺寸过大，请使用不超过 4096 像素和 1200 万像素的图片。"); return;
      }
      setImage(photo);
    }).catch(() => { if (active) setError("图片无法读取，请重新选择 PNG、JPEG 或 GIF 图片。"); });
    return () => { active = false; bitmap?.close(); };
  }, [file]);
  useEffect(() => {
    const context = canvas.current?.getContext("2d");
    if (image && context) drawAvatarCrop(context, image, crop, cropSize);
  }, [image, crop]);
  function update(change: (value: Crop) => Crop) {
    if (!image || busy) return;
    setCrop(value => cropGeometry(image.width, image.height, change(value)).crop);
  }
  function move(event: PointerEvent<HTMLCanvasElement>) {
    const previous = pointers.current.get(event.pointerId);
    if (!previous || busy) return;
    const next = { x: event.clientX, y: event.clientY };
    const other = [...pointers.current.entries()].find(([id]) => id !== event.pointerId)?.[1];
    pointers.current.set(event.pointerId, next);
    if (other) {
      const before = Math.hypot(previous.x - other.x, previous.y - other.y);
      const after = Math.hypot(next.x - other.x, next.y - other.y);
      if (before > 0) update(value => ({ ...value, zoom: value.zoom * after / before }));
    } else {
      const ratio = cropSize / event.currentTarget.getBoundingClientRect().width;
      update(value => ({ ...value, x: value.x + (next.x - previous.x) * ratio, y: value.y + (next.y - previous.y) * ratio }));
    }
  }
  async function save() {
    if (!image || pending.current) return;
    pending.current = true; setBusy(true); setError("");
    try {
      const output = document.createElement("canvas"); output.width = output.height = 256;
      const context = output.getContext("2d");
      if (!context) throw new Error("浏览器无法导出图片，请重试。");
      drawAvatarCrop(context, image, crop, 256);
      const blob = await new Promise<Blob>((resolve, reject) => output.toBlob(value => value ? resolve(value) : reject(new Error("图片导出失败，请重试。")), "image/png"));
      await onSave(new File([blob], "avatar.png", { type: "image/png" }));
      onCancel();
    } catch (reason) { setError(reason instanceof Error ? reason.message : "头像保存失败，请重试。"); }
    finally { pending.current = false; setBusy(false); }
  }
  return <dialog ref={dialog} className="avatar-crop-dialog" aria-labelledby="avatar-crop-title" onKeyDown={event => { if (event.key === "Escape") event.stopPropagation(); }} onCancel={event => { event.preventDefault(); if (!busy) onCancel(); }}>
    <header><div><h2 id="avatar-crop-title">裁剪头像</h2><p id="avatar-crop-help">拖动调整位置，缩放选择范围。手机支持双指缩放。</p></div><button type="button" className="icon-button" aria-label="取消裁剪" disabled={busy} onClick={onCancel}><X aria-hidden="true" /></button></header>
    <div className="avatar-crop-viewport"><canvas ref={canvas} width={cropSize} height={cropSize} role="img" tabIndex={0} aria-label="头像裁剪预览，使用方向键移动图片" aria-describedby="avatar-crop-help" onPointerDown={event => { if (!busy && image) { event.currentTarget.setPointerCapture(event.pointerId); pointers.current.set(event.pointerId, { x: event.clientX, y: event.clientY }); } }} onPointerMove={move} onPointerUp={event => pointers.current.delete(event.pointerId)} onPointerCancel={event => pointers.current.delete(event.pointerId)} onLostPointerCapture={event => pointers.current.delete(event.pointerId)} onKeyDown={event => {
      if (!["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"].includes(event.key)) return;
      event.preventDefault(); const step = event.shiftKey ? 20 : 5;
      update(value => ({ ...value, x: value.x + (event.key === "ArrowLeft" ? -step : event.key === "ArrowRight" ? step : 0), y: value.y + (event.key === "ArrowUp" ? -step : event.key === "ArrowDown" ? step : 0) }));
    }} /><span className="avatar-crop-mask" aria-hidden="true" /></div>
    {!image && !error && <p role="status">正在读取图片…</p>}
    <label className="avatar-crop-zoom">缩放 <input type="range" min="1" max="4" step="0.01" value={crop.zoom} disabled={!image || busy} onChange={event => update(value => ({ ...value, zoom: Number(event.target.value) }))} /><output>{Math.round(crop.zoom * 100)}%</output></label>
    <div className="avatar-crop-tools"><button type="button" className="secondary-button" disabled={!image || busy} onClick={() => update(value => ({ ...value, rotation: (value.rotation + 90) % 360, x: 0, y: 0 }))}><RotateCw size={16} aria-hidden="true" />旋转</button><button type="button" className="text-button" disabled={!image || busy} onClick={() => setCrop(initialCrop)}><Undo2 size={16} aria-hidden="true" />重置</button></div>
    {error && <p className="settings-error" role="alert">{error}</p>}
    <footer><button type="button" className="secondary-button" disabled={busy} onClick={onCancel}>取消</button><button type="button" className="primary-button" disabled={!image || busy} onClick={() => void save()}>{busy ? "保存中…" : "保存头像"}</button></footer>
  </dialog>;
}
