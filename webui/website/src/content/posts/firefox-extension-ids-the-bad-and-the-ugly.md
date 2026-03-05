---
date: '2026-02-12T17:27:42+01:00'
draft: false
title: 'Firefox Extension IDs: The Bad and the Ugly'
description: 'Firefox extensions use unique internal UUIDs per installation instead of static IDs in the Origin HTTP header that creates privacy nightmare and makes CSRF protection harder to implement for extension-to-server communication'
---

If you've ever developed a web application that communicates with a browser extension, you've probably encountered the subtle but significant differences between how Chrome and Firefox handle extension identifiers. While both browsers allow developers to specify static extension IDs, their implementation approaches diverge in ways that create real problems for security, privacy, user and developer experience.

This post explores an issue I discovered while building [Hister](https://github.com/asciimoo/hister). What started as a straightforward CSRF protection implementation turned into a deep dive into Firefox's extension architecture decisions.

---

Both Chrome and Firefox allow extension developers to have a static extension ID in their manifest. This ID serves as a persistent identifier for the extension across different installations and updates.

In Chrome (and Chromium-based browsers), extension ID handling works exactly as you'd expect:

- You specify a public key in your manifest which guarantees a static extension ID
- The browser uses this ID consistently
- All network requests from the extension include this ID in the `Origin` HTTP header
- Servers can identify which extension is making requests
- The ID remains the same across all installations of the extension

If your extension ID is `cciilamhchpmbdnniabclekddabkifhb`, every installation of your extension will use that ID, and every HTTP request will identify itself with that origin.

Firefox's approach… is different:

Firefox also lets you specify a static extension ID in the manifest. However, at the moment of installation, Firefox generates a unique "internal UUID" for each installation. This UUID is what actually appears in the `Origin` header of HTTP requests, **not** the static ID you specified.

On the surface, this might seem like a minor implementation detail. In practice, it creates significant problems.

## The Bad: Breaking CSRF Protection

Cross-Site Request Forgery (CSRF) protection is a fundamental security concern for any web application. The basic problem: how do you ensure that a request to your server came from your legitimate client application and not from a malicious site?

For traditional web applications, there are well-established patterns:

- CSRF tokens embedded in forms
- Origin HTTP header checks
- SameSite cookie attributes

But browser extensions present a unique challenge. Extension code runs independently from web pages. It's not subject to the same-origin policy in the same way. This means traditional CSRF protection mechanisms don't work.

### Origin Header: The Natural Solution

The `Origin` HTTP header was designed exactly for this purpose. When a browser makes a cross-origin request, it includes an `Origin` header identifying where the request came from. For extensions, this header contains the extension ID.

In Chrome, CSRF protection for extension-to-server communication is straightforward:

```javascript
app.post('/api/add', (req, res) => {
  const allowedOrigin = 'chrome-extension://cciilamhchpmbdnniabclekddabkifhb';

  if (req.headers.origin !== allowedOrigin) {
    return res.status(403).json({ error: 'Invalid origin' });
  }

  // Process the request...
});
```

This is secure, simple, and requires no user interaction. The extension can make "authenticated" requests to your server, and you can verify they're coming from your legitimate extension, not from a malicious website or a rogue extension.

With Firefox's unique internal UUID per installation, this pattern becomes impossible: You cannot allowlist a specific origin because you don't know what the UUID will be. Each user who installs your extension gets a different UUID.

### The Workaround: Manual Configuration

The only reliable solution is to require users to manually configure a shared secret:

1. User installs your extension
2. Server generates a secret token
3. User manually copies this token into the extension's settings
4. Extension includes the token in all requests
5. Server validates the token instead of the `Origin` header

This works, but it's terrible UX:

- Extra setup steps discourage users
- High potential for user error
- Token management becomes the user's problem
- Can't automatically validate origin at the HTTP layer

## The Ugly: Privacy Implications

While breaking CSRF protection is bad for developers, Firefox's internal UUID approach has even more troubling implications for user privacy.

### A Built-in Tracking Mechanism

The internal UUID is unique per browser installation, persistent across websites, and **completely unavoidable**. This way of tracking is even worse than cookies:

**Tracking cookies:**

- Can be blocked by browser settings
- Can be cleared by the user
- Subject to SameSite policies
- Users are increasingly aware of them
- Privacy tools can block them

**Firefox extension internal UUIDs:**

- ❌ Cannot be disabled
- ❌ Cannot be cleared (except by reinstalling)
- ❌ Persist across all websites
- ❌ Invisible to users (not shown in extension details)
- ❌ Not affected by privacy tools or private browsing
- ❌ Unique to each browser installation

## Why Did Firefox Do This?

I don't have a clear answer to that. Mozilla mentions "sandboxing and security" reasons. But, for me neither of the arguments validate the usage of "internal UUID" in the `Origin` HTTP header.

I can speculate on why Firefox implemented internal UUIDs:

**Possible reason 1: Security isolation**  
Perhaps the intent was to provide better security isolation between different extension installations. If each installation has a unique ID at the browser level, it's theoretically harder for one malicious extension to impersonate another.

However, this benefit is questionable. Extension IDs are already validated by the browser. A malicious extension can't fake someone else's ID because the browser controls the `Origin` header generation and the extension installation process as well.

**Possible reason 2: Migration from legacy extension system**  
Firefox underwent a major transition from legacy XUL extensions to WebExtensions. The internal UUID system might be a holdover from the legacy architecture that was never fully reconsidered.

**Possible reason 3: Accidental consequence**  
It's possible this wasn't a deliberate design decision at all, but rather an accidental consequence of how Firefox's extension system was architected.

Whatever the reason is, the current behavior has serious flaws.

You know the issue is serious when even Chrome has a more privacy-respecting solution to the problem

#### UPDATE (2026.02.16)

[Seems like](https://bugzilla.mozilla.org/show_bug.cgi?id=1372288) their goal was to prevent **extension fingerprinting**.

## The Developer Perspective

As someone building an free software project that prioritizes privacy and local-first architecture, Firefox's behavior is frustrating:

**For users:**

- Firefox users get a worse experience (manual configuration)
- The browser marketed for privacy actually creates privacy issues
- No transparency about the internal UUID system

**For developers:**

- Can't implement proper CSRF protection via `Origin` header
- Must implement workarounds that harm UX
- Documentation becomes more complex
- Testing is harder (can't easily simulate multiple Firefox installations)

## What Should Firefox Do?

The solution is straightforward: **use a static extension ID in the `Origin` HTTP header**, just like Chrome does.

## Disclaimer

While I've spent significant amount of time researching and trying to find ways to resolve these issues, it can easily happen that I've completely missed something and there is solution to either or both of the mentioned problems. In this case please contact me at [@asciimoo@chaos.social](https://chaos.social/@asciimoo) on Mastodon.
