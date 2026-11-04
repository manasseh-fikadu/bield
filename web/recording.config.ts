import { defineConfig } from '@playwright/test';

// Records the clip in docs/images from `bield serve --demo`: `make recording`, after `make build`.
// It's a separate config from the screenshots', so retaking those doesn't film anything.
const port = Number(process.env.BIELD_SCREENSHOT_PORT ?? 4700);
const url = `http://127.0.0.1:${port}`;
const size = { width: 1280, height: 800 };

export default defineConfig({
	testDir: 'screenshots',
	testMatch: 'recording.spec.ts',
	workers: 1,
	reporter: 'list',
	use: {
		baseURL: url,
		viewport: size,
		colorScheme: 'dark',
		video: { mode: 'on', size }
	},
	webServer: {
		command: `../bield serve --demo --demo-listen 127.0.0.1:${port}`,
		url: `${url}/api/health`,
		timeout: 120_000,
		stderr: 'pipe'
	}
});
