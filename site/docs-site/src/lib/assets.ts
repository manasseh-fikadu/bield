import { cpSync, existsSync, readFileSync, statSync } from 'node:fs';
import { basename, extname, join, normalize, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import type { AstroIntegration } from 'astro';

const types: Record<string, string> = {
	'.png': 'image/png',
	'.jpg': 'image/jpeg',
	'.jpeg': 'image/jpeg',
	'.gif': 'image/gif',
	'.svg': 'image/svg+xml',
	'.webp': 'image/webp',
	'.mp4': 'video/mp4',
	'.webm': 'video/webm'
};

/**
 * Serves the files beside the pages in docs/ (the screenshots, mostly) at the same paths under the
 * site, so a page's `images/overview.png` works on the site as it does on GitHub.
 */
/** A path that isn't valid percent-encoding is a path that matches nothing. */
function safeDecode(path: string): string {
	try {
		return decodeURI(path);
	} catch {
		return '';
	}
}

export function docsAssets(dir: string, base: string): AstroIntegration {
	const isAsset = (path: string) => extname(path) !== '.md' && !basename(path).startsWith('.');
	return {
		name: 'bield-docs-assets',
		hooks: {
			'astro:server:setup': ({ server }) => {
				server.middlewares.use((req, res, next) => {
					const path = safeDecode((req.url ?? '').split(/[?#]/)[0]);
					if (!path.startsWith(`${base}/`)) return next();
					const file = normalize(join(dir, path.slice(base.length + 1)));
					if (!file.startsWith(dir + sep) || !isAsset(file) || !existsSync(file)) return next();
					if (!statSync(file).isFile()) return next();
					res.setHeader('content-type', types[extname(file)] ?? 'application/octet-stream');
					res.end(readFileSync(file));
				});
			},
			'astro:build:done': ({ dir: out }) => {
				cpSync(dir, fileURLToPath(out), {
					recursive: true,
					filter: (src) => statSync(src).isDirectory() || isAsset(src)
				});
			}
		}
	};
}
