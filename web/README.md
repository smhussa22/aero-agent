# Aero web app

React + TypeScript dashboard. Same origin as the Go API, so the browser needs no CORS.

## Run it

```
cd web && npm install
npm run build                      # writes web/dist
WEB_DIR=$PWD/dist INSECURE_DEV=1 ../backend/bin/api   # API + app on :8080 (use TLS_CERT_FILE/TLS_KEY_FILE in real use)
npm run dev                        # or: Vite dev server, proxies /api to https://localhost:8080 (AERO_API to change)
npm test                           # unit tests
```

`WEB_CONNECT_EXTRA=http://localhost:11434` lets the page call a local AI model; the Anthropic and OpenAI
APIs are always allowed. Nothing else can be reached from the page (Content-Security-Policy).

## Your AI key

Pasted in Settings or the assistant. It stays in this browser (sessionStorage, or localStorage if you tick
"Remember") and goes only to your provider. Aero's server never sees it, and exported layouts never contain it.

## Add a widget

1. Write a component that takes `WidgetProps` (`src/widgets/registry.ts`). Fetch with `usePoll` or `useLive`.
2. Add one entry to `list` in `registry.ts`: type, title, size, config `fields`, component.

The gallery, settings form, saved layouts, JSON export/import and validation all pick it up.
