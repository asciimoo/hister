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

After enabling user handling, create your first administrator with
[`user create --offline --admin`](#user-create) while the server is stopped, then
start the server. OAuth and proxy authentication can create ordinary accounts
automatically. Setting `server.proxy_auth_header` also enables user handling
automatically.

Starting or reloading with user handling enabled requires an existing administrator
with a usable password, personal access token, or trusted proxy login. If none exists,
Hister rejects the change and directs you to the offline recovery command above.
When OAuth is the only login method, keep the administrator's personal access token
for command line recovery.

## Authentication

### Web Interface

When user handling is enabled, the web interface presents a login page to unauthenticated visitors. Enter your username and password to log in. Your session uses an HTTP only cookie containing a random opaque identifier. Sessions expire thirty days after the most recent valid request. Each valid request refreshes the session and cookie expiry to thirty days from that request.

The cookie uses `SameSite=Lax`. Its `Secure` attribute is enabled when `server.base_url` uses HTTPS and disabled when that URL uses HTTP, so loopback HTTP instances continue to work. Plain HTTP cannot protect a session from interception on the network. Use HTTPS for every network exposed instance.

If OAuth providers are configured, the login page also shows **Sign in with &lt;Provider&gt;** buttons. See [OAuth Login](#oauth-login) below.

After signing in, Hister returns you to the page you were viewing, including its search query or preview link. This also applies when a session expires or you choose **Login** from the menu. Opening the login page directly takes you to the home page after signing in.

### OAuth Login

Hister supports signing in via GitHub, Google, or any OpenID Connect provider when `server.oauth` is configured. No password is required for OAuth accounts.

When a user signs in via OAuth for the first time, Hister automatically creates a local account linked to their provider identity. The initial Hister username comes from the GitHub login name, the Google account name with the full email address as a fallback, or OIDC's `preferred_username` with the full email address as a fallback. Subsequent logins with the same provider identity reuse the same account. GitHub account links use a permanent account ID, so changing a GitHub username preserves access and does not rename the Hister account.

OAuth accounts work identically to password accounts: they have scoped documents and search results, a personal access token, rules, and aliases. An OAuth user can generate a personal access token from their profile page to use with the CLI or browser extension.

See the [OAuth section of the configuration docs](/docs/configuration#oauth) for setup instructions.

### OAuth-Only Mode

Setting `server.oauth_only: true` disables password logins entirely. Only OAuth sign-in is accepted through the web interface. The login page hides the credential form and shows only the OAuth provider buttons.

This is useful when you want to enforce a single sign-on policy and prevent users from bypassing it with locally-set passwords.

Personal access tokens continue to work when `oauth_only` is enabled, so API clients and CLI tools can authenticate without a browser login. In multiple user mode, `app.access_token` is a client default and must contain a user's personal token to authenticate.

See the [OAuth-Only Mode section of the configuration docs](/docs/configuration#oauth-only-mode) for the full configuration reference.

### Reverse Proxy Authentication

Hister can use a username supplied by a trusted authentication proxy through `server.proxy_auth_header`, such as `Remote-User`. This automatically enables user handling. Existing usernames reuse their accounts and permissions; new usernames receive accounts without a password or administrator privileges.

> **Security warning: Never enable `server.proxy_auth_header` without an authentication proxy in front of Hister.** The proxy must authenticate requests, discard client supplied identity headers, and set the configured header from the verified identity. Block direct access to Hister from untrusted clients. Otherwise, anyone who can supply the header can impersonate users, including administrators. A proxy that only provides HTTPS is insufficient.

Personal access tokens and existing sessions still work when the proxy header is absent. Hister's **Logout** action does not end the proxy session; sign out through the authentication proxy to stop automatic authentication.

See [Reverse Proxy Authentication](/docs/configuration#reverse-proxy-authentication) for configuration, account matching, and deployment requirements.

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

Use `hister user --help` to list account management commands.

`user create`, `user show`, `user update`, and `user delete` use the configured
running server and require an administrator's personal access token.
`user create --offline` requires `user_handling: true` in the local configuration
and direct access to the server's data.

### `user create`

Create a new account on the configured running server. User handling must be
enabled on that server; it does not need to be enabled locally. The command
prompts for a password twice. Passwords must contain 8 to 72 UTF-8 bytes; a
Unicode character may occupy multiple bytes.

```bash
hister user create USERNAME [--admin] [--offline]

# Create an account on a remote server
hister --server-url https://hister.example --token YOUR_ADMIN_TOKEN user create alice
```

| Flag        | Description                                                                         |
| ----------- | ----------------------------------------------------------------------------------- |
| `--admin`   | Grant the user admin privileges.                                                    |
| `--offline` | Create the account directly in the configured database. The server must be stopped. |

To create the first administrator or recover access, stop the server and use its
configuration and data directory:

```bash
hister --config /path/to/config.yml user create admin --offline --admin
```

Choose an unused username. Offline creation with `--admin` prints the new account's
access token. After starting the server, use this token with `--token` to manage
other accounts. Offline creation of an ordinary account does not print its token.

Creation through the server displays a confirmation without retrieving the new
token. Use `user show USERNAME --show-token` if you need it. The server must support
remote user creation. If it rejects the request or cannot be reached, the command
reports an error without creating a local account.

### `user delete`

Soft delete a user account on the configured server. That server must have `user_handling: true`. The username and owned documents are both resolved there, so local accounts are unaffected even when they have the same name or numeric ID. Local user handling does not need to be enabled.

If the user owns indexed documents, the command refuses to continue unless `--purge` is supplied. Purging removes that user's current indexed documents before deleting the account and reports the number removed. If checking or purging documents fails, the account remains; a failed purge may already have removed some documents. If account deletion is rejected after a purge, documents already removed are not restored. This is not complete data erasure, as explained in [Data Lifecycle and Retention](data-lifecycle#multi-user-ownership).

```bash
hister user delete USERNAME [--purge]

# Delete an account and its indexed documents on a remote server
hister --server-url https://hister.example --token YOUR_ADMIN_TOKEN user delete alice --purge
```

| Flag      | Description                                                      |
| --------- | ---------------------------------------------------------------- |
| `--purge` | Delete the user's indexed documents before deleting the account. |

The server must support remote user deletion. If an older server rejects the request, upgrade it and retry. The command does not fall back to deleting a local account or retry failed requests automatically.

### `user show`

Display information about a user account on the configured running server. The server must have user handling enabled, and the request requires an administrator's personal access token. Local user handling does not need to be enabled.

```bash
hister user show USERNAME [--show-token]

# Inspect a user on a remote server
hister --server-url https://hister.example --token YOUR_ADMIN_TOKEN user show alice

# Also retrieve the user's existing access token
hister --server-url https://hister.example --token YOUR_ADMIN_TOKEN user show alice --show-token
```

| Flag           | Description                                                         |
| -------------- | ------------------------------------------------------------------- |
| `--show-token` | Retrieve and display the user's existing token without rotating it. |

The display flag was renamed from `--token` to `--show-token`. The global `--token`
flag now supplies your administrator token for authentication. Without
`--show-token`, the user's token is neither retrieved nor displayed.

The username is resolved on the selected server. If the account is missing or an
older server does not support user lookup, the command reports an error and does
not read a local account with the same name.

Example output:

```
Username:   alice
ID:         1
Admin:      yes
Created at: 2026-03-26 09:00:00
Updated at: 2026-03-26 09:00:00
```

### `user update`

Modify an existing account on the configured running server. User handling must
be enabled on that server, and the command requires an administrator's personal
access token. Local user handling does not need to be enabled. At least one
change must be requested.

```bash
hister user update USERNAME [--username NEW] [--password] [--regen-token] [--toggle-admin]

# Rename an account and change its password together on a remote server
hister --server-url https://hister.example --token YOUR_ADMIN_TOKEN user update alice --username alice-new --password
```

| Flag             | Description                                                        |
| ---------------- | ------------------------------------------------------------------ |
| `--username NEW` | Rename the user to `NEW`.                                          |
| `--password`     | Prompt for and set a new password.                                 |
| `--regen-token`  | Generate a new access token and print it. Invalidates the old one. |
| `--toggle-admin` | Toggle admin status on or off.                                     |

Flags may be combined. All requested changes are applied together. Cancelling
password entry, entering mismatched passwords, or submitting an invalid change
leaves the account unchanged. Passwords must contain 8 to 72 UTF-8 bytes and are
entered twice for confirmation. Confirmations and any new token are displayed
only after the server accepts the update.

If the account changes while you are entering a password or submitting the
update, the server rejects the request. Inspect the account with `user show`
and review your intended changes before trying again. The command does not
retry conflicting updates automatically. The last administrator cannot be
demoted; create or promote another administrator first.

The server must support remote user updates. If an older server rejects the
request, upgrade it and retry. The command does not fall back to modifying local
accounts. If the connection fails after submission, inspect the account before
trying again, because the server may already have applied the update.

When rotating your own token, save the new token and use it with `--token` or
update `access_token` in your configuration for subsequent commands.

Changing a password signs that user out of all existing browser sessions,
including OAuth sessions. They can sign in again with the new password or their
configured OAuth provider. Other users' sessions are unaffected. Personal access
tokens remain valid; use `--regen-token` to replace the token as well.

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

Any signed in user can reset their own access token from the Profile page. If
the account changes before the reset completes, reload the page and review the
account before trying again.

## Admin Users

Admin users have access to privileged operations. Currently, the following endpoints require admin privileges:

- **`GET /api/users`** lists user accounts and can find an exact username.
- **`POST /api/users`** creates an account, optionally with administrator privileges.
- **`GET /api/users/{id}`** shows account information without credentials.
- **`PATCH /api/users/{id}`** changes a username, password, administrator status, or access token together.
- **`DELETE /api/users/{id}`** deletes an account, with `?purge=true` to also remove its indexed documents.
- **`GET /api/users/{id}/token`** explicitly retrieves an account's existing access token without changing it.
- **`POST /api/reindex`** rebuilds the entire full-text search index.
- **`POST /api/cleanup`** removes local documents that no longer match configured directories and removes stored HTML and favicon files that no current document references.

Non-admin users receive `403 Forbidden` when attempting to call admin-only endpoints.

Account creation, lookup, and token retrieval require user handling to be enabled. See
[User Administration Reads](/docs/developer#user-administration-reads) for pagination,
response fields, and API examples, and [User Creation](/docs/developer#user-creation)
for creating accounts through the server. [User Updates](/docs/developer#user-updates)
explains how to apply changes together and detect conflicting edits.
[User Deletion](/docs/developer#user-deletion) covers deleting accounts and their documents.
[User Token Rotation](/docs/developer#user-token-rotation) covers replacing a token on its own.
That operation allows users to rotate their own token and administrators to rotate
other users' tokens.

Grant or revoke admin status using `user create --admin` (at creation time) or `user update --toggle-admin` (at any time). The last administrator cannot be demoted or deleted. Create or promote another administrator first.

## Single User Compatibility

Hister reserves user ID `0` for unauthenticated use. Documents indexed without user handling enabled are stored under user ID `0` and remain visible to all authenticated users after the feature is turned on. This means you can enable user handling on an existing instance without losing access to previously indexed content.

To make existing global documents private to one user, find the numeric user ID with
`hister user show USERNAME`, then run the query update as an administrator:

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

- Generate a new token from the web UI (Profile → Generate Token) or via `hister user update USERNAME --regen-token`.
- Generating a new token immediately invalidates the previous one. Update any clients (browser extension, scripts) accordingly.
- Tokens are not retrieved or displayed in `user show` output by default; use `--show-token` to reveal them.

## Security Considerations

- Passwords are hashed with bcrypt before storage and are never returned by any API.
- Browser cookies contain only random session identifiers. Session data and identifier hashes are stored in the configured SQL database. Logout revokes the database record immediately.
- Personal access tokens bypass session cookies and can be used in scripts. Keep them secret and regenerate them if compromised.
- OAuth state tokens are single use random values stored in the server side session. They prevent cross site request forgery during the OAuth redirect flow.
- OAuth logins use S256 PKCE by default. A private verifier binds the authorization request to its token exchange and is stored with the state and provider in the server side session. See [PKCE and compatibility](/docs/configuration#pkce-and-compatibility) for older provider settings and upgrade behavior.
- OAuth accounts are created without a password. An administrator can assign one with `hister user update USERNAME --password`. If you need to disable an OAuth user's access, use `hister user delete` or remove the provider from the configuration.
- Enable `server.oauth_only: true` to enforce OAuth login and prevent password authentication. Personal access tokens remain valid for API and CLI access. In multiple user mode, `app.access_token` must contain a user's personal token.
- User handling is intended for a trusted group of users on a shared instance (family, team). For public-facing deployments, place Hister behind a reverse proxy with HTTPS and only index content that may be shown publicly.
