## Structure

`app/` contains all the resources required to build the Nogon web UI
`website/` contains all the static site resources required to build nogon.eu and the documentation
`components/` contains all the reusable components used by either the `app/` or the `website/`
`ext/` contains the browser extension

## Build

From the repository root, execute `./manage.sh build` to build the app and Go binary.
Execute `npm run build -w @nogon/website` to build the website, or `npm run build -w @nogon/ext` to build the browser extension.

A live website preview is available with `npm run preview -w @nogon/website`.

## Add new component from ShadCN

```bash
cd webui/components
npx shadcn-svelte@latest add [component]
```

Change imports from `$lib/utils` to `@nogon/components/utils` under `src/lib/components/ui/[component]/*` if necessary.
