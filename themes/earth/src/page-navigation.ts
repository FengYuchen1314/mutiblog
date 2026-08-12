/**
 * The browser-facing navigation indicator deliberately keeps its decision
 * making independent from the DOM.  That lets the inline script adapt a real
 * MouseEvent once, while this module owns the safety rules and remains easy
 * to exercise without a browser.
 */
export type PageNavigationClick = {
  defaultPrevented: boolean;
  button: number;
  metaKey: boolean;
  ctrlKey: boolean;
  shiftKey: boolean;
  altKey: boolean;
  hasLink: boolean;
  href: string | null;
  imageLink: boolean;
  download: boolean;
  noPageTransition: boolean;
  target: string | null;
  currentURL: string;
};

/** Returns whether a click will navigate the current document to another page. */
export function qualifiesPageNavigation(click: PageNavigationClick): boolean {
  if (
    click.defaultPrevented
    || click.button !== 0
    || click.metaKey
    || click.ctrlKey
    || click.shiftKey
    || click.altKey
    || !click.hasLink
    || click.imageLink
    || click.download
    || click.noPageTransition
  ) return false;

  const target = (click.target ?? "").trim().toLowerCase();
  if (target && target !== "_self") return false;

  let destination: URL;
  let current: URL;
  try {
    destination = new URL(click.href ?? "", click.currentURL);
    current = new URL(click.currentURL);
  } catch {
    return false;
  }

  if (destination.origin !== current.origin || !/^https?:$/.test(destination.protocol)) return false;
  return destination.pathname !== current.pathname || destination.search !== current.search;
}

export type PageNavigationTimer = unknown;

export type PageNavigationLifecycleOptions = {
  qualifies: (click: PageNavigationClick) => boolean;
  onPending: () => void;
  onReset: () => void;
  schedule: (callback: () => void, delay: number) => PageNavigationTimer;
  cancel: (timer: PageNavigationTimer) => void;
  timeoutMs?: number;
};

export type PageNavigationLifecycle = {
  handleClick: (click: PageNavigationClick) => void;
  handlePageShow: () => void;
  handlePopState: () => void;
  isPending: () => boolean;
};

/**
 * Owns indicator state while the browser changes documents. `handlePageShow`
 * and `handlePopState` are intentionally named event handlers so callers
 * cannot forget to clear a stale indicator when restoring a page from bfcache.
 */
export function createPageNavigationLifecycle(options: PageNavigationLifecycleOptions): PageNavigationLifecycle {
  let pending = false;
  let timer: PageNavigationTimer | undefined;

  const reset = () => {
    pending = false;
    if (timer !== undefined) {
      options.cancel(timer);
      timer = undefined;
    }
    options.onReset();
  };

  const setPending = () => {
    if (pending) return;
    pending = true;
    options.onPending();
    timer = options.schedule(() => {
      timer = undefined;
      pending = false;
      options.onReset();
    }, options.timeoutMs ?? 12_000);
  };

  return {
    handleClick(click) {
      if (options.qualifies(click)) setPending();
    },
    handlePageShow: reset,
    handlePopState: reset,
    isPending: () => pending,
  };
}
