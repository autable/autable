import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import React from "react";
import { afterEach, vi } from "vitest";

// The editor is lazy-loaded behind setupMonaco; tests render the mocked
// @monaco-editor/react below instead of pulling in the real Monaco bundle.
vi.mock("../monacoLocal", () => ({
  setupMonaco: () => Promise.resolve()
}));

vi.mock("@monaco-editor/react", () => ({
  default: ({
    className,
    onChange,
    options,
    value,
    wrapperProps
  }: {
    className?: string;
    onChange?: (value: string) => void;
    options?: { ariaLabel?: string; readOnly?: boolean };
    value?: string;
    wrapperProps?: Record<string, unknown>;
  }) =>
    React.createElement("textarea", {
      ...wrapperProps,
      "aria-label": options?.ariaLabel ?? String(wrapperProps?.["aria-label"] ?? ""),
      className,
      disabled: Boolean(options?.readOnly),
      onChange: (event: React.ChangeEvent<HTMLTextAreaElement>) => onChange?.(event.currentTarget.value),
      value: value ?? ""
    }),
  useMonaco: () => null
}));

afterEach(() => {
  cleanup();
});

class ResizeObserverMock {
  observe() {}
  unobserve() {}
  disconnect() {}
}

Object.defineProperty(window, "ResizeObserver", {
  writable: true,
  configurable: true,
  value: ResizeObserverMock
});

// jsdom does no layout: offsetParent is always null and every bounding rect
// is 0x0. Fluent's focus management (tabster) reads both to decide whether an
// element is visible, so without these every control inside a dialog counts as
// display:none, the dialog focuses its own surface instead of a control, and
// tabster then marks the dialog itself aria-hidden a tick later. Approximate
// the browser rules instead: an element has an offsetParent unless it, or an
// ancestor, is display:none, it is position:fixed, or it is <body>/<html>.
Object.defineProperty(HTMLElement.prototype, "offsetParent", {
  configurable: true,
  get(this: HTMLElement) {
    if (!this.isConnected || this === document.body || this === document.documentElement) {
      return null;
    }
    for (let element: HTMLElement | null = this; element; element = element.parentElement) {
      const style = window.getComputedStyle(element);
      if (style.display === "none") {
        return null;
      }
      if (element === this && style.position === "fixed") {
        return null;
      }
    }
    return this.parentElement;
  }
});

Object.defineProperty(HTMLBodyElement.prototype, "getBoundingClientRect", {
  configurable: true,
  writable: true,
  value: () => new DOMRect(0, 0, window.innerWidth, window.innerHeight)
});

Object.defineProperty(Element.prototype, "scrollIntoView", {
  writable: true,
  configurable: true,
  value: () => {}
});

Object.defineProperty(HTMLCanvasElement.prototype, "getContext", {
  writable: true,
  configurable: true,
  value: () => ({
    measureText: (text: string) => ({ width: text.length * 8 }),
    clearRect: () => {},
    fillRect: () => {},
    strokeRect: () => {},
    fillText: () => {},
    beginPath: () => {},
    moveTo: () => {},
    lineTo: () => {},
    stroke: () => {},
    save: () => {},
    restore: () => {},
    scale: () => {},
    translate: () => {},
    setLineDash: () => {}
  })
});
