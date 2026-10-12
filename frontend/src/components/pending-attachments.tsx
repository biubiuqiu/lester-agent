"use client";
/* eslint-disable @next/next/no-img-element -- Temporary local object URLs are revoked on unmount. */

import { useEffect, useRef, useState } from "react";
import { FileText, X } from "lucide-react";
import { attachmentSize } from "@/lib/attachments";
import { useT } from "./i18n";

function LocalImage({ file, alt }: { file: File; alt: string }) {
  const image = useRef<HTMLImageElement>(null);
  useEffect(() => {
    const url = URL.createObjectURL(file);
    if (image.current) image.current.src = url;
    return () => URL.revokeObjectURL(url);
  }, [file]);
  return <img ref={image} alt={alt} />;
}

export function PendingAttachments({ files, disabled, onRemove }: { files: File[]; disabled: boolean; onRemove: (index: number) => void }) {
  const t = useT();
  const [selected, setSelected] = useState<File | null>(null);
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const node = dialog.current;
    if (selected && node && !node.open) node.showModal();
    if (!selected && node?.open) node.close();
  }, [selected]);
  return <>
    {files.length ? <div className="pending-attachments">{files.map((file, index) => {
      const image = /^image\/(png|jpeg|gif|webp|avif|bmp)$/.test(file.type);
      return <span key={`${file.name}-${file.size}-${file.lastModified}`} className="pending-attachment">
        {image ? <button type="button" className="attachment-thumbnail" aria-label={t("预览图片 {0}", [file.name])} onClick={() => setSelected(file)}><LocalImage file={file} alt="" /></button> : <FileText size={17} aria-hidden="true" />}
        <span className="attachment-label"><strong title={file.name}>{file.name}</strong><small>{attachmentSize(file.size)}</small></span>
        <button type="button" disabled={disabled} onClick={() => onRemove(index)} aria-label={t("移除 {0}", [file.name])}><X size={14} /></button>
      </span>;
    })}</div> : null}
    <dialog ref={dialog} className="attachment-preview-dialog" aria-label={t("图片预览")} onCancel={event => { event.preventDefault(); setSelected(null); }} onKeyDown={event => { if (event.key === "Escape") event.stopPropagation(); }}>
      {selected ? <><header><strong>{selected.name}</strong><button type="button" aria-label={t("关闭图片预览")} onClick={() => setSelected(null)}><X size={18} /></button></header><LocalImage file={selected} alt={selected.name} /><small>{attachmentSize(selected.size)}</small></> : null}
    </dialog>
  </>;
}
