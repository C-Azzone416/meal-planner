import { describe, expect, it } from "vitest";
import {
  EMPTY_DRAWING,
  INK_COLORS,
  MAX_POINTS_PER_STROKE,
  MAX_STROKES,
  MIN_POINT_DISTANCE,
  allStrokes,
  beginStroke,
  cancelStroke,
  clearDrawing,
  canUndo,
  fitScale,
  inkFor,
  STROKE_CSS_PX,
  MIN_STROKE_NATURAL_PX,
  EDGE_HALO,
  EDGE_OUTLINE,
  endStroke,
  extendStroke,
  hasMarks,
  inkValue,
  strokeCount,
  toNaturalPoint,
  undo,
  type Drawing,
  type Rect,
  type Size,
} from "./annotation-model";

const ink = { color: "red", width: 6, unit: 1 } as const;

function stroked(...paths: { x: number; y: number }[][]): Drawing {
  let drawing: Drawing = EMPTY_DRAWING;
  for (const path of paths) {
    drawing = beginStroke(drawing, ink, path[0]);
    for (const point of path.slice(1)) drawing = extendStroke(drawing, point);
    drawing = endStroke(drawing);
  }
  return drawing;
}

describe("the four inks", () => {
  it("are four, with distinct names and distinct colours", () => {
    expect(INK_COLORS.map((color) => color.label)).toEqual(["Black", "Red", "Blue", "Yellow"]);
    expect(INK_COLORS).toHaveLength(4);
    expect(new Set(INK_COLORS.map((color) => color.id)).size).toBe(4);
    expect(new Set(INK_COLORS.map((color) => color.value)).size).toBe(4);
    for (const color of INK_COLORS) expect(color.value).toMatch(/^#[0-9a-f]{6}$/);
  });

  it("each can be looked up, and an unknown one falls back to the first", () => {
    expect(inkValue("blue")).toBe(INK_COLORS[2].value);
    expect(inkValue("nope" as never)).toBe(INK_COLORS[0].value);
  });
});

describe("the line width", () => {
  it("is 4 CSS pixels as shown, so the saved picture has the weight that was drawn", () => {
    // A 1280-wide screenshot shown at 640 wide: each CSS pixel is 2 natural pixels, so 4 CSS px is 8 natural px.
    expect(inkFor("red", 0.5)).toEqual({ color: "red", width: 8, unit: 2 });
    expect(inkFor("blue", 1)).toEqual({ color: "blue", width: STROKE_CSS_PX, unit: 1 });
  });

  it("is never under 3 natural pixels, even when the picture is shown larger than it is", () => {
    expect(inkFor("red", 4).width).toBe(MIN_STROKE_NATURAL_PX);
    expect(inkFor("red", 4).unit).toBe(0.25);
  });

  it("falls back to one natural pixel per CSS pixel for a scale that is not usable", () => {
    for (const scale of [0, -1, Number.NaN, Number.POSITIVE_INFINITY * 0]) {
      expect(inkFor("red", scale).unit).toBe(1);
    }
  });

  it("uses the same scale the pointer mapping does (contain)", () => {
    expect(fitScale({ width: 600, height: 800 }, { width: 800, height: 1600 })).toBe(0.5);
    expect(fitScale({ width: 400, height: 1000 }, { width: 800, height: 1600 })).toBe(0.5);
    expect(fitScale({ width: 0, height: 10 }, { width: 800, height: 1600 })).toBeNull();
    expect(fitScale({ width: 10, height: 10 }, { width: 8, height: Number.NaN })).toBeNull();
  });

  it("has an edge of near-black and white under every stroke", () => {
    expect(EDGE_OUTLINE).toBe("#111111");
    expect(EDGE_HALO).toBe("#ffffff");
  });
});

describe("pointer position to the screenshot's natural pixels", () => {
  const natural: Size = { width: 800, height: 1600 };

  it("maps a preview shown at half size (no letterboxing)", () => {
    const rect: Rect = { left: 100, top: 50, width: 400, height: 800 };
    expect(toNaturalPoint({ x: 100, y: 50 }, rect, natural)).toEqual({
      point: { x: 0, y: 0 },
      inside: true,
    });
    expect(toNaturalPoint({ x: 300, y: 450 }, rect, natural)).toEqual({
      point: { x: 400, y: 800 },
      inside: true,
    });
    expect(toNaturalPoint({ x: 500, y: 850 }, rect, natural)).toEqual({
      point: { x: 800, y: 1600 },
      inside: true,
    });
  });

  it("maps a preview shown larger than the picture", () => {
    const rect: Rect = { left: 0, top: 0, width: 1600, height: 3200 };
    expect(toNaturalPoint({ x: 800, y: 1600 }, rect, natural)?.point).toEqual({ x: 400, y: 800 });
  });

  it("accounts for the bars when the preview box is wider than the picture (contain)", () => {
    // The box is 600x800 but the picture, scaled to fit, is 400x800, centred: 100px bars left and right.
    const rect: Rect = { left: 10, top: 20, width: 600, height: 800 };
    expect(toNaturalPoint({ x: 110, y: 20 }, rect, natural)).toEqual({
      point: { x: 0, y: 0 },
      inside: true,
    });
    expect(toNaturalPoint({ x: 310, y: 420 }, rect, natural)?.point).toEqual({ x: 400, y: 800 });
    const bar = toNaturalPoint({ x: 50, y: 420 }, rect, natural);
    expect(bar?.inside).toBe(false);
    expect(bar?.point.x).toBe(0);
  });

  it("accounts for the bars when the preview box is taller than the picture", () => {
    const rect: Rect = { left: 0, top: 0, width: 400, height: 1000 };
    // Fits to width 400, so the picture is 400x800, centred with 100px above and below.
    expect(toNaturalPoint({ x: 0, y: 100 }, rect, natural)).toEqual({
      point: { x: 0, y: 0 },
      inside: true,
    });
    expect(toNaturalPoint({ x: 0, y: 50 }, rect, natural)?.inside).toBe(false);
    expect(toNaturalPoint({ x: 0, y: 50 }, rect, natural)?.point.y).toBe(0);
  });

  it("clamps a pointer past the edge onto the picture and says it was outside", () => {
    const rect: Rect = { left: 0, top: 0, width: 400, height: 800 };
    expect(toNaturalPoint({ x: 9999, y: -50 }, rect, natural)).toEqual({
      point: { x: 800, y: 0 },
      inside: false,
    });
  });

  it("gives a point at one decimal place", () => {
    const rect: Rect = { left: 0, top: 0, width: 300, height: 600 };
    const point = toNaturalPoint({ x: 100, y: 100 }, rect, natural)?.point;
    expect(point).toEqual({ x: 266.7, y: 266.7 });
  });

  it("is the same picture point at a phone-sized and a desktop-sized preview", () => {
    const phone: Rect = { left: 5, top: 5, width: 300, height: 600 };
    const desktop: Rect = { left: 40, top: 80, width: 500, height: 1000 };
    const at = (rect: Rect, fx: number, fy: number) =>
      toNaturalPoint(
        { x: rect.left + rect.width * fx, y: rect.top + rect.height * fy },
        rect,
        natural,
      )?.point;
    expect(at(phone, 0.25, 0.75)).toEqual(at(desktop, 0.25, 0.75));
    expect(at(phone, 0.25, 0.75)).toEqual({ x: 200, y: 1200 });
  });

  it("is null for a preview or picture with no usable size, or a pointer that is not a number", () => {
    const rect: Rect = { left: 0, top: 0, width: 400, height: 800 };
    expect(toNaturalPoint({ x: 1, y: 1 }, { ...rect, width: 0 }, natural)).toBeNull();
    expect(toNaturalPoint({ x: 1, y: 1 }, { ...rect, height: -3 }, natural)).toBeNull();
    expect(toNaturalPoint({ x: 1, y: 1 }, rect, { width: 0, height: 10 })).toBeNull();
    expect(toNaturalPoint({ x: 1, y: 1 }, rect, { width: 10, height: Number.NaN })).toBeNull();
    expect(toNaturalPoint({ x: Number.NaN, y: 1 }, rect, natural)).toBeNull();
    expect(toNaturalPoint({ x: 1, y: 1 }, { ...rect, left: Infinity }, natural)).toBeNull();
  });
});

describe("drawing strokes", () => {
  it("starts empty", () => {
    expect(hasMarks(EMPTY_DRAWING)).toBe(false);
    expect(strokeCount(EMPTY_DRAWING)).toBe(0);
    expect(allStrokes(EMPTY_DRAWING)).toEqual([]);
  });

  it("builds a stroke from down, move, up and keeps its colour and width", () => {
    let drawing = beginStroke(
      EMPTY_DRAWING,
      { color: "blue", width: 9, unit: 2 },
      { x: 10, y: 10 },
    );
    expect(drawing.active?.points).toEqual([{ x: 10, y: 10 }]);
    expect(hasMarks(drawing)).toBe(true);
    drawing = extendStroke(drawing, { x: 30, y: 10 });
    drawing = endStroke(drawing);
    expect(drawing.active).toBeNull();
    expect(drawing.strokes).toEqual([
      {
        color: "blue",
        width: 9,
        unit: 2,
        points: [
          { x: 10, y: 10 },
          { x: 30, y: 10 },
        ],
      },
    ]);
  });

  it("keeps a single tap as a one-point stroke (drawn as a dot)", () => {
    const drawing = stroked([{ x: 5, y: 5 }]);
    expect(drawing.strokes).toHaveLength(1);
    expect(drawing.strokes[0].points).toHaveLength(1);
  });

  it("includes the stroke still being drawn in what gets drawn", () => {
    const drawing = beginStroke(stroked([{ x: 1, y: 1 }]), ink, { x: 50, y: 50 });
    expect(strokeCount(drawing)).toBe(2);
    expect(allStrokes(drawing)).toHaveLength(2);
  });

  it("drops a point too close to the last one", () => {
    let drawing = beginStroke(EMPTY_DRAWING, ink, { x: 10, y: 10 });
    const near = extendStroke(drawing, { x: 10 + MIN_POINT_DISTANCE / 2, y: 10 });
    expect(near).toBe(drawing);
    drawing = extendStroke(drawing, { x: 10 + MIN_POINT_DISTANCE, y: 10 });
    expect(drawing.active?.points).toHaveLength(2);
  });

  it("finishes an open stroke when another begins", () => {
    let drawing = beginStroke(EMPTY_DRAWING, ink, { x: 1, y: 1 });
    drawing = beginStroke(drawing, { color: "yellow", width: 4, unit: 1 }, { x: 40, y: 40 });
    expect(drawing.strokes).toHaveLength(1);
    expect(drawing.active?.color).toBe("yellow");
  });

  it("ignores a point that is not a number, a bad width, and a move or up with nothing open", () => {
    expect(beginStroke(EMPTY_DRAWING, ink, { x: Number.NaN, y: 1 })).toBe(EMPTY_DRAWING);
    expect(beginStroke(EMPTY_DRAWING, { color: "red", width: 0, unit: 1 }, { x: 1, y: 1 })).toBe(
      EMPTY_DRAWING,
    );
    expect(beginStroke(EMPTY_DRAWING, { color: "red", width: 4, unit: 0 }, { x: 1, y: 1 })).toBe(
      EMPTY_DRAWING,
    );
    expect(extendStroke(EMPTY_DRAWING, { x: 1, y: 1 })).toBe(EMPTY_DRAWING);
    expect(endStroke(EMPTY_DRAWING)).toBe(EMPTY_DRAWING);
    const open = beginStroke(EMPTY_DRAWING, ink, { x: 1, y: 1 });
    expect(extendStroke(open, { x: Number.NaN, y: 1 })).toBe(open);
  });

  it("stops taking points at the per-stroke limit and strokes at the stroke limit", () => {
    let drawing = beginStroke(EMPTY_DRAWING, ink, { x: 0, y: 0 });
    for (let index = 1; index < MAX_POINTS_PER_STROKE + 50; index += 1) {
      drawing = extendStroke(drawing, { x: index * 2, y: 0 });
    }
    expect(drawing.active?.points).toHaveLength(MAX_POINTS_PER_STROKE);

    let many: Drawing = EMPTY_DRAWING;
    for (let index = 0; index < MAX_STROKES + 5; index += 1) {
      many = endStroke(beginStroke(many, ink, { x: index, y: index }));
    }
    expect(many.strokes).toHaveLength(MAX_STROKES);
    expect(beginStroke(many, ink, { x: 1, y: 1 })).toEqual(many);
  });

  it("does not change the drawing it was given", () => {
    const before = stroked([
      { x: 0, y: 0 },
      { x: 20, y: 20 },
    ]);
    const snapshot = JSON.stringify(before);
    extendStroke(beginStroke(before, ink, { x: 5, y: 5 }), { x: 50, y: 50 });
    undo(before);
    clearDrawing(before);
    expect(JSON.stringify(before)).toBe(snapshot);
  });
});

describe("Undo, Clear and cancel", () => {
  const path = (offset: number) => [
    { x: offset, y: offset },
    { x: offset + 20, y: offset + 20 },
  ];

  it("Undo takes back the last finished stroke, one at a time", () => {
    let drawing = stroked(path(0), path(100), path(200));
    drawing = undo(drawing);
    expect(drawing.strokes).toHaveLength(2);
    expect(drawing.strokes[1].points[0]).toEqual({ x: 100, y: 100 });
    drawing = undo(undo(drawing));
    expect(drawing.strokes).toHaveLength(0);
    expect(undo(drawing)).toBe(drawing);
  });

  it("Undo during a stroke takes back that stroke and keeps the earlier ones", () => {
    const drawing = beginStroke(stroked(path(0)), ink, { x: 90, y: 90 });
    const undone = undo(drawing);
    expect(undone.active).toBeNull();
    expect(undone.strokes).toHaveLength(1);
  });

  it("Clear drawing removes every mark, including one being drawn", () => {
    const drawing = beginStroke(stroked(path(0), path(100)), ink, { x: 1, y: 1 });
    const cleared = clearDrawing(drawing);
    expect(cleared.strokes).toEqual([]);
    expect(cleared.active).toBeNull();
    expect(hasMarks(cleared)).toBe(false);
    expect(clearDrawing(EMPTY_DRAWING)).toBe(EMPTY_DRAWING);
    expect(clearDrawing(cleared)).toBe(cleared);
  });

  it("Undo right after Clear brings the whole cleared drawing back in one step", () => {
    const drawing = stroked(path(0), path(100), path(200));
    const cleared = clearDrawing(drawing);
    expect(canUndo(cleared)).toBe(true);
    const restored = undo(cleared);
    expect(restored.strokes).toEqual(drawing.strokes);
    expect(restored.cleared).toBeNull();
    // ... and Undo after that is an ordinary undo of the last mark.
    expect(undo(restored).strokes).toHaveLength(2);
  });

  it("a new stroke after Clear makes the cleared drawing unrecoverable", () => {
    const cleared = clearDrawing(stroked(path(0), path(100)));
    const redrawn = endStroke(beginStroke(cleared, ink, { x: 300, y: 300 }));
    expect(redrawn.cleared).toBeNull();
    expect(redrawn.strokes).toHaveLength(1);
    const undone = undo(redrawn);
    expect(undone.strokes).toHaveLength(0);
    expect(undo(undone)).toBe(undone);
  });

  it("knows when Undo and Clear have something to do", () => {
    expect(canUndo(EMPTY_DRAWING)).toBe(false);
    expect(canUndo(stroked(path(0)))).toBe(true);
    expect(canUndo(clearDrawing(stroked(path(0))))).toBe(true);
    expect(canUndo(undo(clearDrawing(undo(clearDrawing(stroked(path(0)))))))).toBe(true);
    expect(canUndo(undo(stroked(path(0))))).toBe(false);
  });

  it("a cancelled gesture drops the half-drawn stroke only", () => {
    const drawing = beginStroke(stroked(path(0)), ink, { x: 90, y: 90 });
    const cancelled = cancelStroke(drawing);
    expect(cancelled.active).toBeNull();
    expect(cancelled.strokes).toHaveLength(1);
    expect(cancelStroke(cancelled)).toBe(cancelled);
  });
});
