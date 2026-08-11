// @vitest-environment jsdom

import { describe, expect, test } from "vitest";
import {
  convertPreviewMathMarkers,
  editorMathJaxConfiguration,
  loadMathJax,
  sanitizeMathJaxOutput,
} from "./mathJaxPreview";

describe("editor MathJax safety", () => {
  test("disables dynamic TeX package loading for untrusted Markdown", () => {
    expect(editorMathJaxConfiguration.startup.typeset).toBe(false);
    expect(editorMathJaxConfiguration.tex.packages["[-]"]).toEqual(
      expect.arrayContaining(["require", "autoload", "configmacros", "textmacros"]),
    );
    expect(editorMathJaxConfiguration.svg.fontCache).toBe("none");
  });

  test("removes active or unsafe nodes and attributes from MathJax SVG output", () => {
    const output = document.createElement("mjx-container");
    output.innerHTML =
      '<svg onclick="alert(1)" style="fill: url(javascript:alert(1))"><a href="https://example.com" xlink:href="javascript:alert(1)"><path /></a><foreignObject><iframe src="https://example.com"></iframe></foreignObject></svg>';

    sanitizeMathJaxOutput(output);

    expect(output.querySelector("foreignObject")).toBeNull();
    expect(output.querySelector("iframe")).toBeNull();
    expect(output.querySelector("[onclick]")).toBeNull();
    expect(output.querySelector("[href]")).toBeNull();
    expect(output.querySelector("[xlink\\:href]")).toBeNull();
    expect(output.querySelector("svg")?.getAttribute("style")).toBeNull();
  });

  test("isolates an invalid formula so later markers still render", async () => {
    const preview = document.createElement("article");
    preview.innerHTML = '<span data-math-preview="inline">bad</span><span data-math-preview="inline">2+2</span>';

    await convertPreviewMathMarkers(preview, {
      async tex2svgPromise(source) {
        if (source === "bad") throw new Error("invalid formula");
        const output = document.createElement("mjx-container");
        output.textContent = source;
        return output;
      },
    });

    expect(preview.querySelector('[data-math-preview-error="true"]')?.textContent).toBe("$bad$");
    expect(preview.querySelector("[data-math-preview]")).toBeNull();
    expect(preview.querySelector("mjx-container")?.textContent).toBe("2+2");
  });

  test("discards a failed MathJax startup before retrying with a fresh script", async () => {
    const firstLoad = loadMathJax();
    const firstScript = document.getElementById("mutiblog-editor-mathjax") as HTMLScriptElement;
    window.MathJax = {
      startup: { promise: Promise.reject(new Error("startup failed")) },
      async tex2svgPromise() {
        return document.createElement("mjx-container");
      },
    };
    firstScript.dispatchEvent(new Event("load"));

    await expect(firstLoad).rejects.toThrow("startup failed");
    expect(window.MathJax).toBeUndefined();
    expect(firstScript.isConnected).toBe(false);

    const retry = loadMathJax();
    const retryScript = document.getElementById("mutiblog-editor-mathjax") as HTMLScriptElement;
    expect(retryScript).not.toBe(firstScript);
    retryScript.dispatchEvent(new Event("error"));
    await expect(retry).rejects.toThrow("could not be loaded");
  });
});
