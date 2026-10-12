export const maxAttachmentBytes = 25 << 20;
export function appendAttachments(current: File[], incoming: File[]) {
  const files = [...current], oversized: string[] = [], duplicates: string[] = [];
  for (const file of incoming) {
    if (file.size > maxAttachmentBytes) { oversized.push(file.name); continue; }
    if (files.some(item => item.name === file.name && item.size === file.size && item.lastModified === file.lastModified && item.type === file.type)) { duplicates.push(file.name); continue; }
    files.push(file);
  }
  return { files, oversized, duplicates };
}

export function attachmentSize(size: number) {
  return size < 1024 ? `${size} B` : size < (1 << 20) ? `${(size / 1024).toFixed(1)} KiB` : `${(size / (1 << 20)).toFixed(1)} MiB`;
}
