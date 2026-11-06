---
date: '2026-02-21T16:18:00+01:00'
draft: false
title: 'Server Setup'
description: 'Run Hister on a local network or public server with authentication, TLS, and reverse proxies.'
---

First, decide where you want your Hister server to run, relative to your client(s).
There are three options:

- **On the same computer**: simplest, but limited to that machine (single device).
- **On the same local network**: still mostly simple, but limited to that network (roughly, only for devices connected to the same router&mdash;it won't work over 4G/5G/...!).
- **Over the Internet**: requires the most setup, but accessible from everywhere.

The first one is covered by the basic setup (just run `hister listen`), but the other two require some more consideration, and modifying the Hister server's config.

## Local Network

The [`server` configuration] needs to be edited to tell the Hister server to allow talking with other machines on the local network (usually, this means "connected to your router").

1. Edit the `address` and `base_url` fields:

   ```yaml
   server:
     address: '[::]:4433'
     base_url: http://localhost:4433
   ```

   (_Optionally_, if you want to listen on a specific network interface, append `%<interface_name>` after the closing bracket `]` in `address`.)

2. To apply those changes, close the Hister server if it was already running, and restart it.
   Check that you can access the Hister Web interface at <http://localhost:4433>.
   If not, replace `[::]` in `address` with `0.0.0.0`, and retry this step.

We now need to replace `localhost` with a location other machines can use to find this computer.
You can either use a **static IP**, if you know how to assign one to a computer; otherwise, you can use the computer's **hostname**, which may be a little more finicky and less reliable.

<details><summary>Static IP</summary>

Assign the Hister server's computer its permanent IP, then register it in `base_url`.
For example, for the IP `192.168.42.69`:

```yaml
base_url: http://192.168.42.69:4433
```

It is also possible to access the Web interface using the computer's hostname even if it has a static IP, which may be easier to remember; but placing the IP in the config should remain more reliable.

</details>
<details><summary>Hostname</summary>

This can be more finicky, because sharing hostnames between computers without some setup is sometimes broken on home networks because routers break them.
This can be less reliable, because if your computer's IP address may occasionally change while the server is up, which can cause short disruptions.

1. Obtain the computer's hostname:
   - **Linux**, **macOS**: you can run the `hostname` command in the terminal, or find the hostname somewhere in system settings.
   - **Windows**: open **Settings**, then **System**, then **About**, and use the **Device name**. You can also run `hostname` in PowerShell. You may need to enable network discovery before other computers can resolve this name.

2. Replace `localhost` in `base_url:` with the hostname. Linux and macOS commonly advertise the name with a `.local` suffix. On Windows, try the device name without a suffix first.
   For example, if a laptop reports the hostname `my-laptop`, use:

   ```yaml
   server:
     address: '[::]:4433'
     base_url: http://my-laptop.local:4433
   ```

3. Restart the Hister server, then try accessing the Hister Web interface (preferably from another device!) at the address specified in the `base_url` field.
   If your browser reports being unable to connect to the server, check your firewall settings.

   If the issue persists, add or remove the `.local` suffix, restart the server, and use the same address in your browser. A static local IP is the more reliable option when hostname discovery is unavailable.

</details>

## Over the Internet

_Since this implies administration a system exposed to the Internet and **owning a domain name**, this section assumes more technical knowledge than the previous one._

Careful! **<mark>Security</mark>** considerations start rearing their head.
Hister transmits your entire browsing history, _with page contents_, to and from the server.
This is not something you want circulating unencrypted on the public Internet; therefore, we **strongly** recommend connecting to the server using HTTPS.

Hister's server does not support HTTPS itself, which is solved by using a [reverse proxy].
In particular, some, like [Caddy] or [Traefik], have built-in support for automatically requesting the required [TLS certificate].

> **Security warning:** The HTTPS proxy examples below do not authenticate users. Leave `server.proxy_auth_header` disabled unless you also configure a trusted authentication proxy to verify users and replace client supplied identity headers, and prevent untrusted clients from reaching Hister directly. Otherwise, clients can impersonate any Hister user, including administrators. See [Reverse Proxy Authentication](/docs/configuration#reverse-proxy-authentication) before enabling this feature.

1. Fill in the [`server` configuration] with your domain name and a loopback listen address:
   ```yaml
   server:
     address: 127.0.0.1:4433
     base_url: https://hister.example.com
   ```
2. (Re)start the Hister server to apply the change.
3. Set up your favourite reverse proxy. Here are some examples:

### Public Hister instances

If you want anonymous visitors to search a shared Hister index, enable public mode and keep authentication configured for write access:

```yaml
app:
  public: true
  access_token: 'your-secret-token-here'

server:
  address: 127.0.0.1:4433
  base_url: https://hister.example.com
```

You can also start a configured authenticated server with:

```bash
hister listen --public
```

Public mode allows unauthenticated search, previews, file serving, API documentation, and MCP search. It does not allow anonymous writes, history access, or admin operations. Authenticated callers can access web history normally.

Only index content intended for public access. Local files, stored previews, and MCP responses can reveal indexed content to anonymous users.

### Caddy

In your `Caddyfile`:

```
hister.example.com {
    reverse_proxy localhost:4433
}
```

Alternatively, if running Caddy solely for Hister:

```bash
caddy reverse-proxy --from hister.example.com --to localhost:4433
```

### Nginx

This example uses HTTP/2. To enable HTTP/3, follow the optional setup below.

```nginx
# Make sure you have a good TLS configuration!
# https://ssl-config.mozilla.org/#server=nginx

# You will also need to point Nginx at your TLS certificate,
# https://nginx.org/en/docs/http/configuring_https_servers.html
# and likely also set up auto-renewal, assuming you're using Let's Encrypt.
# https://letsencrypt.org/docs/client-options/

server {
	server_name hister.example.com;
	listen 443 ssl; listen [::]:443 ssl;
	http2 on;

	gzip off; # Prevents some security vulns on encrypted traffic.
	# Redirect HTTP reqs made on the HTTPS port into proper HTTPS reqs.
	error_page 497 =301 https://$host:$server_port$request_uri;

	location / {
		proxy_pass http://127.0.0.1:4433$request_uri;

		# More secure and performant than the default.
		proxy_http_version 1.1;

		# Uncomment this if you know what you're doing.
		# add_header Access-Control-Allow-Origin *;
		proxy_set_header        Host              $host;
		proxy_set_header        X-Real-IP         $remote_addr;
		proxy_set_header        X-Forwarded-For   $proxy_add_x_forwarded_for;
		proxy_set_header        X-Forwarded-Proto $scheme;
		proxy_set_header        X-Scheme          $scheme;

		# Configuration necessary for the /search endpoint.
		proxy_set_header   Upgrade    $http_upgrade;
		proxy_set_header   Connection $hister_connection; # This variable is defined in a `map` below.
		proxy_read_timeout 86400;

		# Some pages are larger than the default of 1 MiB, causing `413` errors.
		# 100 MiB should be reasonably large, adjust to taste.
		client_max_body_size 100m;
	}
}
# Optionally: add an extra `server` block to redirect HTTP to HTTPS.
server {
	server_name hister.example.com;
	listen 80;
	listen [::]:80;
	# Redirect HTTP to HTTPS.
	return 301 https://$host$request_uri;
}
# This is used to upgrade WebSocket connections on the `/search` endpoint.
map $uri $hister_connection {
	/search upgrade;
	default close;
}
```

**Note**: if search doesn't work, there likely is a problem with WebSocket connections; troubleshooting info can be gathered from Nginx's `access.log` (tells you whether requests reached Nginx) and `error.log` (tells if you if any errors occurred _within_ Nginx).

#### Optional HTTP/3

Nginx can serve HTTP/3 to browsers while forwarding requests to Hister over HTTP/1.1. Use an Nginx build with [HTTP/3 support](https://nginx.org/en/docs/quic.html), enable TLS 1.3 in your TLS configuration, and allow UDP port 443 through your firewall and any port forwarding. Keep the existing TCP listeners, certificates, and WebSocket proxy settings.

Add this default QUIC server alongside your other `server` blocks inside `http {}`. It rejects requests for sites that are not configured for HTTP/3, preventing them from reaching Hister:

```nginx
server {
	listen 443 quic reuseport default_server;
	listen [::]:443 quic reuseport default_server;
	http3 off;
	ssl_reject_handshake on;
	return 421;
}
```

Use only one QUIC `default_server` and specify `reuseport` only once per shared UDP address and port. If these listeners already exist, adapt the existing default server instead of adding a duplicate. Omit the IPv6 listeners in both examples if IPv6 is disabled.

Add these lines inside Hister's existing HTTPS `server` block:

```nginx
listen 443 quic;
listen [::]:443 quic;
http3 on;
add_header Alt-Svc 'h3=":443"; ma=300' always;
```

Keep `Alt-Svc` inside each site configured for HTTP/3, rather than setting it globally. Other sites can remain on HTTP/2. Use the short five minute advertisement while validating the setup, then increase `ma` if desired. Run `nginx -t` before reloading Nginx and check that each hostname reaches its intended site. With an HTTP/3 capable curl build, test Hister using `curl --http3-only https://hister.example.com/`.

To disable HTTP/3, remove Hister's QUIC listeners and `http3 on` directive, and temporarily replace its `Alt-Svc` advertisement with `add_header Alt-Svc 'clear' always;`. This clears previously cached advertisements when clients receive the response; removing the old header alone does not clear them immediately.

## Unix sockets

On systems with Unix socket support, Hister can serve HTTP through a filesystem socket. Set `server.address` to `unix:` followed by an absolute path:

```yaml
server:
  address: unix:/run/hister/hister.sock
  base_url: https://hister.example.com
```

The equivalent command line options are:

```bash
hister listen --address unix:/run/hister/hister.sock --server-url https://hister.example.com
```

You can also set `HISTER__SERVER__ADDRESS=unix:/run/hister/hister.sock` and `HISTER__SERVER__BASE_URL=https://hister.example.com`. `HISTER_PORT` is ignored for Unix socket addresses.

An explicit `base_url` is required because a socket path cannot provide the public HTTP URL. Browsers, the extension, and Hister CLI clients connect through the reverse proxy at that URL.

Create the parent directory before starting Hister. Its owner must be able to create the socket, and the proxy user must be able to traverse the directory and connect to the socket. Socket permissions follow the process umask. For example, a directory owned by Hister with a group shared by the proxy, mode `0750`, and umask `0007` allows the proxy group to connect on Linux. Restrict directory access as well, since some systems do not enforce socket file permissions. With systemd, `RuntimeDirectory=hister`, `RuntimeDirectoryMode=0750`, and `UMask=0007` can provide this setup when the service group is shared with the proxy.

For [Caddy](https://caddyserver.com/docs/caddyfile/directives/reverse_proxy#upstream-addresses), use the socket as the upstream:

```text
hister.example.com {
    reverse_proxy unix//run/hister/hister.sock
}
```

To check the socket directly:

```bash
curl --unix-socket /run/hister/hister.sock http://localhost/api/config
```

Hister removes its socket when it shuts down on `SIGINT` or `SIGTERM`. It refuses to replace an existing file or socket. After an unclean shutdown, remove a leftover socket only after confirming that no running process uses it.

## Monitoring

Enable `server.metrics: true` to expose operational statistics for Prometheus. The endpoint follows your configured base URL and authentication settings, and requires an administrator in multiple user mode. See [Monitoring](monitoring) for scrape configuration and metric definitions.

## AppArmor Profile

If your Linux system uses AppArmor, you can confine Hister with the following profile.
Save it to `/etc/apparmor.d/hister`, then load it with `sudo apparmor_parser -r /etc/apparmor.d/hister`.

```
abi <abi/4.0>,

include <tunables/global>

@{hister_exe} = /usr/{local/,}bin/hister

profile hister @{hister_exe} {
  include <abstractions/base>
  include <abstractions/nameservice>
  include <abstractions/ssl_certs>
  include <abstractions/user-tmp>

  network inet dgram,
  network inet stream,
  network inet6 stream,

  unix (connect, send, receive, create) type=stream,

  @{HOME}/.config/chromium/Default/History r,
  @{HOME}/.config/hister{,/**} rwlk,
  @{HOME}/.mozilla/firefox/*.*/places.sqlite r,
  @{PROC}/sys/net/core/somaxconn r,
  @{hister_exe} rix,
  owner /proc/@{pid}/{cgroup,comm,cmdline,smaps,statm,stat,mountinfo,fdinfo/*} r,
}
```

**Note**: the paths above assume the default Hister data directory (`~/.config/hister`).
If you use a custom `app.directory`, add a corresponding `rwlk` rule for that path.
Browser history paths may also need adjustment depending on which profiles you use.

[`server` configuration]: configuration#server-section
[reverse proxy]: http://en.wikipedia.org/wiki/Reverse_proxy
[Caddy]: https://caddyserver.com/
[Traefik]: https://doc.traefik.io/traefik/getting-started/
[TLS certificate]: https://en.wikipedia.org/wiki/TLS_certificate
