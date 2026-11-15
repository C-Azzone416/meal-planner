// What leaves the sheet as the screenshot at Send (#266). One decision, made BEFORE the report reaches the route or the outbox:
// no image at all (text only), the reviewed picture as it was, or the picture with the marks on it. A picture that is not
// no larger than the send limit is reported as too large, and nothing else is substituted for it: not a shrunk copy, not a crop,
// not the picture without its marks.
import { compositeMarks, type CompositeDeps } from "./annotation-composite";
import { allStrokes, type Drawing } from "./annotation-model";
import { fitsSendLimit } from "./screenshot-limit";

export type FinalScreenshot =
  /** Text only: nothing is composited, saved or uploaded. */
  | { kind: "none" }
  | { kind: "ready"; blob: Blob; annotated: boolean }
  /** `annotated`: the size is that of the picture WITH marks (so removing marks may fix it), not of the screenshot alone. */
  | { kind: "too-large"; bytes: number; annotated: boolean }
  | { kind: "failed" };

export async function finalScreenshot(options: {
  /** The tick box: false is text-only feedback, whatever was drawn. */
  include: boolean;
  /** The reviewed screenshot, exactly as taken. */
  original: Blob | null;
  drawing: Drawing;
  deps?: CompositeDeps;
}): Promise<FinalScreenshot> {
  const { include, original, drawing, deps } = options;
  if (!include || !original) return { kind: "none" };
  const strokes = allStrokes(drawing);
  if (strokes.length === 0) {
    return fitsSendLimit(original.size)
      ? { kind: "ready", blob: original, annotated: false }
      : { kind: "too-large", bytes: original.size, annotated: false };
  }
  const composed = await compositeMarks(original, strokes, deps);
  if (composed.kind !== "ok") return { kind: "failed" };
  return fitsSendLimit(composed.blob.size)
    ? { kind: "ready", blob: composed.blob, annotated: true }
    : { kind: "too-large", bytes: composed.blob.size, annotated: true };
}
