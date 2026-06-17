import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import React from "react";
import { afterEach, vi } from "vitest";

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
