// The one place that says how big a screenshot may be and how that is judged (#260, #266).

/**
 * The largest screenshot offered. A report goes through the app's own route, and the host rejects a request body over about
 * 4.5 MB before the app sees it (#259), while the SDK itself allows 8 MiB. A screenshot may be up to and including this many bytes
 * (see `fitsSendLimit`); a bigger one is never shrunk, cropped or swapped, and the notes still send.
 */
export const SCREENSHOT_SEND_LIMIT_BYTES = 4_000_000;

/** True when an image of this many bytes may go to the route and the outbox: no larger than the limit (exactly 4,000,000 is fine, 4,000,001 is not). */
export function fitsSendLimit(bytes: number): boolean {
  return Number.isFinite(bytes) && bytes >= 0 && bytes <= SCREENSHOT_SEND_LIMIT_BYTES;
}
