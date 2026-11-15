// Puts the reviewed marks onto the reviewed screenshot (#266), at Send and nowhere else. The result is a new PNG at the picture's
// own natural size: never scaled down, cropped or re-encoded smaller to fit a limit (the size check is the caller's, see
// annotation-final.ts). Canvas and image decoding are passed in so this is testable without a browser and has no hidden state.
import {
  EDGE_HALO,
  EDGE_OUTLINE,
  HALO_CSS_PX,
  OUTLINE_CSS_PX,
  inkValue,
  type Size,
  type Stroke,
} from "./annotation-model";

/** A pixel budget past which a canvas cannot be relied on to exist; such a picture is reported as failed, never resized. */
export const MAX_COMPOSITE_PIXELS = 12_000_000;
export const MAX_COMPOSITE_SIDE = 4096;

export interface DrawingSurface {
  save(): void;
  restore(): void;
  beginPath(): void;
  moveTo(x: number, y: number): void;
  lineTo(x: number, y: number): void;
  quadraticCurveTo(cx: number, cy: number, x: number, y: number): void;
  arc(x: number, y: number, radius: number, start: number, end: number): void;
  stroke(): void;
  fill(): void;
  drawImage(image: unknown, x: number, y: number): void;
  lineCap: string;
  lineJoin: string;
  lineWidth: number;
  strokeStyle: string;
  fillStyle: string;
}

export interface CompositeCanvas {
  context(): DrawingSurface | null;
  toPng(): Promise<Blob | null>;
}

export interface DecodedImage extends Size {
  image: unknown;
  close(): void;
}

export interface CompositeDeps {
  decode(blob: Blob): Promise<DecodedImage | null>;
  createCanvas(width: number, height: number): CompositeCanvas | null;
}

export type CompositeResult = { kind: "ok"; blob: Blob } | { kind: "failed" };

/** One pass of a stroke's path at one width and colour. A tap is a dot, a longer stroke a curve through its midpoints. */
function pass(surface: DrawingSurface, stroke: Stroke, color: string, width: number) {
  const [first, ...rest] = stroke.points;
  surface.strokeStyle = color;
  surface.fillStyle = color;
  surface.lineWidth = width;
  surface.beginPath();
  if (rest.length === 0) {
    surface.arc(first.x, first.y, width / 2, 0, Math.PI * 2);
    surface.fill();
    return;
  }
  surface.moveTo(first.x, first.y);
  for (let index = 0; index < rest.length - 1; index += 1) {
    const point = rest[index];
    const next = rest[index + 1];
    surface.quadraticCurveTo(point.x, point.y, (point.x + next.x) / 2, (point.y + next.y) / 2);
  }
  const last = rest[rest.length - 1];
  surface.lineTo(last.x, last.y);
  surface.stroke();
}

function drawStroke(surface: DrawingSurface, stroke: Stroke) {
  if (stroke.points.length === 0) return;
  surface.save();
  surface.lineCap = "round";
  surface.lineJoin = "round";
  // Two rings under the colour (design's #266 spec), so the mark shows on a light, a dark and a mid-grey picture alike: a
  // near-black line outside, then a white halo, then the coloured core. Each ring is a fixed number of CSS pixels wide.
  const ring = (css: number) => 2 * css * stroke.unit;
  pass(surface, stroke, EDGE_OUTLINE, stroke.width + ring(HALO_CSS_PX + OUTLINE_CSS_PX));
  pass(surface, stroke, EDGE_HALO, stroke.width + ring(HALO_CSS_PX));
  pass(surface, stroke, inkValue(stroke.color), stroke.width);
  surface.restore();
}

/** Draws the strokes onto a surface already showing the picture. The sheet's live view and the saved PNG both use this. */
export function paintStrokes(surface: DrawingSurface, strokes: readonly Stroke[]) {
  for (const stroke of strokes) drawStroke(surface, stroke);
}

/** The reviewed screenshot with the strokes drawn on it, as a PNG at the picture's natural size. */
export async function compositeMarks(
  source: Blob,
  strokes: readonly Stroke[],
  deps: CompositeDeps = browserCompositeDeps,
): Promise<CompositeResult> {
  let decoded: DecodedImage | null = null;
  try {
    decoded = await deps.decode(source);
    if (!decoded) return { kind: "failed" };
    const { width, height } = decoded;
    const sane =
      Number.isInteger(width) &&
      Number.isInteger(height) &&
      width > 0 &&
      height > 0 &&
      width <= MAX_COMPOSITE_SIDE &&
      height <= MAX_COMPOSITE_SIDE &&
      width * height <= MAX_COMPOSITE_PIXELS;
    if (!sane) return { kind: "failed" };
    const canvas = deps.createCanvas(width, height);
    const surface = canvas?.context();
    if (!canvas || !surface) return { kind: "failed" };
    surface.drawImage(decoded.image, 0, 0);
    paintStrokes(surface, strokes);
    const blob = await canvas.toPng();
    return blob && blob.size > 0 ? { kind: "ok", blob } : { kind: "failed" };
  } catch {
    return { kind: "failed" };
  } finally {
    decoded?.close();
  }
}

/** The browser's own decoding and canvas. Reached only in the real sheet; tests pass their own. */
export const browserCompositeDeps: CompositeDeps = {
  async decode(blob) {
    const bitmap = await createImageBitmap(blob);
    return {
      image: bitmap,
      width: bitmap.width,
      height: bitmap.height,
      close: () => bitmap.close(),
    };
  },
  createCanvas(width, height) {
    const element = document.createElement("canvas");
    element.width = width;
    element.height = height;
    return {
      context: () => element.getContext("2d") as unknown as DrawingSurface | null,
      toPng: () => new Promise((resolve) => element.toBlob(resolve, "image/png")),
    };
  },
};
