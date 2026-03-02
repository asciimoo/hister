<script lang="ts">
   import { onMount } from 'svelte';
   import { fetchConfig, apiFetch } from '$lib/api';
   import { Button } from '@hister/components/ui/button';
   import { Input } from '@hister/components/ui/input';
   import { Badge } from '@hister/components/ui/badge';
   import * as Card from '@hister/components/ui/card';
   import * as Alert from '@hister/components/ui/alert';
   import * as Table from '@hister/components/ui/table';
   import * as Select from '@hister/components/ui/select';
   import { Shield, Link2, Plus, Trash2, AlertCircle, CheckCircle } from 'lucide-svelte';

  interface RulesData {
    skip: string[];
    priority: string[];
    aliases: Record<string, string>;
  }

  interface RuleRow {
    pattern: string;
    type: 'skip' | 'priority';
  }

  let rules: RulesData = $state({ skip: [], priority: [], aliases: {} });
  let loading = $state(true);
  let saving = $state(false);
  let message = $state('');
  let isError = $state(false);
  let newAliasKeyword = $state('');
  let newAliasValue = $state('');
  let newRulePattern = $state('');
  let newRuleType: 'skip' | 'priority' = $state('skip');

  const ruleRows = $derived.by(() => {
    const rows: RuleRow[] = [];
    for (const p of rules.skip) rows.push({ pattern: p, type: 'skip' });
    for (const p of rules.priority) rows.push({ pattern: p, type: 'priority' });
    return rows;
  });

  onMount(async () => {
    await fetchConfig();
    await loadRules();
  });

  async function loadRules() {
    loading = true;
    try {
      const res = await apiFetch('/rules', { headers: { 'Accept': 'application/json' } });
      if (!res.ok) throw new Error('Failed to load rules');
      rules = await res.json();
    } catch (e) {
      message = String(e);
      isError = true;
    } finally {
      loading = false;
    }
  }

  async function saveRules() {
    if (saving) return;
    saving = true;
    message = '';
    try {
      const formData = new URLSearchParams();
      formData.set('skip', rules.skip.join('\n'));
      formData.set('priority', rules.priority.join('\n'));
      const res = await apiFetch('/rules', {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: formData.toString()
      });
      if (!res.ok) throw new Error('Failed to save rules');
      message = 'Rules saved successfully';
      isError = false;
      await loadRules();
    } catch (e) {
      message = String(e);
      isError = true;
    } finally {
      saving = false;
    }
  }

  function removeRule(pattern: string, type: 'skip' | 'priority') {
    if (type === 'skip') {
      rules.skip = rules.skip.filter(p => p !== pattern);
    } else {
      rules.priority = rules.priority.filter(p => p !== pattern);
    }
    saveRules();
  }

  function addRule() {
    if (!newRulePattern.trim()) return;
    if (newRuleType === 'skip') {
      rules.skip = [...rules.skip, newRulePattern.trim()];
    } else {
      rules.priority = [...rules.priority, newRulePattern.trim()];
    }
    newRulePattern = '';
    saveRules();
  }

  async function deleteAlias(keyword: string) {
    const formData = new URLSearchParams({ alias: keyword });
    const res = await apiFetch('/delete_alias', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: formData.toString()
    });
    if (res.ok) await loadRules();
  }

  async function addAlias(e: Event) {
    e.preventDefault();
    if (!newAliasKeyword || !newAliasValue) return;
    const formData = new URLSearchParams({
      'alias-keyword': newAliasKeyword,
      'alias-value': newAliasValue
    });
    const res = await apiFetch('/add_alias', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: formData.toString()
    });
    if (res.ok) {
      newAliasKeyword = '';
      newAliasValue = '';
      await loadRules();
    }
  }
</script>

<svelte:head>
  <title>Hister Beta - Rules</title>
</svelte:head>

<div class="px-6 md:px-12 py-8 md:py-12 flex flex-col gap-8 md:gap-10 overflow-y-auto flex-1">
  <!-- Section Header -->
  <div class="flex flex-col gap-4">
    <div class="flex items-center gap-6">
      <div class="w-1.5 h-10 bg-hister-coral"></div>
      <h1 class="font-space text-3xl md:text-5xl font-black tracking-[3px] text-text-brand">RULES & ALIASES</h1>
    </div>
    <p class="font-inter text-base md:text-lg text-text-brand-secondary leading-relaxed max-w-[700px]">
      Configure how Hister indexes and searches your browsing history.
    </p>
    <div class="flex items-center gap-3 md:gap-4">
      <div class="flex items-center gap-2 text-hister-coral border-[3px] border-brutal-border px-4 py-2 shadow-[3px_3px_0_var(--brutal-shadow)]">
        <Shield class="size-[18px]" />
        <span class="font-outfit text-xl font-extrabold">{ruleRows.length}</span>
        <span class="font-inter text-sm">rules</span>
      </div>
      <div class="flex items-center gap-2 text-hister-indigo border-[3px] border-brutal-border px-4 py-2 shadow-[3px_3px_0_var(--brutal-shadow)]">
        <Link2 class="size-[18px]" />
        <span class="font-outfit text-xl font-extrabold">{Object.keys(rules.aliases).length}</span>
        <span class="font-inter text-sm">aliases</span>
      </div>
    </div>
  </div>

  {#if message}
    <Alert.Root class="border-[3px] rounded-none shadow-[4px_4px_0_var(--brutal-shadow)] {isError ? 'border-hister-rose bg-hister-rose/10 text-hister-rose' : 'border-hister-teal bg-hister-teal/10 text-hister-teal'}">
      {#if isError}
        <AlertCircle class="size-5" />
      {:else}
        <CheckCircle class="size-5" />
      {/if}
      <Alert.Description class="font-inter text-[15px]">{message}</Alert.Description>
    </Alert.Root>
  {/if}

  {#if loading}
    <div class="flex items-center justify-center py-16">
      <p class="font-inter text-lg text-text-brand-muted">Loading rules...</p>
    </div>
  {:else}
    <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
      <!-- Search Aliases Card -->
      <Card.Root class="bg-card-surface border-[3px] border-brutal-border rounded-none py-0 gap-0 overflow-hidden shadow-[6px_6px_0_var(--brutal-shadow)] flex flex-col">
        <Card.Header class="flex-row items-center gap-4 px-6 py-6 bg-hister-indigo">
          <div class="bg-white/20 w-12 h-12 flex items-center justify-center shrink-0">
            <Link2 class="size-6 text-white" />
          </div>
          <div class="flex flex-col gap-1">
            <Card.Title class="font-space text-xl font-extrabold tracking-[1px] text-white">SEARCH ALIASES</Card.Title>
            <Card.Description class="font-inter text-sm text-white/70">{Object.keys(rules.aliases).length} aliases configured</Card.Description>
          </div>
        </Card.Header>

        <Card.Content class="p-0 flex-1">
          <!-- Desktop table -->
          <div class="hidden md:block">
            <Table.Root>
              <Table.Header>
                <Table.Row class="bg-muted-surface border-b-[3px] border-brutal-border hover:bg-muted-surface">
                  <Table.Head class="font-space text-xs font-bold tracking-[1px] text-text-brand-muted w-[140px] px-5 py-3 h-auto">KEYWORD</Table.Head>
                  <Table.Head class="font-space text-xs font-bold tracking-[1px] text-text-brand-muted px-5 py-3 h-auto">EXPANDS TO</Table.Head>
                  <Table.Head class="w-10 px-5 py-3 h-auto"></Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {#each Object.entries(rules.aliases) as [keyword, value]}
                  <Table.Row class="border-b-[3px] border-brutal-border">
                    <Table.Cell class="font-fira text-sm font-semibold text-text-brand w-[140px] px-5 py-3">{keyword}</Table.Cell>
                    <Table.Cell class="font-fira text-sm text-text-brand-secondary truncate px-5 py-3 max-w-0">{value}</Table.Cell>
                    <Table.Cell class="w-10 px-5 py-3">
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        class="shrink-0 text-text-brand-muted hover:text-hister-rose transition-colors"
                        onclick={() => deleteAlias(keyword)}
                      >
                        <Trash2 class="size-4" />
                      </Button>
                    </Table.Cell>
                  </Table.Row>
                {/each}
              </Table.Body>
            </Table.Root>
          </div>

          <!-- Mobile stacked list -->
          <div class="md:hidden divide-y-[3px] divide-brutal-border">
            {#each Object.entries(rules.aliases) as [keyword, value]}
              <div class="flex items-center gap-3 px-4 py-3.5">
                <div class="flex-1 min-w-0">
                  <span class="font-fira text-sm font-semibold text-text-brand">{keyword}</span>
                  <span class="font-inter text-xs text-text-brand-muted mx-1.5">&rarr;</span>
                  <span class="font-fira text-sm text-text-brand-secondary truncate block">{value}</span>
                </div>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  class="shrink-0 text-text-brand-muted hover:text-hister-rose transition-colors"
                  onclick={() => deleteAlias(keyword)}
                >
                  <Trash2 class="size-4" />
                </Button>
              </div>
            {/each}
          </div>

          {#if Object.keys(rules.aliases).length === 0}
            <div class="flex flex-col items-center justify-center py-10 gap-3">
              <div class="bg-hister-indigo/10 w-12 h-12 flex items-center justify-center">
                <Link2 class="size-5 text-hister-indigo" />
              </div>
              <p class="font-inter text-sm text-text-brand-muted">No aliases defined yet.</p>
            </div>
          {/if}
        </Card.Content>

        <Card.Footer class="px-4 md:px-5 py-4 md:py-5 bg-muted-surface border-t-[3px] border-brutal-border">
          <form onsubmit={addAlias} class="flex flex-col md:flex-row items-stretch md:items-center gap-3 w-full">
            <div class="flex items-center gap-3 md:contents">
              <Input
                type="text"
                bind:value={newAliasKeyword}
                placeholder="keyword..."
                class="w-28 md:w-[140px] h-10 px-3 bg-card-surface border-[3px] border-brutal-border font-fira text-sm text-text-brand shadow-none focus-visible:ring-0 focus-visible:border-hister-indigo"
              />
              <Input
                type="text"
                bind:value={newAliasValue}
                placeholder="expands to..."
                class="flex-1 h-10 px-3 bg-card-surface border-[3px] border-brutal-border font-fira text-sm text-text-brand shadow-none focus-visible:ring-0 focus-visible:border-hister-indigo"
              />
            </div>
            <Button
              type="submit"
              class="bg-hister-indigo text-white font-space text-sm font-bold tracking-[1px] border-[3px] border-brutal-border h-10 px-5 shadow-[3px_3px_0_var(--brutal-shadow)] hover:shadow-[1px_1px_0_var(--brutal-shadow)] hover:translate-x-[2px] hover:translate-y-[2px] transition-all gap-2"
            >
              <Plus class="size-4 shrink-0" />
              ADD
            </Button>
          </form>
        </Card.Footer>
      </Card.Root>

      <!-- Indexing Rules Card -->
      <Card.Root class="bg-card-surface border-[3px] border-brutal-border rounded-none py-0 gap-0 overflow-hidden shadow-[6px_6px_0_var(--brutal-shadow)] flex flex-col">
        <Card.Header class="flex-row items-center gap-4 px-6 py-6 bg-hister-coral">
          <div class="bg-white/20 w-12 h-12 flex items-center justify-center shrink-0">
            <Shield class="size-6 text-white" />
          </div>
          <div class="flex flex-col gap-1">
            <Card.Title class="font-space text-xl font-extrabold tracking-[1px] text-white">INDEXING RULES</Card.Title>
            <Card.Description class="font-inter text-sm text-white/70">{ruleRows.length} rules configured</Card.Description>
          </div>
        </Card.Header>

        <Card.Content class="p-0 flex-1">
          <!-- Desktop table -->
          <div class="hidden md:block">
            <Table.Root>
              <Table.Header>
                <Table.Row class="bg-muted-surface border-b-[3px] border-brutal-border hover:bg-muted-surface">
                  <Table.Head class="font-space text-xs font-bold tracking-[1px] text-text-brand-muted px-5 py-3 h-auto">PATTERN</Table.Head>
                  <Table.Head class="font-space text-xs font-bold tracking-[1px] text-text-brand-muted px-5 py-3 h-auto w-28">TYPE</Table.Head>
                  <Table.Head class="w-10 px-5 py-3 h-auto"></Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {#each ruleRows as row}
                  <Table.Row class="border-b-[3px] border-brutal-border">
                    <Table.Cell class="font-fira text-sm text-text-brand truncate px-5 py-3 max-w-0">{row.pattern}</Table.Cell>
                    <Table.Cell class="px-5 py-3 w-28">
                      <Badge
                        variant="default"
                        class="font-space text-xs font-bold tracking-[0.5px] px-3 py-1 border-0 {row.type === 'skip' ? 'bg-hister-rose text-white' : 'bg-hister-teal text-white'}"
                      >
                        {row.type === 'skip' ? 'SKIP' : 'PRIORITY'}
                      </Badge>
                    </Table.Cell>
                    <Table.Cell class="w-10 px-5 py-3">
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        class="shrink-0 text-text-brand-muted hover:text-hister-rose transition-colors"
                        onclick={() => removeRule(row.pattern, row.type)}
                      >
                        <Trash2 class="size-4" />
                      </Button>
                    </Table.Cell>
                  </Table.Row>
                {/each}
              </Table.Body>
            </Table.Root>
          </div>

          <!-- Mobile stacked list -->
          <div class="md:hidden divide-y-[3px] divide-brutal-border">
            {#each ruleRows as row}
              <div class="flex items-center gap-3 px-4 py-3.5">
                <div class="flex-1 min-w-0">
                  <span class="font-fira text-sm text-text-brand block truncate">{row.pattern}</span>
                </div>
                <Badge
                  variant="default"
                  class="font-space text-xs font-bold tracking-[0.5px] px-2.5 py-0.5 border-0 shrink-0 {row.type === 'skip' ? 'bg-hister-rose text-white' : 'bg-hister-teal text-white'}"
                >
                  {row.type === 'skip' ? 'SKIP' : 'PRIORITY'}
                </Badge>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  class="shrink-0 text-text-brand-muted hover:text-hister-rose transition-colors"
                  onclick={() => removeRule(row.pattern, row.type)}
                >
                  <Trash2 class="size-4" />
                </Button>
              </div>
            {/each}
          </div>

          {#if ruleRows.length === 0}
            <div class="flex flex-col items-center justify-center py-10 gap-3">
              <div class="bg-hister-coral/10 w-12 h-12 flex items-center justify-center">
                <Shield class="size-5 text-hister-coral" />
              </div>
              <p class="font-inter text-sm text-text-brand-muted">No rules defined yet.</p>
            </div>
          {/if}
        </Card.Content>

        <Card.Footer class="px-4 md:px-5 py-4 md:py-5 bg-muted-surface border-t-[3px] border-brutal-border">
          <div class="flex flex-col md:flex-row items-stretch md:items-center gap-3 w-full">
            <div class="flex items-center gap-3 md:contents">
              <Input
                type="text"
                bind:value={newRulePattern}
                placeholder="Enter regex pattern..."
                class="flex-1 h-10 px-3 bg-card-surface border-[3px] border-brutal-border font-fira text-sm text-text-brand shadow-none focus-visible:ring-0 focus-visible:border-hister-coral"
              />
              <Select.Root type="single" bind:value={newRuleType}>
                <Select.Trigger class="h-10 px-3 w-[100px] md:w-[110px] bg-card-surface border-[3px] border-brutal-border font-space text-xs font-bold tracking-[0.5px] text-text-brand outline-none cursor-pointer shrink-0 rounded-none shadow-none focus-visible:ring-0 focus-visible:border-hister-coral justify-center gap-1">
                  {newRuleType === 'skip' ? 'SKIP' : 'PRIORITY'}
                </Select.Trigger>
                <Select.Content class="border-[3px] border-brutal-border bg-card-surface rounded-none shadow-[4px_4px_0_var(--brutal-shadow)] min-w-[100px]">
                  <Select.Item value="skip" label="SKIP" class="font-space text-xs font-bold tracking-[0.5px] rounded-none cursor-pointer">SKIP</Select.Item>
                  <Select.Item value="priority" label="PRIORITY" class="font-space text-xs font-bold tracking-[0.5px] rounded-none cursor-pointer">PRIORITY</Select.Item>
                </Select.Content>
              </Select.Root>
            </div>
            <Button
              type="button"
              onclick={addRule}
              class="bg-hister-coral text-white font-space text-sm font-bold tracking-[1px] border-[3px] border-brutal-border h-10 px-5 shadow-[3px_3px_0_var(--brutal-shadow)] hover:shadow-[1px_1px_0_var(--brutal-shadow)] hover:translate-x-[2px] hover:translate-y-[2px] transition-all gap-2"
            >
              <Plus class="size-4 shrink-0" />
              ADD
            </Button>
          </div>
        </Card.Footer>
      </Card.Root>
    </div>
  {/if}
</div>
