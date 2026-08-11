// @vitest-environment jsdom

import { describe, expect, test } from "vitest";
import { editorMathJaxConfiguration, sanitizeMathJaxOutput } from "./mathJaxPreview";

describe("editor MathJax safety", () => {
  test("disables dynamic TeX package loading for untrusted Markdown", () => {
    expect(editorMathJaxConfiguration.startup.typeset).toBe(false);
    expect(editorMathJaxConfiguration.tex.packages["[-]"]).toEqual(expect.arrayContaining(["require", "autoload", "configmacros", "textmacros"]));
    expect(editorMathJaxConfiguration.svg.fontCache).toBe("none");
  });

  test("removes active or unsafe nodes and attributes from MathJax SVG output", () => {
    const output = document.createElement("mjx-container");
    output.innerHTML = '<svg onclick="alert(1)" style="fill: url(javascript:alert(1))"><a href="https://example.com" xlink:href="javascript:alert(1)"><path /></a><foreignObject><iframe src="https://example.com"></iframe></foreignObject></svg>';

    sanitizeMathJaxOutput(output);

    expect(output.querySelector("foreignObject")).toBeNull();
    expect(output.querySelector("iframe")).toBeNull();
    expect(output.querySelector("[onclick]")).toBeNull();
    expect(output.querySelector("[href]")).toBeNull();
    expect(output.querySelector("[xlink\\:href]")).toBeNull();
    expect(output.querySelector("svg")?.getAttribute("style")).toBeNull();
  });
});
