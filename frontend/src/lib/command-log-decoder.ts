type LogPage = { data: string; offset: number; next_offset: number; has_more: boolean };

// Raw byte pages may divide a UTF-8 character. Preserve the trailing bytes when
// continuing; seeking or restarting creates a fresh decoder instead.
export class CommandLogDecoder {
  private decoder = new TextDecoder();
  private next = 0;
  decode(page: LogPage, terminal: boolean) {
    if (page.offset === 0 || page.offset !== this.next) this.decoder = new TextDecoder();
    const bytes = Uint8Array.from(atob(page.data || ""), char => char.charCodeAt(0));
    const text = this.decoder.decode(bytes, { stream: page.has_more || !terminal });
    this.next = page.next_offset;
    return text;
  }
}
