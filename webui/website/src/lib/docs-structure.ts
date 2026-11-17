interface DocsCategory {
  name: string;
  slugs: string[];
  color: string;
}

export const docsStructure: DocsCategory[] = [
  {
    name: 'Start Here',
    slugs: ['intro', 'quickstart', 'installing', 'browser-extension', 'query-language'],
    color: 'indigo',
  },
  {
    name: 'Collecting Content',
    slugs: ['browser-ingestion', 'import', 'crawler', 'file-types'],
    color: 'coral',
  },
  {
    name: 'Data and Access',
    slugs: ['user-handling', 'rules', 'data-lifecycle'],
    color: 'amber',
  },
  {
    name: 'Search and Integrations',
    slugs: ['language-detection', 'terminal-client', 'extractors', 'mcp'],
    color: 'lime',
  },
  {
    name: 'Running Nogon',
    slugs: ['configuration', 'server-setup', 'docker', 'monitoring', 'troubleshooting'],
    color: 'teal',
  },
  {
    name: 'Development',
    slugs: ['developer'],
    color: 'indigo',
  },
];
