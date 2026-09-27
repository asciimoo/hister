---
date: '2026-03-26T00:00:00+00:00'
draft: false
title: 'User Handling'
description: 'Configure multiple users, authentication, access tokens, document ownership, and public access.'
---

User handling enables multiple independent users to share a single Hister instance. Each user has their own credentials, document collection, scoped search results, and access token for API clients. The server uses shared index files and enforces ownership during searches. User handling is disabled by default, making Hister fully backward compatible. Existing single user setups require no changes.

## Activation

Set `user_handling: true` in the `app` section of your configuration file:

```yaml
app:
  user_handling: true
```

> **Note**: When `user_handling` is active, `access_token` is used only to authenticate users by comparing it to the user's access token. This can be useful when the Hister admin sets `app.access_token` in the configuration file to their personal access token in order to execute command-line Hister commands as the admin user.

After enabling user handling, restart the server and create at least one user account before attempting to log in.

## Authentication

### Web Interface

When user handling is enabled, the web interface presents a login page to unauthenticated visitors. Enter your username and password to log in. Your session uses an HTTP only cookie containing a random opaque identifier. Sessions expire thirty days after the most recent valid request. Each valid request refreshes the session and cookie expiry to thirty days from that request.

The cookie uses `SameSite=Lax`. Its `Secure` attribute is enabled when `server.base_url` uses HTTPS and disabled when that URL uses HTTP, so loopback HTTP instances continue to work. Plain HTTP cannot protect a session from interception on the network. Use HTTPS for every network exposed instance.

If OAuth providers are configured, the login page also shows **Sign in with &lt;Provider&gt;** buttons. See [OAuth Login](#oauth-login) below.

### OAuth Login

Hister supports signing in via GitHub, Google, or any OpenID Connect provider when `server.oauth` is configured. No password is required for OAuth accounts.

When a user signs in via OAuth for the first time, Hister automatically creates a local account linked to their provider identity. GitHub uses the login name. Google uses the account name with the full email address as a fallback. OIDC uses `preferred_username` with the full email address as a fallback. Subsequent logins with the same provider identity reuse the same account.

OAuth accounts work identically to password accounts: they have scoped documents and search results, a personal access token, rules, and aliases. An OAuth user can generate a personal access token from their profile page to use with the CLI or browser extension.

See the [OAuth section of the configuration docs](/docs/configuration#oauth) for setup instructions.

### OAuth-Only Mode

Setting `server.oauth_only: true` disables password logins entirely. Only OAuth sign-in is accepted through the web interface. The login page hides the credential form and shows only the OAuth provider buttons.

This is useful when you want to enforce a single sign-on policy and prevent users from bypassing it with locally-set passwords.

Personal access tokens continue to work when `oauth_only` is enabled, so API clients and CLI tools can authenticate without a browser login. In multiple user mode, `app.access_token` is a client default and must contain a user's personal token to authenticate.

See the [OAuth-Only Mode section of the configuration docs](/docs/configuration#oauth-only-mode) for the full configuration reference.

### Browser Extension

The extension can authenticate with a personal access token. Enter the token in the popup settings or options page and save the settings.

Alternatively, the extension can copy the session cookies from the logged in web interface:

1. Log in to the Hister web interface in the same browser.
2. Click the **Authenticate with Browser Session** button in the extension popup or options page.

The extension will copy the active session cookies from the web UI. All pages indexed through the extension are stored under your user account.

### API / Command-Line Client

The `hister` CLI and any API client can authenticate using the personal access token via the `X-Access-Token` header:

```bash
curl -H "X-Access-Token: <your-token>" http://localhost:4433/api/stats
```

When using the `hister` CLI with user handling, pass your token with the `-t` flag:

```bash
hister -t <your-token> search "query"
```

## User Management Commands

All user management commands require `user_handling: true` and direct access to the server host because they update the user database directly. The `delete-user` command also contacts the configured running Hister server to search for owned documents and, with `--purge`, delete them through the API.

### `create-user`

Create a new user account. Prompts interactively for a password (minimum 8 characters).

```bash
hister create-user USERNAME [--admin]
```

| Flag      | Description                      |
| --------- | -------------------------------- |
| `--admin` | Grant the user admin privileges. |

### `delete-user`

Soft delete a user account. If the server finds indexed documents owned by that user, the command refuses to continue unless `--purge` is supplied. Purging removes the documents found by the preflight before soft deleting the account. It is not complete data erasure, as explained in [Data Lifecycle and Retention](data-lifecycle#multi-user-ownership).

```bash
hister delete-user USERNAME [--purge]
```

| Flag      | Description                                                       |
| --------- | ----------------------------------------------------------------- |
| `--purge` | Delete indexed documents found for the user before deleting them. |

### `show-user`

Display information about a user account.

```bash
hister show-user USERNAME [--token]
```

| Flag      | Description                           |
| --------- | ------------------------------------- |
| `--token` | Also display the user's access token. |

Example output:

```
Username:   alice
ID:         1
Admin:      yes
Created at: 2026-03-26 09:00:00
Updated at: 2026-03-26 09:00:00
```

### `update-user`

Modify an existing user account. At least one flag must be provided.

```bash
hister update-user USERNAME [--username NEW] [--password] [--regen-token] [--toggle-admin]
```

| Flag             | Description                                                        |
| ---------------- | ------------------------------------------------------------------ |
| `--username NEW` | Rename the user to `NEW`.                                          |
| `--password`     | Prompt for and set a new password.                                 |
| `--regen-token`  | Generate a new access token and print it. Invalidates the old one. |
| `--toggle-admin` | Toggle admin status on or off.                                     |

Flags may be combined. When `--username` is used together with other flags, the rename is applied first.
Passwords must contain at least eight characters and are entered twice for confirmation.

## Per-User Rules and Aliases

When user handling is enabled, each user has their own set of rules and aliases stored in the database. Changes made through the web UI or API affect only the authenticated user's rules and do not modify the configuration file.

- **Skip rules**: URLs matching a user's skip rules are silently ignored when indexing, just as in single-user mode.
- **Priority rules**: A user's priority rules boost matching results to the top of their search results.
- **Versioning rules**: URLs matching a user's versioning rules have their content diffed and stored on each re-index.
- **Search aliases**: Aliases defined by a user apply only to that user's searches.

Users can view and edit their rules and aliases through the **Rules** tab in the web interface, or via the API endpoints.

Expand **Add rule**, which is closed by default, then choose **Domain**, **URL**, or **Advanced regexp** to define what a rule matches. For **Domain**, enter a domain or paste a page URL, then choose whether to include subdomains. Domain rules match the hostname over HTTP and HTTPS on any port, so a domain mentioned in another site's path or query does not match. **URL** matches one complete URL literally, including its scheme, path, and query. Use the URL as shown in your index. **Advanced regexp** lets you write a custom pattern or adjust a generated one.

Choose the rule's action separately from the **Action** menu: skip indexing, prioritize in search, allow indexing, or keep document versions. Every matching option supports every action, and changing the matching option preserves the selected action.

The optional **Test URL** field checks a draft with the same matcher used by the server. It reports whether the URL matches without opening the URL or saving the rule. The check applies only to that draft; other rules, including allow and skip precedence, can still affect indexing. URL testing is also available when editing an existing rule. Select **Add rule** or save the edit to apply your changes.

In single user mode, rules and aliases continue to be read from and written to `rules.json` in the application data directory.

## Regexp

Skip rules apply upon the **full** URL (from protocol to the query-string parameters) and limited

- Anchoring must include the protocol: Eg `^https://foo.com` or `^https?://(login|mail)\.` but no `^foo.com`
- `/login$` would **not** match `https://foo.com/login?auth=1`
- URL hash is removed (`https://foo.com/#active-tab` -> `https://foo.com/`)
- Query string parameters are **not reordered**. Only `utm_*` parameters are stripped.
- [Go regular expression](https://pkg.go.dev/regexp/syntax) does not support look-ahead/look-behind regexp.

## Admin Users

Admin users have access to privileged operations. Currently, the following endpoints require admin privileges:

- **`POST /api/reindex`** rebuilds the entire full-text search index.
- **`POST /api/cleanup`** removes local documents that no longer match configured directories and removes stored HTML and favicon files that no current document references.

Non-admin users receive `403 Forbidden` when attempting to call admin-only endpoints.

Grant or revoke admin status using `create-user --admin` (at creation time) or `update-user --toggle-admin` (at any time).

## Single User Compatibility

Hister reserves user ID `0` for unauthenticated use. Documents indexed without user handling enabled are stored under user ID `0` and remain visible to all authenticated users after the feature is turned on. This means you can enable user handling on an existing instance without losing access to previously indexed content.

To make existing global documents private to one user, find the numeric user ID with
`hister show-user USERNAME`, then run the query update as an administrator:

```bash
hister update 'user_id:0' --user-id USER_ID
```

Preview the affected count with `--dry`. Ownership conflicts are skipped when that user already
owns the same URL. Watched local files also require the directory configuration `user` value to
match the new owner.

## Document Isolation

Each user's indexed documents are stored with their user ID. Searches are automatically scoped to:

- Documents indexed by the authenticated user.
- Documents indexed without user handling enabled (user ID `0`). These act as a shared, read-only baseline visible to everyone.

Users cannot see each other's documents.

The document count shown on the home page reflects the authenticated user's own document count rather than the total across all users.

## Public Mode

When `app.public: true` is enabled together with user handling, anonymous visitors can search only global documents stored under user ID `0`. Documents owned by named users remain private to those authenticated users.

Authenticated users can still add, delete, label, manage their own content, and access their web history according to the normal user handling rules. Web history remains unavailable to anonymous visitors.

## Personal Access Tokens

Every user account has a personal access token used for API authentication. Tokens are random and stored in the database.

- Generate a new token from the web UI (Profile → Generate Token) or via `hister update-user --regen-token`.
- Generating a new token immediately invalidates the previous one. Update any clients (browser extension, scripts) accordingly.
- Tokens are not displayed in `show-user` output by default; use `--token` to reveal them.

## Security Considerations

- Passwords are hashed with bcrypt before storage and are never returned by any API.
- Browser cookies contain only random session identifiers. Session data and identifier hashes are stored in the configured SQL database. Logout revokes the database record immediately.
- Personal access tokens bypass session cookies and can be used in scripts. Keep them secret and regenerate them if compromised.
- OAuth state tokens are single use random values stored in the server side session. They prevent cross site request forgery during the OAuth redirect flow.
- OAuth logins use S256 PKCE by default. A private verifier binds the authorization request to its token exchange and is stored with the state and provider in the server side session. See [PKCE and compatibility](/docs/configuration#pkce-and-compatibility) for older provider settings and upgrade behavior.
- OAuth accounts are created without a password. An administrator can assign one with `hister update-user USERNAME --password`. If you need to disable an OAuth user's access, use `hister delete-user` or remove the provider from the configuration.
- Enable `server.oauth_only: true` to enforce OAuth login and prevent password authentication. Personal access tokens remain valid for API and CLI access. In multiple user mode, `app.access_token` must contain a user's personal token.
- User handling is intended for a trusted group of users on a shared instance (family, team). For public-facing deployments, place Hister behind a reverse proxy with HTTPS and only index content that may be shown publicly.
