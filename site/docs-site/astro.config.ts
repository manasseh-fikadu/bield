import starlight from '@astrojs/starlight';
import { unified } from '@astrojs/markdown-remark';
import { defineConfig } from 'astro/config';
import { docsAssets } from './src/lib/assets';
import { branch, docsDir, repoRoot, repoUrl } from './src/lib/paths';
import { sidebarOf } from './src/lib/sidebar';
import { githubAlerts } from './src/plugins/github-alerts';
import { repoLinks } from './src/plugins/repo-links';

// Where the whole site is served: the landing page at its root and these pages under docs/. On
// GitHub Pages that's a path, and with a domain it's the domain. In development it's Vite's port.
const siteUrl = new URL(process.env.BIELD_SITE_URL ?? 'http://127.0.0.1:4701/');
const base = `${siteUrl.pathname.replace(/\/$/, '')}/docs`;

export default defineConfig({
	site: siteUrl.origin,
	base,
	// Into the landing page's build, where it's served from, so `bun run build` in site/ makes the whole site.
	outDir: '../build/docs',
	server: { host: '127.0.0.1', port: 4702 },
	devToolbar: { enabled: false },
	markdown: {
		processor: unified({
			remarkPlugins: [githubAlerts, [repoLinks, { repoRoot, base, repoUrl, branch }]]
		})
	},
	integrations: [
		docsAssets(docsDir, base),
		starlight({
			title: 'Bield',
			description:
				'Documentation for Bield, one binary that joins your Tailscale or Headscale network and shows every machine on it.',
			social: [{ icon: 'github', label: 'GitHub', href: repoUrl }],
			sidebar: sidebarOf(docsDir).map((group) => ({
				label: group.label,
				items: group.items
			})),
			customCss: [
				'@fontsource-variable/host-grotesk',
				'@fontsource/ibm-plex-mono/400.css',
				'./src/styles/bield.css'
			],
			components: {
				SiteTitle: './src/components/SiteTitle.astro',
				ThemeProvider: './src/components/ThemeProvider.astro',
				ThemeSelect: './src/components/Nothing.astro'
			},
			expressiveCode: { themes: ['github-dark-default'], useStarlightDarkModeSwitch: false },
			head: [
				{ tag: 'meta', attrs: { name: 'twitter:card', content: 'summary_large_image' } },
				{
					tag: 'meta',
					attrs: { property: 'og:image', content: new URL('social.png', siteUrl).href }
				},
				{
					tag: 'meta',
					attrs: { name: 'twitter:image', content: new URL('social.png', siteUrl).href }
				}
			],
			// docs/ is outside src/content/docs, so Starlight is told to treat it the same: callouts, heading links.
			markdown: { processedDirs: [docsDir] },
			lastUpdated: false,
			pagination: true
		})
	]
});
