import js from '@eslint/js';
import svelte from 'eslint-plugin-svelte';
import globals from 'globals';
import ts from 'typescript-eslint';
import svelteConfig from './svelte.config.js';

export default ts.config(
	js.configs.recommended,
	...ts.configs.recommended,
	...svelte.configs.recommended,
	{ languageOptions: { globals: { ...globals.browser } } },
	{ files: ['mock/**', '*.config.*', 'e2e/**'], languageOptions: { globals: { ...globals.node } } },
	{
		files: ['**/*.svelte', '**/*.svelte.ts'],
		languageOptions: {
			parserOptions: {
				projectService: true,
				extraFileExtensions: ['.svelte'],
				parser: ts.parser,
				svelteConfig
			}
		}
	},
	{
		// URLSearchParams here only build query strings; nothing reads them reactively.
		rules: { 'svelte/no-navigation-without-resolve': 'off', 'svelte/prefer-svelte-reactivity': 'off' }
	},
	{
		ignores: [
			'build/',
			'.svelte-kit/',
			'node_modules/',
			'test-results/',
			'playwright-report/',
			'.impeccable/'
		]
	}
);
