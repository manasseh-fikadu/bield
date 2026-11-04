import { defineCollection } from 'astro:content';
import { docsSchema } from '@astrojs/starlight/schema';
import { docsLoader } from './lib/loader';

export const collections = {
	docs: defineCollection({ loader: docsLoader(), schema: docsSchema() })
};
