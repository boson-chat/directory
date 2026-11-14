// jsdom has no layout: give elements the scrolling API components call.
if (!Element.prototype.scrollIntoView) Element.prototype.scrollIntoView = () => {};
if (!globalThis.CSS?.escape) {
	globalThis.CSS = { ...globalThis.CSS, escape: (s: string) => s.replace(/[^\w-]/g, '\\$&') } as typeof CSS;
}
