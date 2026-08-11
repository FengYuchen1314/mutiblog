import { expect, test } from "vitest";
import { renderMarkdown } from "./index";

test("renders markdown while rejecting raw HTML", () => {
  const output = renderMarkdown("# Hello\n\n<script>alert(1)</script>");
  expect(output).toContain("<h1>Hello</h1>");
  expect(output).not.toContain("<script>");
});
