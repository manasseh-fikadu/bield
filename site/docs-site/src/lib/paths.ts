import { fileURLToPath } from 'node:url';

/** The repository's root, which docs/ sits in. */
export const repoRoot = fileURLToPath(new URL('../../../../', import.meta.url));

/** Where the pages are read from, in place. */
export const docsDir = `${repoRoot}docs`;

export const repoUrl = 'https://github.com/manasseh-fikadu/bield';
export const branch = 'main';
