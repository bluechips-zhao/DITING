// A layout/detail resize can change the bottom gap without the reader scrolling
// away. Preserve the pin until upward movement or an explicit upward intent.
export function bindTranscriptScroll(
  viewport: HTMLElement,
  pinned: { current: boolean },
  onScroll: () => void,
): () => void {
  let lastTop = viewport.scrollTop;
  const nearBottom = (advance = 0) => viewport.scrollTop + viewport.clientHeight + advance >= viewport.scrollHeight - 60;
  const scroll = () => {
    pinned.current = nearBottom() || (pinned.current && viewport.scrollTop >= lastTop - 1);
    lastTop = viewport.scrollTop;
    onScroll();
  };
  const wheel = (event: WheelEvent) => {
    if (event.deltaY < 0) pinned.current = false;
    else if (event.deltaY > 0 && nearBottom(event.deltaY)) pinned.current = true;
  };
  const keyboard = (event: KeyboardEvent) => {
    if (["Home", "ArrowUp", "PageUp"].includes(event.key) || (event.key === " " && event.shiftKey)) {
      pinned.current = false;
    } else if (event.key === "End") {
      pinned.current = true;
    } else if ((event.key === "PageDown" || event.key === " ") && nearBottom(viewport.clientHeight)) {
      pinned.current = true;
    } else if (event.key === "ArrowDown" && nearBottom(40)) {
      pinned.current = true;
    }
  };
  viewport.addEventListener("scroll", scroll, { passive: true });
  viewport.addEventListener("wheel", wheel, { passive: true });
  viewport.addEventListener("keydown", keyboard);
  return () => {
    viewport.removeEventListener("scroll", scroll);
    viewport.removeEventListener("wheel", wheel);
    viewport.removeEventListener("keydown", keyboard);
  };
}
