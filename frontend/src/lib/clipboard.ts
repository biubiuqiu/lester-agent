const clipboardImageExtensions: Record<string, string> = {
  avif: "avif",
  bmp: "bmp",
  gif: "gif",
  jpeg: "jpg",
  png: "png",
  "svg+xml": "svg",
  tiff: "tiff",
  webp: "webp",
};

/** Return image files from a paste event without reading their contents. */
export function pastedImageFiles(event: { clipboardData?: DataTransfer | null }): File[] {
  const items = Array.from(event.clipboardData?.items ?? []);
  const pastedAt = Date.now();
  return items.flatMap((item, index) => {
    if (item.kind !== "file" || !item.type.startsWith("image/")) return [];
    const file = item.getAsFile();
    if (!file) return [];
    if (file.name) return [file];
    const subtype = item.type.slice("image/".length).toLowerCase();
    const extension = clipboardImageExtensions[subtype] || "png";
    return [new File([file], `pasted-image-${pastedAt}-${index}.${extension}`, { type: item.type, lastModified: pastedAt })];
  });
}
