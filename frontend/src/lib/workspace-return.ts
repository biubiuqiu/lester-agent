/** Only workspace reading/composing routes can be settings return targets. */
export function workspaceReturnPath(value: string | null | undefined): string {
  return value && !/[\\\x00-\x20]/.test(value) && /^\/app(?:\/new|\/p\/[A-Za-z0-9_-]+(?:\/overview)?|\/c\/[A-Za-z0-9_-]+)?(?:\?agent=[A-Za-z0-9_-]+)?$/.test(value) ? value : "/app";
}
