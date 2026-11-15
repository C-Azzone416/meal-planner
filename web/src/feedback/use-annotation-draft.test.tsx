// @vitest-environment jsdom
import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { hasMarks, strokeCount } from "./annotation-model";
import { useAnnotationDraft } from "./use-annotation-draft";

const ink = { color: "red", width: 6, unit: 1 } as const;

function drawOne(draft: ReturnType<typeof useAnnotationDraft>, x = 10) {
  draft.begin(ink, { x, y: x });
  draft.extend({ x: x + 30, y: x + 30 });
  draft.end();
}

describe("the marks on a sheet's screenshot", () => {
  it("start empty and follow a stroke from down to up", () => {
    const { result } = renderHook(() => useAnnotationDraft("account-a"));
    expect(hasMarks(result.current.drawing)).toBe(false);
    act(() => result.current.begin(ink, { x: 1, y: 1 }));
    expect(result.current.drawing.active).not.toBeNull();
    act(() => result.current.extend({ x: 40, y: 40 }));
    act(() => result.current.end());
    expect(result.current.drawing.strokes).toHaveLength(1);
    expect(result.current.drawing.active).toBeNull();
  });

  it("Undo and Clear drawing work, and a cancelled gesture leaves nothing half drawn", () => {
    const { result } = renderHook(() => useAnnotationDraft("account-a"));
    act(() => drawOne(result.current, 10));
    act(() => drawOne(result.current, 100));
    expect(strokeCount(result.current.drawing)).toBe(2);
    act(() => result.current.undo());
    expect(strokeCount(result.current.drawing)).toBe(1);
    act(() => result.current.begin(ink, { x: 5, y: 5 }));
    act(() => result.current.cancel());
    expect(result.current.drawing.active).toBeNull();
    expect(strokeCount(result.current.drawing)).toBe(1);
    act(() => result.current.clear());
    expect(hasMarks(result.current.drawing)).toBe(false);
  });

  it("are gone as soon as the account (the outbox scope) changes, and never come back", () => {
    const { result, rerender } = renderHook(({ scope }) => useAnnotationDraft(scope), {
      initialProps: { scope: "account-a" as string | null },
    });
    act(() => drawOne(result.current));
    expect(hasMarks(result.current.drawing)).toBe(true);

    rerender({ scope: "account-b" });
    expect(hasMarks(result.current.drawing)).toBe(false);
    rerender({ scope: "account-a" });
    // Even back on the first account, what was drawn before the switch is not restored: a switch discards it.
    expect(hasMarks(result.current.drawing)).toBe(false);
  });

  it("ignores a late call from the previous account's render, so its mark never lands in the new account's sheet", () => {
    const { result, rerender } = renderHook(({ scope }) => useAnnotationDraft(scope), {
      initialProps: { scope: "account-a" as string | null },
    });
    const stale = result.current;
    rerender({ scope: "account-b" });
    act(() => stale.begin(ink, { x: 9, y: 9 }));
    act(() => stale.end());
    expect(hasMarks(result.current.drawing)).toBe(false);
    act(() => drawOne(result.current, 30));
    expect(strokeCount(result.current.drawing)).toBe(1);
  });

  it("a stroke drawn after a switch belongs to the new account only", () => {
    const { result, rerender } = renderHook(({ scope }) => useAnnotationDraft(scope), {
      initialProps: { scope: "account-a" as string | null },
    });
    act(() => drawOne(result.current, 10));
    rerender({ scope: "account-b" });
    act(() => drawOne(result.current, 200));
    expect(strokeCount(result.current.drawing)).toBe(1);
    expect(result.current.drawing.strokes[0].points[0]).toEqual({ x: 200, y: 200 });
  });

  it("are gone when the sheet is signed out of (no scope), and discard() drops them now", () => {
    const { result, rerender } = renderHook(({ scope }) => useAnnotationDraft(scope), {
      initialProps: { scope: "account-a" as string | null },
    });
    act(() => drawOne(result.current));
    rerender({ scope: null });
    expect(hasMarks(result.current.drawing)).toBe(false);

    rerender({ scope: "account-a" });
    act(() => drawOne(result.current));
    act(() => result.current.discard());
    expect(hasMarks(result.current.drawing)).toBe(false);
  });

  it("two sheets open at once never share marks", () => {
    const first = renderHook(() => useAnnotationDraft("account-a"));
    const second = renderHook(() => useAnnotationDraft("account-a"));
    act(() => drawOne(first.result.current));
    expect(strokeCount(first.result.current.drawing)).toBe(1);
    expect(hasMarks(second.result.current.drawing)).toBe(false);
  });

  it("a sheet that closes and reopens starts with a clean slate", () => {
    const first = renderHook(() => useAnnotationDraft("account-a"));
    act(() => drawOne(first.result.current));
    first.unmount();
    const reopened = renderHook(() => useAnnotationDraft("account-a"));
    expect(hasMarks(reopened.result.current.drawing)).toBe(false);
  });

  it("never writes the marks to browser storage", () => {
    const local = vi.spyOn(Storage.prototype, "setItem");
    const { result } = renderHook(() => useAnnotationDraft("account-a"));
    act(() => drawOne(result.current));
    expect(local).not.toHaveBeenCalled();
    local.mockRestore();
  });
});
