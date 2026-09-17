---
date: '2026-09-10T08:00:00+02:00'
draft: false
title: 'Hister: A Private Search Engine for Teams and Organizations'
description: 'Run Hister for a team, cooperative, or hackerspace with personal browsing collections and shared reference material on one server.'
---

Every group has a few pages that everyone needs. The handbook, the equipment
manual, the project wiki, the guide someone always sends to new members.

Then there is everything each person finds while working: an explanation of a
problem, a useful discussion, an article worth coming back to. That collection
looks different for everyone, even when they work on the same project.

Hister can handle both on one server. Each member gets an account and a personal
searchable collection. Shared documents appear alongside their own results.
The group maintains one installation, and each person builds on the same
starting material as they browse.

Here is how that could work for a small team. The same setup fits a cooperative,
a research group, or a hackerspace.

## Alice, Bob, and the Shared Handbook

Imagine Alice and Bob working on the same project. Both need the team's handbook
and reference documentation. Alice is investigating a database problem, while
Bob is reading about deployment tooling.

An administrator indexes the handbook as shared material. Alice and Bob connect
their browser extensions using their own accounts. With the extension's default
personal submission setting, the pages they browse go into their respective
collections.

Their searches now cover:

| Indexed material             | Alice can find it | Bob can find it |
| ---------------------------- | ----------------- | --------------- |
| Shared team handbook         | Yes               | Yes             |
| Article indexed under Alice  | Yes               | No              |
| Discussion indexed under Bob | No                | Yes             |

When Alice searches for `database backup`, she can find the team's backup
instructions alongside an article she read about restoring PostgreSQL. Bob can
find the shared instructions too. His searches also include whatever he has
indexed under his own account.

The server applies this scope automatically. Neither person needs to add an
ownership filter to everyday queries.

## Set Up the Group's Instance

Start with a fresh Hister installation using the [installation](/docs/installing)
and [server setup](/docs/server-setup) guides. This example uses
`https://hister.example.com`, with an HTTPS reverse proxy on the same machine as
Hister.

Add these settings to the group's `config.yml`:

```yaml
app:
  user_handling: true
  public: false

server:
  address: 127.0.0.1:4433
  base_url: https://hister.example.com
```

Replace the example hostname with your own. The reverse proxy forwards requests
to `127.0.0.1:4433`; setting `base_url` alone does not configure HTTPS.

`user_handling` enables individual accounts. With `public: false`, searching the
instance requires authentication, including searches over shared documents.

On the server host, create an administrator and two ordinary member accounts:

```bash
hister --config config.yml create-user admin --admin
hister --config config.yml create-user alice
hister --config config.yml create-user bob
```

Each command prompts for a password. Run these commands with the same
configuration and operating system account that will run the server, so they
use the same database and data directory.

Then start Hister:

```bash
hister --config config.yml listen
```

Alice and Bob can now open the group's address and sign in.

If you are adapting an existing personal installation, review its document
ownership first. Documents indexed while user handling was disabled belong to
user ID `0` and become visible to every signed in member. The
[user handling guide](/docs/user-handling#single-user-compatibility) explains how
to move those documents to a personal account.

## Give Everyone a Useful Starting Collection

Hister calls documents owned by user ID `0` global documents. In this setup,
they form the shared collection available to all members.

The administrator can add to it with the `--global` indexing flag. First,
retrieve the administrator's personal access token on the server host:

```bash
hister --config config.yml show-user admin --token
```

Use that token in place of `ADMIN_TOKEN` below. Run the crawl from the same
server configuration, replacing the handbook URL and allowed domain with your
own:

```bash
hister --config config.yml --token 'ADMIN_TOKEN' index \
  --recursive \
  --global \
  --label=team-handbook \
  --allowed-domain=handbook.example.com \
  --max-depth=4 \
  --max-links=200 \
  https://handbook.example.com/
```

`--global` makes the indexed pages available to every member. It requires an
administrator's token in this command. `--label` gives the collection a name
people can use when searching. The remaining options keep the crawl within the
chosen site and limit its size.

The token authenticates with Hister. If the handbook itself requires a login,
configure access to that source separately using the
[crawler's authentication options](/docs/crawler).

Both Alice and Bob can now search:

```text
label:team-handbook database backup
```

To search across the whole shared collection, use:

```text
user_id:0 database backup
```

Labels organize results. Every member can still search all global documents,
whatever label they carry. Choose shared sources with that audience in mind.

A small collection is enough to start: the handbook, a project wiki, and the
reference pages members regularly ask for. For more crawling examples, see
[Index Your Entire Documentation Stack](/posts/index-your-documentation).

## Connect Each Member's Browser

Alice installs the [browser extension](/docs/browser-extension), sets its server
URL to the group's Hister address, and authenticates with her own account.

She can generate a personal access token from her Hister profile and save it in
the extension. Alternatively, she can sign in to the web interface in the same
browser and click **Authenticate with Browser Session** in the extension.

Bob follows the same steps with his account. Each member uses their own
credentials, including in command line clients.

For personal browsing, keep **Submit as public documents** turned off in the
extension. That is the default. Pages captured while Alice browses are then
stored under Alice, and pages captured by Bob are stored under Bob.

Each account also has its own skip rules, priority rules, and search aliases.
Alice can tune her search around database work while Bob creates shortcuts for
deployment documentation. Their rules affect their own searches and indexing.

Personal collections are separated through Hister's access controls. The
operator of the server controls the stored data, so choose someone the group
trusts to host it.

## Share Useful Discoveries Deliberately

Suppose Alice finds a guide the whole team could use. One simple workflow is to
send its URL to the administrator, who adds it to the shared collection with
`--global` after reviewing it.

Members can also contribute directly through the extension's **Submit as public
documents** setting. When enabled, newly submitted pages go into the global
collection. With the server configuration above, those pages are searchable by
signed in members. Enabling `app.public` on the server would also make global
documents accessible to anonymous visitors.

The extension setting applies to subsequent submissions until it is turned off.
For a deliberate sharing session, disable automatic indexing, enable public
submission, manually index the intended pages, then turn public submission off
before returning to personal browsing.

The group can choose the contribution habit that works for it. An administrator
can maintain the core references, while members add selected discoveries as
they work.

## Keep the Collection Useful for the Next Member

Shared material needs someone to look after it. Give a member responsibility
for each source: rerun the crawl when the handbook changes, check that useful
pages were captured, and remove material the group has stopped using.

When a new member joins, create an account and connect their extension. Their
personal collection starts empty, but the shared handbook and reference
material are already searchable. Their own browsing gradually adds the context
that makes the search useful to them.

For a hackerspace, those first results might be equipment manuals and workshop
notes. For a research group, they might be background reading and project
documentation. For a cooperative, they might be operating procedures and the
answers new members regularly need.

Start with the pages your group keeps sending to each other. Give them a shared
home in Hister, then let each member build their own collection around them.
