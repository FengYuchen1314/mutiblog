import mathJaxScriptUrl from "mathjax/es5/tex-svg.js?url";

type MathJaxOutput = Element;

interface BrowserMathJax {
  startup: { promise: Promise<unknown> };
  tex2svgPromise(source: string, options: { display: boolean }): Promise<MathJaxOutput>;
}

declare global {
  interface Window {
    MathJax?: BrowserMathJax;
  }
}

const scriptId = "mutiblog-editor-mathjax";
let loader: Promise<BrowserMathJax> | undefined;
let conversionQueue = Promise.resolve();

/**
 * This configuration deliberately keeps MathJax's dynamic TeX package loader
 * off. Blog Markdown is untrusted input: formulae do not need to load modules,
 * define persistent macros, or attach HTML attributes/URLs.
 */
export const editorMathJaxConfiguration = {
  startup: { typeset: false },
  loader: { load: [] },
  tex: {
    packages: { "[-]": ["require", "autoload", "configmacros", "textmacros", "newcommand"] },
    maxBuffer: 5 * 1024,
    maxMacros: 1000,
  },
  svg: { fontCache: "none" },
  options: {
    enableMenu: false,
    skipHtmlTags: ["script", "noscript", "style", "textarea", "pre", "code", "annotation", "annotation-xml"],
  },
};

/**
 * Converts only the inert formula markers under a single Markdown preview.
 * It intentionally never calls MathJax on document.body, which prevents raw
 * dollar amounts, code spans, and unrelated console UI from being scanned.
 */
export function typesetPreviewMath(preview: HTMLElement): Promise<void> {
  const current = conversionQueue.then(() => convertPreviewMath(preview));
  conversionQueue = current.catch(() => undefined);
  return current;
}

async function convertPreviewMath(preview: HTMLElement) {
  const markers = Array.from(preview.querySelectorAll<HTMLElement>("[data-math-preview]"));
  if (markers.length === 0) return;

  const mathJax = await loadMathJax();
  for (const marker of markers) {
    if (!preview.contains(marker)) continue;
    const output = await mathJax.tex2svgPromise(marker.textContent ?? "", {
      display: marker.dataset.mathPreview === "display",
    });
    if (!preview.contains(marker)) continue;
    sanitizeMathJaxOutput(output);
    marker.replaceWith(output);
  }
}

function loadMathJax(): Promise<BrowserMathJax> {
  if (typeof window === "undefined" || typeof document === "undefined") {
    return Promise.reject(new Error("MathJax preview is only available in a browser."));
  }
  if (window.MathJax?.tex2svgPromise) return Promise.resolve(window.MathJax);
  if (loader) return loader;

  loader = new Promise<BrowserMathJax>((resolve, reject) => {
    window.MathJax = editorMathJaxConfiguration as unknown as BrowserMathJax;
    const script = (document.getElementById(scriptId) as HTMLScriptElement | null) ?? document.createElement("script");
    script.id = scriptId;
    script.async = true;
    script.src = mathJaxScriptUrl;
    script.addEventListener(
      "load",
      () => {
        const mathJax = window.MathJax;
        if (!mathJax?.startup?.promise || !mathJax.tex2svgPromise) {
          reject(new Error("The self-hosted MathJax component did not initialise."));
          return;
        }
        mathJax.startup.promise.then(() => resolve(mathJax), reject);
      },
      { once: true },
    );
    script.addEventListener(
      "error",
      () => reject(new Error("The self-hosted MathJax component could not be loaded.")),
      { once: true },
    );
    if (!script.isConnected) document.head.append(script);
  }).catch((error) => {
    loader = undefined;
    throw error;
  });
  return loader;
}

/**
 * MathJax has already parsed the TeX by this point, but defense in depth still
 * strips active HTML/SVG features before a result enters the preview DOM.
 */
export function sanitizeMathJaxOutput(output: Element) {
  for (const element of [output, ...Array.from(output.querySelectorAll<HTMLElement>("*"))]) {
    if (isUnsafeElement(element)) {
      element.remove();
      continue;
    }
    for (const attribute of Array.from(element.attributes)) {
      const name = attribute.name.toLowerCase();
      if (name.startsWith("on") || name === "href" || name === "xlink:href" || name === "src") {
        element.removeAttribute(attribute.name);
        continue;
      }
      if (name === "style" && /(?:expression\s*\(|url\s*\(|@import|-moz-binding)/i.test(attribute.value)) {
        element.removeAttribute(attribute.name);
      }
    }
  }
}

function isUnsafeElement(element: Element) {
  return ["script", "style", "iframe", "object", "embed", "link", "foreignobject"].includes(
    element.localName.toLowerCase(),
  );
}
