// Drawing on a screenshot (#266): the marks as plain data, and the pure steps that change them. Nothing here touches the DOM, a
// canvas or storage, so it is the same on a phone and a desktop. Every coordinate is in the screenshot's NATURAL pixels, so the
// marks mean the same thing at any preview size; the preview only converts a pointer position into one with `toNaturalPoint`.

export interface Point {
  x: number;
  y: number;
}

export interface Size {
  width: number;
  height: number;
}

export interface Rect extends Size {
  left: number;
  top: number;
}

/**
 * Design's four inks (#266 spec section 2): every pair differs by at least 1.69:1 in luminance, so they stay apart in greyscale and
 * for most colour-blind people, and each has a name in the picker. Order is the picker's order.
 */
export const INK_COLORS = [
  { id: "black", label: "Black", value: "#111111" },
  { id: "red", label: "Red", value: "#d92d20" },
  { id: "blue", label: "Blue", value: "#3d9bff" },
  { id: "yellow", label: "Yellow", value: "#ffd21f" },
] as const;

/** The edge drawn under every stroke so it shows on light and dark pictures: a near-black line, then a white halo, then the colour. */
export const EDGE_OUTLINE = "#111111";
export const EDGE_HALO = "#ffffff";
/** Widths of the two rings, in CSS pixels (scaled to natural pixels by the stroke's `unit`). */
export const OUTLINE_CSS_PX = 1;
export const HALO_CSS_PX = 1.5;
/** A stroke is this wide as the person sees it, in CSS pixels. */
export const STROKE_CSS_PX = 4;
export const MIN_STROKE_NATURAL_PX = 3;

export type InkColorId = (typeof INK_COLORS)[number]["id"];

export const DEFAULT_INK: InkColorId = "red";

export interface Stroke {
  color: InkColorId;
  /** The coloured line's width in natural pixels. */
  width: number;
  /** Natural pixels per CSS pixel when it was drawn: the two edge rings are this much wider per CSS pixel. */
  unit: number;
  points: readonly Point[];
}

export interface Drawing {
  /** Finished strokes, oldest first. */
  strokes: readonly Stroke[];
  /** The stroke under the pointer right now, if any. */
  active: Stroke | null;
  /** What Clear drawing took away, so Undo can bring it all back as one step. A new stroke makes it unrecoverable. */
  cleared: readonly Stroke[] | null;
}

export const EMPTY_DRAWING: Drawing = { strokes: [], active: null, cleared: null };

/** Bounds, so a long scribble cannot grow without end: past them further input is ignored, not queued. */
export const MAX_STROKES = 100;
export const MAX_POINTS_PER_STROKE = 1500;
/** A point closer than this (natural pixels) to the last one adds nothing and is dropped. */
export const MIN_POINT_DISTANCE = 1.5;

const finite = (value: number) => Number.isFinite(value);

export function inkValue(id: InkColorId): string {
  return INK_COLORS.find((color) => color.id === id)?.value ?? INK_COLORS[0].value;
}

/** The scale at which the whole picture is shown in `rect` (contain): displayed pixels per natural pixel. Null if unusable. */
export function fitScale(rect: Size, natural: Size): number | null {
  const sizes = [rect.width, rect.height, natural.width, natural.height];
  if (!sizes.every((value) => finite(value) && value > 0)) return null;
  return Math.min(rect.width / natural.width, rect.height / natural.height);
}

/**
 * A new stroke's ink at the size the person sees: 4 CSS pixels wide on screen, so the saved PNG shows the marks at the same weight
 * they were drawn at, at every preview size (never under 3 natural pixels). `scale` is `fitScale` of the preview.
 */
export function inkFor(
  color: InkColorId,
  scale: number,
): { color: InkColorId; width: number; unit: number } {
  const unit = finite(scale) && scale > 0 ? 1 / scale : 1;
  return { color, width: Math.max(MIN_STROKE_NATURAL_PX, STROKE_CSS_PX * unit), unit };
}

/**
 * Converts a pointer position (client coordinates) to a point in the screenshot's natural pixels. The preview shows the whole
 * picture scaled uniformly and centred in `rect` (like `object-fit: contain`), so any letterboxing is accounted for. The point is
 * clamped onto the picture; `inside` says whether the pointer was really over it. Null when either size is unusable.
 */
export function toNaturalPoint(
  client: Point,
  rect: Rect,
  natural: Size,
): { point: Point; inside: boolean } | null {
  const scale = fitScale(rect, natural);
  if (scale === null) return null;
  if (![client.x, client.y, rect.left, rect.top].every(finite)) return null;
  const left = rect.left + (rect.width - natural.width * scale) / 2;
  const top = rect.top + (rect.height - natural.height * scale) / 2;
  const x = (client.x - left) / scale;
  const y = (client.y - top) / scale;
  const inside = x >= 0 && x <= natural.width && y >= 0 && y <= natural.height;
  const clamp = (value: number, max: number) => Math.min(Math.max(value, 0), max);
  const tenth = (value: number) => Math.round(value * 10) / 10;
  return {
    point: { x: tenth(clamp(x, natural.width)), y: tenth(clamp(y, natural.height)) },
    inside,
  };
}

export const hasMarks = (drawing: Drawing): boolean =>
  drawing.strokes.length > 0 || drawing.active !== null;

/** Undo has something to do: a mark to take back, or a cleared drawing to bring back. */
export const canUndo = (drawing: Drawing): boolean => hasMarks(drawing) || drawing.cleared !== null;

/** Every stroke to draw, the one still being drawn included. */
export const allStrokes = (drawing: Drawing): readonly Stroke[] =>
  drawing.active ? [...drawing.strokes, drawing.active] : drawing.strokes;

export const strokeCount = (drawing: Drawing): number => allStrokes(drawing).length;

/** Starts a stroke at a point. A stroke still open is finished first. Ignored at the stroke limit or for a bad point. */
export function beginStroke(
  drawing: Drawing,
  ink: { color: InkColorId; width: number; unit: number },
  point: Point,
): Drawing {
  if (!finite(point.x) || !finite(point.y) || !(ink.width > 0) || !(ink.unit > 0)) return drawing;
  const settled = endStroke(drawing);
  if (settled.strokes.length >= MAX_STROKES) return settled;
  // A new stroke after a Clear makes the cleared drawing unrecoverable.
  return {
    ...settled,
    cleared: null,
    active: { color: ink.color, width: ink.width, unit: ink.unit, points: [point] },
  };
}

/** Adds a point to the stroke under the pointer, unless it is a near-duplicate or the stroke is at its limit. */
export function extendStroke(drawing: Drawing, point: Point): Drawing {
  const { active } = drawing;
  if (!active || !finite(point.x) || !finite(point.y)) return drawing;
  if (active.points.length >= MAX_POINTS_PER_STROKE) return drawing;
  const last = active.points[active.points.length - 1];
  if (Math.hypot(point.x - last.x, point.y - last.y) < MIN_POINT_DISTANCE) return drawing;
  return { ...drawing, active: { ...active, points: [...active.points, point] } };
}

/** Lifts the pointer: the stroke under it (a single tap is a dot) becomes a finished stroke. */
export function endStroke(drawing: Drawing): Drawing {
  return drawing.active
    ? { ...drawing, strokes: [...drawing.strokes, drawing.active], active: null }
    : drawing;
}

/** The pointer was interrupted (cancelled gesture): the open stroke is dropped, not kept half-drawn. */
export function cancelStroke(drawing: Drawing): Drawing {
  return drawing.active ? { ...drawing, active: null } : drawing;
}

/**
 * Undo: takes back the stroke being drawn, or else the last finished one, or else (right after Clear drawing) the whole cleared
 * drawing in one step.
 */
export function undo(drawing: Drawing): Drawing {
  if (drawing.active) return { ...drawing, active: null };
  if (drawing.strokes.length > 0) return { ...drawing, strokes: drawing.strokes.slice(0, -1) };
  if (drawing.cleared) return { strokes: drawing.cleared, active: null, cleared: null };
  return drawing;
}

/** Clear drawing: every mark removed, kept only so that Undo can bring it all back. */
export function clearDrawing(drawing: Drawing): Drawing {
  return hasMarks(drawing) ? { strokes: [], active: null, cleared: allStrokes(drawing) } : drawing;
}
