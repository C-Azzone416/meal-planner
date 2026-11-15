// @vitest-environment jsdom
import { useEffect } from "react";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { INK_COLORS, allStrokes, strokeCount, type Drawing } from "./annotation-model";
import { EDITOR_COPY, ScreenshotEditor } from "./ScreenshotEditor";
import { useAnnotationDraft, type AnnotationDraft } from "./use-annotation-draft";

// The preview box is 400x800 at (10, 20); the picture is 800x1600, so it is shown at half size with no letterboxing.
const BOX = { left: 10, top: 20, width: 400, height: 800 };
const NATURAL = { width: 800, height: 1600 };

// One picture for the whole run: the editor reloads (and forgets its size) when it is handed a different Blob.
const PICTURE = new Blob(["png"], { type: "image/png" });

let latest: AnnotationDraft;
const surface = {
  calls: [] as string[],
  clearRect: vi.fn(),
  save: () => surface.calls.push("save"),
  restore: () => undefined,
  beginPath: () => undefined,
  moveTo: () => undefined,
  lineTo: () => undefined,
  quadraticCurveTo: () => undefined,
  arc: () => undefined,
  stroke: () => surface.calls.push("stroke"),
  fill: () => surface.calls.push("fill"),
  drawImage: () => undefined,
  lineCap: "",
  lineJoin: "",
  lineWidth: 0,
  strokeStyle: "",
  fillStyle: "",
};

function Harness({
  disabled = false,
  scope = "account-a",
}: {
  disabled?: boolean;
  scope?: string;
}) {
  const draft = useAnnotationDraft(scope);
  useEffect(() => {
    latest = draft;
  });
  return <ScreenshotEditor blob={PICTURE} draft={draft} disabled={disabled} />;
}

function mount(props: { disabled?: boolean; scope?: string } = {}) {
  const view = render(<Harness {...props} />);
  const image = view.container.querySelector(".shot-image") as HTMLImageElement;
  Object.defineProperty(image, "naturalWidth", { value: NATURAL.width, configurable: true });
  Object.defineProperty(image, "naturalHeight", { value: NATURAL.height, configurable: true });
  fireEvent.load(image);
  const canvas = screen.getByRole("img", { name: EDITOR_COPY.canvasLabel }) as HTMLCanvasElement;
  return { ...view, canvas, image };
}

const at = (x: number, y: number, extra: Record<string, unknown> = {}) => ({
  clientX: BOX.left + x,
  clientY: BOX.top + y,
  pointerId: 1,
  pointerType: "mouse",
  button: 0,
  ...extra,
});

const drawing = (): Drawing => latest.drawing;

beforeEach(() => {
  surface.calls = [];
  surface.clearRect.mockClear();
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(
    surface as unknown as CanvasRenderingContext2D,
  );
  vi.spyOn(HTMLCanvasElement.prototype, "getBoundingClientRect").mockReturnValue({
    ...BOX,
    right: BOX.left + BOX.width,
    bottom: BOX.top + BOX.height,
    x: BOX.left,
    y: BOX.top,
    toJSON: () => ({}),
  });
  if (!URL.createObjectURL) URL.createObjectURL = () => "";
  if (!URL.revokeObjectURL) URL.revokeObjectURL = () => {};
  vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:preview");
  vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => undefined);
});

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

describe("the editor's controls", () => {
  it("starts with four named inks (red chosen), a hint, and Undo and Clear drawing off", () => {
    mount();
    const group = screen.getByRole("radiogroup", { name: EDITOR_COPY.inkGroup });
    const radios = within(group).getAllByRole("radio");
    expect(radios.map((radio) => radio.textContent?.replace("✓", ""))).toEqual([
      "Black",
      "Red",
      "Blue",
      "Yellow",
    ]);
    expect(radios.map((radio) => radio.getAttribute("aria-checked"))).toEqual([
      "false",
      "true",
      "false",
      "false",
    ]);
    expect(screen.getByText(EDITOR_COPY.hint)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Undo" })).toHaveProperty("disabled", true);
    expect(screen.getByRole("button", { name: "Clear drawing" })).toHaveProperty("disabled", true);
  });

  it("shows the selected ink with a tick as well as colour", () => {
    mount();
    const red = screen.getByRole("radio", { name: /Red/ });
    expect(within(red).getByText("✓")).toBeTruthy();
    expect(within(screen.getByRole("radio", { name: /Blue/ })).queryByText("✓")).toBeNull();
  });

  it("keeps the canvas out of the tab order and describes the non-drawing alternative", () => {
    const { canvas } = mount();
    expect(canvas.tabIndex).toBe(-1);
    expect(canvas.getAttribute("aria-label")).toContain(
      "You can describe the problem in the notes instead.",
    );
  });

  it("moves between inks with the arrow keys, Home and End (one tab stop, on the chosen ink)", async () => {

    mount();
    const radios = screen.getAllByRole("radio");
    expect(radios.map((radio) => radio.tabIndex)).toEqual([-1, 0, -1, -1]);
    radios[1].focus();
    fireEvent.keyDown(document.activeElement!, { key: "ArrowRight" });
    expect(screen.getByRole("radio", { name: /Blue/ }).getAttribute("aria-checked")).toBe("true");
    expect(document.activeElement).toBe(screen.getByRole("radio", { name: /Blue/ }));
    fireEvent.keyDown(document.activeElement!, { key: "End" });
    expect(screen.getByRole("radio", { name: /Yellow/ }).getAttribute("aria-checked")).toBe("true");
    fireEvent.keyDown(document.activeElement!, { key: "ArrowRight" });
    expect(screen.getByRole("radio", { name: /Black/ }).getAttribute("aria-checked")).toBe("true");
    fireEvent.keyDown(document.activeElement!, { key: "ArrowLeft" });
    expect(screen.getByRole("radio", { name: /Yellow/ }).getAttribute("aria-checked")).toBe("true");
    fireEvent.keyDown(document.activeElement!, { key: "Home" });
    expect(screen.getByRole("radio", { name: /Black/ }).getAttribute("aria-checked")).toBe("true");
    fireEvent.keyDown(document.activeElement!, { key: "ArrowDown" });
fireEvent.keyDown(document.activeElement!, { key: "ArrowUp" });
fireEvent.keyDown(document.activeElement!, { key: "a" });
    expect(screen.getByRole("radio", { name: /Black/ }).getAttribute("aria-checked")).toBe("true");
  });

  it("chooses an ink by a click", async () => {

    mount();
    fireEvent.click(screen.getByRole("radio", { name: /Yellow/ }));
    expect(screen.getByRole("radio", { name: /Yellow/ }).getAttribute("aria-checked")).toBe("true");
  });
});

describe("drawing", () => {
  it("maps the pointer to the picture's natural pixels and uses the chosen ink at the size seen on screen", async () => {

    const { canvas } = mount();
    fireEvent.click(screen.getByRole("radio", { name: /Blue/ }));
    fireEvent.pointerDown(canvas, at(100, 200));
    fireEvent.pointerMove(canvas, at(150, 260));
    fireEvent.pointerUp(canvas, at(200, 300));
    const [stroke] = allStrokes(drawing());
    expect(stroke.color).toBe("blue");
    // Shown at half size: 4 CSS px is 8 natural px, and each CSS px is 2 natural px.
    expect(stroke.width).toBe(8);
    expect(stroke.unit).toBe(2);
    expect(stroke.points).toEqual([
      { x: 200, y: 400 },
      { x: 300, y: 520 },
      { x: 400, y: 600 },
    ]);
    expect(screen.getByText("1 mark.")).toBeTruthy();
    expect(screen.getByRole("status").textContent).toBe("Mark added. 1 mark.");
  });

  it("paints the strokes on the canvas with the same code as the saved picture", () => {
    const { canvas } = mount();
    surface.clearRect.mockClear();
    fireEvent.pointerDown(canvas, at(100, 100));
    fireEvent.pointerMove(canvas, at(200, 100));
    fireEvent.pointerUp(canvas, at(300, 100));
    expect(surface.clearRect).toHaveBeenCalledWith(0, 0, 800, 1600);
    expect(surface.calls).toContain("stroke");
  });

  it("makes a tap into a one-point mark", () => {
    const { canvas } = mount();
    fireEvent.pointerDown(canvas, at(50, 50));
    fireEvent.pointerUp(canvas, at(50, 50));
    expect(strokeCount(drawing())).toBe(1);
    expect(allStrokes(drawing())[0].points).toHaveLength(1);
  });

  it("says how many marks there are", () => {
    const { canvas } = mount();
    for (const y of [50, 150]) {
      fireEvent.pointerDown(canvas, at(50, y));
      fireEvent.pointerUp(canvas, at(60, y));
    }
    expect(screen.getByText("2 marks.")).toBeTruthy();
  });

  it("ignores a press outside the picture, a right-click, and a move with nothing pressed", () => {
    const { canvas } = mount();
    fireEvent.pointerDown(canvas, { ...at(0, 0), clientX: 0, clientY: 0 });
    fireEvent.pointerDown(canvas, at(100, 100, { button: 2 }));
    fireEvent.pointerMove(canvas, at(120, 120));
    expect(strokeCount(drawing())).toBe(0);
  });

  it("does nothing until the picture has loaded", () => {
    render(<Harness />);
    const canvas = screen.getByRole("img", { name: EDITOR_COPY.canvasLabel });
    fireEvent.pointerDown(canvas, at(100, 100));
    expect(strokeCount(drawing())).toBe(0);
  });

  it("drops a stroke the browser cancelled, and ignores another pointer's moves", () => {
    const { canvas } = mount();
    fireEvent.pointerDown(canvas, at(100, 100));
    fireEvent.pointerMove(canvas, at(120, 120, { pointerId: 9 }));
    expect(allStrokes(drawing())[0].points).toHaveLength(1);
    fireEvent.pointerCancel(canvas, at(120, 120));
    expect(strokeCount(drawing())).toBe(0);
  });

  it("takes a second finger as a pinch or scroll: the stroke is dropped and nothing is drawn until both lift", () => {
    const { canvas } = mount();
    const finger = (id: number, x: number, y: number) =>
      at(x, y, { pointerId: id, pointerType: "touch" });
    fireEvent.pointerDown(canvas, finger(1, 100, 100));
    fireEvent.pointerMove(canvas, finger(1, 150, 150));
    expect(strokeCount(drawing())).toBe(1);
    fireEvent.pointerDown(canvas, finger(2, 200, 200));
    expect(strokeCount(drawing())).toBe(0);
    fireEvent.pointerMove(canvas, finger(1, 160, 160));
    fireEvent.pointerUp(canvas, finger(2, 200, 200));
    // One finger is still down: the gesture is still a pinch, so a fresh press is not a mark either.
    fireEvent.pointerMove(canvas, finger(1, 170, 170));
    expect(strokeCount(drawing())).toBe(0);
    fireEvent.pointerUp(canvas, finger(1, 170, 170));
    fireEvent.pointerDown(canvas, finger(3, 100, 300));
    fireEvent.pointerUp(canvas, finger(3, 120, 300));
    expect(strokeCount(drawing())).toBe(1);
  });

  it("does not draw while saving, and turns every control off", () => {
    const { canvas } = mount({ disabled: true });
    fireEvent.pointerDown(canvas, at(100, 100));
    expect(strokeCount(drawing())).toBe(0);
    expect(screen.getByRole("button", { name: "Undo" })).toHaveProperty("disabled", true);
    expect(screen.getByRole("button", { name: "Clear drawing" })).toHaveProperty("disabled", true);
    for (const radio of screen.getAllByRole("radio")) {
      expect(radio.getAttribute("aria-disabled")).toBe("true");
    }
  });

  it("does not change the ink while saving", async () => {

    mount({ disabled: true });
    fireEvent.click(screen.getByRole("radio", { name: /Yellow/ }));
    expect(screen.getByRole("radio", { name: /Red/ }).getAttribute("aria-checked")).toBe("true");
    screen.getByRole("radio", { name: /Red/ }).focus();
    fireEvent.keyDown(document.activeElement!, { key: "ArrowRight" });
    expect(screen.getByRole("radio", { name: /Red/ }).getAttribute("aria-checked")).toBe("true");
  });
});

describe("pointer capture", () => {
  it("is asked for, and a browser that refuses it does not stop the mark", () => {
    const { canvas } = mount();
    const capture = vi.fn(() => {
      throw new DOMException("No active pointer with that id", "NotFoundError");
    });
    canvas.setPointerCapture = capture;
    fireEvent.pointerDown(canvas, at(100, 100));
    fireEvent.pointerUp(canvas, at(120, 100));
    expect(capture).toHaveBeenCalledWith(1);
    expect(strokeCount(drawing())).toBe(1);
  });
});

describe("Undo and Clear drawing", () => {
  function twoMarks() {
    const view = mount();
    for (const y of [50, 150]) {
      fireEvent.pointerDown(view.canvas, at(50, y));
      fireEvent.pointerUp(view.canvas, at(60, y));
    }
    return view;
  }

  it("Undo takes back the last mark, and says so", async () => {

    twoMarks();
    fireEvent.click(screen.getByRole("button", { name: "Undo" }));
    expect(strokeCount(drawing())).toBe(1);
    expect(screen.getByRole("status").textContent).toBe("Undid the last mark.");
    expect(screen.getByText("1 mark.")).toBeTruthy();
  });

  it("Clear drawing removes everything, says so, and Undo then brings it all back as one step", async () => {

    twoMarks();
    fireEvent.click(screen.getByRole("button", { name: "Clear drawing" }));
    expect(strokeCount(drawing())).toBe(0);
    expect(screen.getByText(EDITOR_COPY.cleared)).toBeTruthy();
    expect(screen.getByRole("status").textContent).toBe("Drawing cleared.");
    expect(screen.getByRole("button", { name: "Clear drawing" })).toHaveProperty("disabled", true);
    expect(screen.getByRole("button", { name: "Undo" })).toHaveProperty("disabled", false);

    fireEvent.click(screen.getByRole("button", { name: "Undo" }));
    expect(strokeCount(drawing())).toBe(2);
    expect(screen.getByRole("status").textContent).toBe("Drawing restored. 2 marks.");
  });

  it("a new mark after Clear drawing leaves nothing to restore", async () => {

    const { canvas } = twoMarks();
    fireEvent.click(screen.getByRole("button", { name: "Clear drawing" }));
    fireEvent.pointerDown(canvas, at(80, 80));
    fireEvent.pointerUp(canvas, at(90, 80));
    fireEvent.click(screen.getByRole("button", { name: "Undo" }));
    expect(strokeCount(drawing())).toBe(0);
    expect(screen.getByRole("button", { name: "Undo" })).toHaveProperty("disabled", true);
  });
});

describe("the picture", () => {
  it("is shown from a local object URL that is released afterwards", () => {
    const view = mount();
    expect(view.image.getAttribute("src")).toBe("blob:preview");
    expect(view.image.getAttribute("alt")).toBe("");
    view.unmount();
    expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:preview");
  });

  it("still works where the canvas has no 2d context (nothing is painted, nothing throws)", () => {
    vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
    const { canvas } = mount();
    fireEvent.pointerDown(canvas, at(100, 100));
    fireEvent.pointerUp(canvas, at(120, 100));
    expect(strokeCount(drawing())).toBe(1);
  });

  it("starts clean for another account", () => {
    const { rerender } = render(<Harness scope="account-a" />);
    act(() => latest.begin({ color: "red", width: 4, unit: 1 }, { x: 5, y: 5 }));
    act(() => latest.end());
    expect(strokeCount(drawing())).toBe(1);
    rerender(<Harness scope="account-b" />);
    expect(strokeCount(drawing())).toBe(0);
  });

  it("offers the inks design named", () => {
    expect(INK_COLORS.map((color) => color.label)).toEqual(["Black", "Red", "Blue", "Yellow"]);
  });
});
