import { describe, expect, test } from "vitest";
import { createPreviewMarkdownRenderer } from "./markdownPreview";

describe("editor Markdown mathematics", () => {
  test("marks inline and display dollar mathematics for the browser-only converter", () => {
    const output = createPreviewMarkdownRenderer().render(
      "Euler: $e^{i\\pi}+1=0$.\n\n$$\\int_0^1 x^2\\,dx=\\frac{1}{3}$$",
    );
    const endOfParagraph = createPreviewMarkdownRenderer().render("At end: $x$");

    expect(output).toContain('data-math-preview="inline">e^{i\\pi}+1=0</span>');
    expect(output).toContain('class="math-display" data-math-preview="display">\\int_0^1 x^2\\,dx=\\frac{1}{3}');
    expect(endOfParagraph).toContain('data-math-preview="inline">x</span>');
  });

  test("keeps currency, escaped dollars, and code spans out of the MathJax markers", () => {
    const output = createPreviewMarkdownRenderer().render(
      "The range is $20,000 to $30,000. Valid math is $2+2$ and $2 + 2$. Use `$x$` and \\$literal.",
    );

    expect(output).toContain("The range is $20,000 to $30,000.");
    expect(output).toContain("<code>$x$</code>");
    expect(output).toContain("$literal.");
    expect(output.match(/data-math-preview=/g)).toHaveLength(2);
    expect(output).toContain('data-math-preview="inline">2+2</span>');
    expect(output).toContain('data-math-preview="inline">2 + 2</span>');
  });

  test("does not pair a currency amount with dollars inside a later code span", () => {
    const output = createPreviewMarkdownRenderer().render("Price: $20. Use `$x$`.");

    expect(output).toContain("Price: $20.");
    expect(output).toContain("<code>$x$</code>");
    expect(output).not.toContain("data-math-preview");
  });

  test("explicitly rejects dynamic or URL-capable TeX before creating a marker", () => {
    const output = createPreviewMarkdownRenderer().render(
      "$\\href{javascript:alert(1)}{unsafe}$\n\n$$\\require{html} x$$",
    );

    expect(output).toContain("$\\href{javascript:alert(1)}{unsafe}$");
    expect(output).toContain("$$\\require{html} x$$");
    expect(output).not.toContain("data-math-preview");
    expect(output).not.toContain("<a ");
    expect(output).not.toContain('href="');
  });

  test("does not swallow later Markdown while a display formula is unfinished", () => {
    const output = createPreviewMarkdownRenderer().render("Before\n\n$$\\frac{1}{2}\n\n## Still visible\n\nAfter");

    expect(output).not.toContain("data-math-preview");
    expect(output).toContain("$$\\frac{1}{2}");
    expect(output).toContain("<h2>Still visible</h2>");
    expect(output).toContain("<p>After</p>");
  });
});
