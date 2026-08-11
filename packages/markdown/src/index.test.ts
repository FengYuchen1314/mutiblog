import { expect, test } from "vitest";
import { renderMarkdown, renderMarkdownWithHeadings } from "./index";

test("renders markdown while rejecting raw HTML", () => {
  const output = renderMarkdown("# Hello\n\n<script>alert(1)</script>");
  expect(output).toContain('<h1 id="hello">Hello</h1>');
  expect(output).not.toContain("<script>");
});

test("renders inline and display mathematics as self-contained SVG", () => {
  const output = renderMarkdown("Euler: $e^{i\\pi}+1=0$.\n\n$$\\int_0^1 x^2\\,dx=\\frac{1}{3}$$");

  expect(output.match(/class="MathJax"/g)?.length).toBe(2);
  expect(output).toContain("<svg");
  expect(output).toContain('xmlns="http://www.w3.org/2000/svg"');
  expect(output).toContain('display="true"');
  expect(output).not.toMatch(/<script\b/i);
  expect(output).not.toMatch(/\b(?:src|href|xlink:href)\s*=\s*["']https?:\/\//i);
  expect(output).not.toMatch(/url\(\s*["']?https?:\/\//i);
});

test("keeps currency and code literal while retaining numeric inline mathematics", () => {
  const output = renderMarkdown("The range is $20,000 to $30,000. Valid math is $2+2$. Use `$x$` in documentation.");

  expect(output.match(/class="MathJax"/g)?.length).toBe(1);
  expect(output).toContain("The range is $20,000 to $30,000.");
  expect(output).toContain("<code>$x$</code>");
});

test("removes active attributes from generated math markup", () => {
  const output = renderMarkdown("$\\href{javascript:alert(1)}{unsafe}$");

  expect(output).not.toMatch(/(?:href|xlink:href)=["'](?:javascript|vbscript|data):/i);
  expect(output).not.toMatch(/\son[a-z]+=/i);
  expect(output).not.toContain("<script");
});

test("adds deterministic, safe heading anchors and exposes matching metadata", () => {
  const rendered = renderMarkdownWithHeadings("# Introduction\n\n## Caffè & 茶\n\n## Caffè & 茶\n\n##### Not in the TOC");

  expect(rendered.html).toContain('<h1 id="introduction">Introduction</h1>');
  expect(rendered.html).toContain('<h2 id="caffe-茶">Caffè &amp; 茶</h2>');
  expect(rendered.html).toContain('<h2 id="caffe-茶-2">Caffè &amp; 茶</h2>');
  expect(rendered.html).not.toContain('id="not-in-the-toc"');
  expect(rendered.headings).toEqual([
    { id: "introduction", level: 1, text: "Introduction" },
    { id: "caffe-茶", level: 2, text: "Caffè & 茶" },
    { id: "caffe-茶-2", level: 2, text: "Caffè & 茶" },
  ]);
});

test("uses visible inline text, never raw inline markup, for heading metadata", () => {
  const rendered = renderMarkdownWithHeadings("## [Safe link](https://example.com) and `code`");

  expect(rendered.headings).toEqual([{ id: "safe-link-and-code", level: 2, text: "Safe link and code" }]);
  expect(rendered.html).toContain('<h2 id="safe-link-and-code">');
});
