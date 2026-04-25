import { sveltekit } from '@sveltejs/kit/vite';
import { SvelteKitPWA } from '@vite-pwa/sveltekit';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [
		sveltekit(),
		SvelteKitPWA({
			registerType: 'autoUpdate',
			workbox: {
				globPatterns: [
					'client/**/*.{js,css,ico,png,svg,webp,webmanifest}',
					// @vite-pwa/sveltekit always expects one prerendered glob. This SPA uses
					// adapter-static fallback instead, so point that slot at existing client assets.
					'prerendered/../client/**/*.{js,css,ico,png,svg,webp,webmanifest}'
				]
			},
			manifest: {
				name: 'Pingit',
				short_name: 'Pingit',
				theme_color: '#14213d',
				background_color: '#f6f4ef',
				display: 'standalone',
				scope: '/',
				start_url: '/',
				icons: [
					{ src: '/icon-192.png', sizes: '192x192', type: 'image/png' },
					{ src: '/icon-512.png', sizes: '512x512', type: 'image/png' }
				]
			}
		})
	]
});
