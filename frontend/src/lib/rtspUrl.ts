/**
 * Client-side validation mirrors the backend's, so most mistakes are caught
 * before a WebSocket is even opened. The backend validates again.
 */
export function validateRtspUrl(input: string): { url: string } | { error: string } {
  const value = input.trim();
  if (!value) return { error: 'Enter an RTSP URL.' };
  if (/\s/.test(value)) return { error: 'The URL must not contain spaces.' };
  if (!/^rtsps?:\/\//i.test(value)) return { error: 'The URL must start with rtsp:// or rtsps://' };

  // URL() does not know rtsp, but parses it as a generic hierarchical URL.
  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch {
    return { error: 'This does not look like a valid URL.' };
  }
  if (!parsed.hostname) return { error: 'The URL has no host.' };
  return { url: value };
}

/** A readable default name: the last path segment, or the host. */
export function defaultStreamName(url: string): string {
  try {
    const u = new URL(url);
    const last = u.pathname.split('/').filter(Boolean).pop();
    return last ? decodeURIComponent(last) : u.hostname;
  } catch {
    return 'Stream';
  }
}

/** Hide credentials when showing a URL in the UI. */
export function displayUrl(url: string): string {
  try {
    const u = new URL(url);
    if (u.password) u.password = 'xxxxx';
    return u.toString();
  } catch {
    return url;
  }
}
