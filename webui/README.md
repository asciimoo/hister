## Structure

`app/` contains all the resources required to build the hister web UI
`website/` contains all the static site resources required to build hister.org and the documentation
`components/` contains all the reusable components used by either the `app/` or the `website/`

## Build

execute `./manage.sh build` to build the `app/`
execute `npm run build -w @hister/website` to build the `website/`

live preview available for the `website/` with: `npm run preview -w @hister/website`

## Add new component from ShadCN

```bash
cd components
npx shadcn-svelte@latest add [component]
```

change imports from `$lib/utils` to `@hister/components/utils` under `src/lib/components/ui/[component]/*` if necessary
