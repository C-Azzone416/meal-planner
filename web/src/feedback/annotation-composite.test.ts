// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  MAX_COMPOSITE_PIXELS,
  MAX_COMPOSITE_SIDE,
  browserCompositeDeps,
  compositeMarks,
  type CompositeDeps,
  type DrawingSurface,
} from "./annotation-composite";
import { EDGE_HALO, EDGE_OUTLINE, INK_COLORS, type Stroke } from "./annotation-model";

/** A surface that records every call, so a test can read what was drawn, in what colour and where. */
function recordingSurface() {
  const calls: string[] = [];
  const styles: { stroke: string; fill: string; width: number }[] = [];
  const surface: DrawingSurface = {
    lineCap: "",
    lineJoin: "",
    lineWidth: 0,
    strokeStyle: "",
    fillStyle: "",
    save: () => calls.push("save"),
    restore: () => calls.push("restore"),
    beginPath: () => calls.push("begin"),
    moveTo: (x, y) => calls.push(`move ${x},${y}`),
    lineTo: (x, y) => calls.push(`line ${x},${y}`),
    quadraticCurveTo: (cx, cy, x, y) => calls.push(`curve ${cx},${cy} ${x},${y}`),
    arc: (x, y, r) => calls.push(`arc ${x},${y} r${r}`),
    stroke: () => {
      calls.push("stroke");
      styles.push({ stroke: surface.strokeStyle, fill: "", width: surface.lineWidth });
    },
    fill: () => {
      calls.push("fill");
      styles.push({ stroke: "", fill: surface.fillStyle, width: surface.lineWidth });
    },
    drawImage: (_image, x, y) => calls.push(`image ${x},${y}`),
  };
  return { surface, calls, styles };
}

function fakeDeps(
  options: {
    width?: number;
    height?: number;
    decoded?: boolean;
    surface?: DrawingSurface | null;
    canvas?: boolean;
    png?: Blob | null;
    onClose?: () => void;
  } = {},
) {
  const log = recordingSurface();
  const created: { width: number; height: number }[] = [];
  const deps: CompositeDeps = {
    decode: async () =>
      options.decoded === false
        ? null
        : {
            image: "the-image",
            width: options.width ?? 800,
            height: options.height ?? 1600,
            close: options.onClose ?? (() => undefined),
          },
    createCanvas: (width, height) => {
      created.push({ width, height });
      if (options.canvas === false) return null;
      return {
        context: () => (options.surface === undefined ? log.surface : options.surface),
        toPng: async () =>
          options.png === undefined ? new Blob(["composed"], { type: "image/png" }) : options.png,
      };
    },
  };
  return { deps, created, ...log };
}

const stroke = (overrides: Partial<Stroke> = {}): Stroke => ({
  color: "red",
  width: 6,
  unit: 1,
  points: [
    { x: 10, y: 10 },
    { x: 40, y: 10 },
    { x: 70, y: 40 },
  ],
  ...overrides,
});

const source = new Blob(["png"], { type: "image/png" });

describe("compositing the marks onto the reviewed screenshot", () => {
  it("draws the screenshot at its own natural size first, then the marks, and returns a PNG", async () => {
    const fake = fakeDeps({ width: 800, height: 1600 });
    const result = await compositeMarks(source, [stroke()], fake.deps);
    expect(result.kind).toBe("ok");
    expect(result.kind === "ok" && result.blob.type).toBe("image/png");
    expect(fake.created).toEqual([{ width: 800, height: 1600 }]);
    expect(fake.calls[0]).toBe("image 0,0");
    expect(fake.calls).toContain("stroke");
  });

  it("draws each stroke in three passes over the same path: outline, white halo, then the colour", async () => {
    const fake = fakeDeps();
    await compositeMarks(source, [stroke({ width: 6, unit: 1 })], fake.deps);
    // Rings are 1 and 1.5 CSS px wide on each side: the outline pass is 6 + 2 x 2.5 = 11, the halo 6 + 2 x 1.5 = 9, the core 6.
    expect(fake.styles).toEqual([
      { stroke: EDGE_OUTLINE, fill: "", width: 11 },
      { stroke: EDGE_HALO, fill: "", width: 9 },
      { stroke: INK_COLORS[1].value, fill: "", width: 6 },
    ]);
  });

  it("scales the rings with the picture: a stroke drawn at 2 natural px per CSS px has rings twice as wide", async () => {
    const fake = fakeDeps();
    await compositeMarks(source, [stroke({ width: 8, unit: 2 })], fake.deps);
    expect(fake.styles.map((style) => style.width)).toEqual([18, 14, 8]);
  });

  it("smooths a stroke through its midpoints and ends on the last point, the same path in every pass", async () => {
    const fake = fakeDeps();
    await compositeMarks(source, [stroke()], fake.deps);
    const pass = ["begin", "move 10,10", "curve 40,10 55,25", "line 70,40", "stroke"];
    expect(fake.calls).toEqual(["image 0,0", "save", ...pass, ...pass, ...pass, "restore"]);
  });

  it("draws a two-point stroke as a straight line", async () => {
    const fake = fakeDeps();
    await compositeMarks(
      source,
      [
        stroke({
          points: [
            { x: 1, y: 2 },
            { x: 30, y: 40 },
          ],
        }),
      ],
      fake.deps,
    );
    const pass = ["begin", "move 1,2", "line 30,40", "stroke"];
    expect(fake.calls).toEqual(["image 0,0", "save", ...pass, ...pass, ...pass, "restore"]);
  });

  it("draws a tap as dots: edge rings, then the colour, each as wide as its line", async () => {
    const fake = fakeDeps();
    await compositeMarks(
      source,
      [stroke({ width: 10, unit: 1, points: [{ x: 5, y: 6 }] })],
      fake.deps,
    );
    expect(fake.calls.filter((call) => call.startsWith("arc"))).toEqual([
      "arc 5,6 r7.5",
      "arc 5,6 r6.5",
      "arc 5,6 r5",
    ]);
    expect(fake.calls).not.toContain("stroke");
    expect(fake.styles.map((style) => style.fill)).toEqual([
      EDGE_OUTLINE,
      EDGE_HALO,
      INK_COLORS[1].value,
    ]);
  });

  it("uses each stroke's own colour", async () => {
    const fake = fakeDeps();
    await compositeMarks(
      source,
      [stroke({ color: "blue", width: 8 }), stroke({ color: "yellow", width: 3 })],
      fake.deps,
    );
    const cores = fake.styles.filter((_style, index) => index % 3 === 2);
    expect(cores).toEqual([
      { stroke: INK_COLORS[2].value, fill: "", width: 8 },
      { stroke: INK_COLORS[3].value, fill: "", width: 3 },
    ]);
  });

  it("skips a stroke with no points and still succeeds", async () => {
    const fake = fakeDeps();
    const result = await compositeMarks(source, [stroke({ points: [] })], fake.deps);
    expect(result.kind).toBe("ok");
    expect(fake.calls).toEqual(["image 0,0"]);
  });

  it("releases the decoded picture whatever happens", async () => {
    const closed = vi.fn();
    await compositeMarks(source, [stroke()], fakeDeps({ onClose: closed }).deps);
    await compositeMarks(source, [stroke()], fakeDeps({ onClose: closed, png: null }).deps);
    await compositeMarks(source, [stroke()], fakeDeps({ onClose: closed, canvas: false }).deps);
    expect(closed).toHaveBeenCalledTimes(3);
  });

  it("fails, never resizing, when the picture cannot be read", async () => {
    expect((await compositeMarks(source, [stroke()], fakeDeps({ decoded: false }).deps)).kind).toBe(
      "failed",
    );
    const throwing: CompositeDeps = {
      decode: async () => {
        throw new Error("not an image");
      },
      createCanvas: () => null,
    };
    expect((await compositeMarks(source, [stroke()], throwing)).kind).toBe("failed");
  });

  it("fails when there is no canvas, no 2d context, or no PNG came out", async () => {
    expect((await compositeMarks(source, [stroke()], fakeDeps({ canvas: false }).deps)).kind).toBe(
      "failed",
    );
    expect((await compositeMarks(source, [stroke()], fakeDeps({ surface: null }).deps)).kind).toBe(
      "failed",
    );
    expect((await compositeMarks(source, [stroke()], fakeDeps({ png: null }).deps)).kind).toBe(
      "failed",
    );
    expect(
      (await compositeMarks(source, [stroke()], fakeDeps({ png: new Blob([]) }).deps)).kind,
    ).toBe("failed");
  });

  it("fails, rather than scaling down, for a picture too large to rely on a canvas for", async () => {
    for (const [width, height] of [
      [MAX_COMPOSITE_SIDE + 1, 100],
      [100, MAX_COMPOSITE_SIDE + 1],
      [MAX_COMPOSITE_SIDE, Math.ceil(MAX_COMPOSITE_PIXELS / MAX_COMPOSITE_SIDE) + 1],
      [0, 100],
      [100.5, 100],
    ] as const) {
      const fake = fakeDeps({ width, height });
      expect((await compositeMarks(source, [stroke()], fake.deps)).kind).toBe("failed");
      expect(fake.created).toEqual([]);
    }
  });
});

describe("the browser's own decoding and canvas", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("decodes with createImageBitmap and closes the bitmap", async () => {
    const close = vi.fn();
    vi.stubGlobal("createImageBitmap", async () => ({ width: 30, height: 40, close }));
    const decoded = await browserCompositeDeps.decode(source);
    expect(decoded).toMatchObject({ width: 30, height: 40 });
    decoded?.close();
    expect(close).toHaveBeenCalled();
  });

  it("makes a canvas of the picture's size, with a 2d context and PNG output", async () => {
    const surface = recordingSurface().surface;
    vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(
      surface as unknown as CanvasRenderingContext2D,
    );
    const toBlob = vi
      .spyOn(HTMLCanvasElement.prototype, "toBlob")
      .mockImplementation((callback) => callback(new Blob(["x"], { type: "image/png" })));
    const canvas = browserCompositeDeps.createCanvas(30, 40);
    expect(canvas?.context()).toBe(surface);
    expect((await canvas?.toPng())?.type).toBe("image/png");
    expect(toBlob.mock.calls[0][1]).toBe("image/png");
  });
});
