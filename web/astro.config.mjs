/// <reference types="node" />
// @ts-check
import { defineConfig } from 'astro/config';
import react from '@astrojs/react';
import tailwindcss from '@tailwindcss/vite';

/**
 * SPA rewrite: routes sub-paths of an SPA route to its root path.
 * e.g. `/mcp/servers/123` → `/mcp/servers` so Astro's static SPA handles it.
 * @param {string} name
 * @param {string} rootPath
 */
function spaRewrite(name, rootPath) {
  const prefix = rootPath.endsWith('/') ? rootPath : rootPath + '/';
  return {
    name: `${name}-rewrite`,
    configureServer(/** @type {import('vite').ViteDevServer} */ server) {
      server.middlewares.use((req, _res, next) => {
        if (req.url?.startsWith(prefix) && req.url !== rootPath && !/\.\w+$/.test(req.url)) {
          req.url = rootPath;
        }
        next();
      });
    },
  };
}

// https://astro.build/config
export default defineConfig({
  integrations: [react()],
  vite: {
    plugins: [tailwindcss(), spaRewrite('mcp', '/mcp/servers'), spaRewrite('openapi', '/openapi/specs'), spaRewrite('jobs', '/jobs/'), spaRewrite('users', '/users/'), spaRewrite('groups', '/groups/'), spaRewrite('features', '/features/budget')],
    resolve: {
      dedupe: ['react', 'react-dom'],
    },
    optimizeDeps: {
      exclude: ['@astrojs/react'],
      include: [
        '@tanstack/react-query',
        '@base-ui/react/menu',
        '@base-ui/react/button',
        '@base-ui/react/dialog',
        '@base-ui/react/switch',
        '@base-ui/react/input',
        '@base-ui/react/select',
        '@radix-ui/react-popover',
        'react-markdown',
        'remark-gfm',
        'lucide-react',
        'next-themes',
        'sonner',
        'cmdk',
      ],
    },

    server: {
      proxy: {
        '/api/': {
          target: 'http://localhost:9191',
          changeOrigin: true,
        },
      },
    },
  },
  output: 'static',
  redirects: {
    '/access': '/access/keys',
    '/features': '/features/budget',
    '/llm': '/llm/providers',
    '/logs': '/logs/requests',
    '/mcp': '/mcp/servers',
    '/openapi': '/openapi/specs',
  },
  server: {
    port: 4321,
  },
});
