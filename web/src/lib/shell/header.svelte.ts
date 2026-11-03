// What the header's breadcrumb says after the page's name. A page about one
// of several things, such as Docker on a device, sets `detail` to its name,
// and clears it when it goes.

export const header = $state({ detail: undefined as string | undefined });

/** Shows `detail` after the page's name while the calling component is mounted. */
export function crumb(detail: () => string | undefined) {
	$effect(() => {
		header.detail = detail();
		return () => {
			header.detail = undefined;
		};
	});
}
