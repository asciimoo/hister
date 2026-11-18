---
date: '2026-11-18T00:00:00+01:00'
draft: false
title: 'Hister Is Now Nogon'
description: 'Why Hister became Nogon and what the rename means for your installation.'
---

Hister is now called **Nogon**. Development continues under the new name, with
the same purpose: helping you save and search the pages, documents, and other
content you find useful, on a server you control.

## Why the Rename

The rename follows a trademark complaint about the similarity between Hister
and another project's name. We tried to settle the dispute, but unfortunately
the other party refused to cooperate. I decided to move forward with a new name.

## Finding a New Name

Finding one turned out to be surprisingly difficult. I wanted something unique,
relatively short, and good sounding, without an existing registered trademark
getting in the way. I could not find a fitting name that met all those requirements.

Eventually, in full desperation mode, I started combining letters from
“**NO** **GO**od **N**ame.” That gave me **Nogon**. After all that searching, the lack
of a good name had finally produced one.

Later, @Groxx on Discord pointed out that it could also mean “**NO**t **GON**e.”
That fits the project much better than my original reasoning. Nogon saves and
indexes the content we find useful so we can find it again.

## What Changes for Users

For existing users, the most important part is what happens to your installation.

### Your Data Stays Where It Is

Nogon reuses existing Hister configuration and data directories. The rename
itself does not require moving your files, importing your collection again, or
rebuilding the search index. Keep your existing configuration and storage paths
when upgrading. Follow any separate upgrade instructions included with the
release you install.

### New Release Downloads Use Nogon Names

Tagged and rolling releases will provide Nogon binaries only. Install the
executable as `nogon`, or `nogon.exe` on Windows. Update scripts, shortcuts,
and service launch commands that point directly to your downloaded Hister
binary. For example, the server command becomes `nogon listen`. Package
installations may still provide the `hister` command during the transition.

### Existing Configuration Variables Still Work for Now

The renamed builds accept the existing `HISTER_` environment variables as well
as their `NOGON_` equivalents. You can keep your current settings during the
upgrade. Prefer Nogon names for new configurations; support for the old names
is temporary and is intended to be removed in a future release. See the
[configuration guide](/docs/configuration#environment-variables) when changing
an existing setup.

### Keep Your Installed Browser Extension

The rename uses the existing store listings, so you can receive it through the
browser's normal update process. The renamed extension keeps your saved server
connection and indexing preferences. Store updates may arrive at different
times, so you may see both names during the transition. The
[extension guide](/docs/browser-extension) links to the Firefox and Chrome
listings.

### Keep Using Your Own Server Address

The project's new website address does not change the URL of your installation.
Your extension and other clients should continue connecting to the same server
with the same credentials.

### Package and Container Names May Take Longer to Change

Follow the [installation guide](/docs/installing) for the package or image you
use. Existing service names, Docker volumes, and storage paths can still
contain `hister`. Keep them when upgrading so the new process opens your
existing data. Docker Compose users should also keep the same project name;
see the [container upgrade instructions](/docs/docker#upgrading-an-existing-container).

### Before You Upgrade

Before upgrading, stop the existing server and make a
[complete backup](/docs/data-lifecycle#backups-and-exports). After installing
Nogon, confirm that it selects your existing configuration and data directory
before starting it. The [upgrade guide](/docs/installing#upgrading-from-hister)
walks through those checks. There is no need to recreate your setup just to
replace its name.

## Project Links and Support

Please use [nogon.eu](https://nogon.eu/) for the project website and
[demo.nogon.eu](https://demo.nogon.eu/) to try the demo. The source repositories
are now [asciimoo/nogon on GitHub](https://github.com/asciimoo/nogon) and
[asciimoo/nogon on Codeberg](https://codeberg.org/asciimoo/nogon). Update your
bookmarks and project links as convenient. Older posts and release notes will
still refer to Hister; they are part of the project's history.

If you have questions or run into problems, open an issue on
[GitHub](https://github.com/asciimoo/nogon/issues) or
[Codeberg](https://codeberg.org/asciimoo/nogon/issues), or join us in `#hister`
on IRCNet or on [Discord](https://discord.gg/beEyuHxRSs).

Thank you to everyone who has contributed, reported problems, shared the
project, or helped make this transition easier. And thank you to @Groxx for
turning my “**NO** **GO**od **N**ame” into “**NO**t **GON**e.” I would much rather get
back to making useful things easier to find than keep searching for a name.
