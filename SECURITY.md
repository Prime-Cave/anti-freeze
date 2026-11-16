# Security policy

## Reporting a vulnerability

Please report security problems **privately**, not in a public issue.

1. Go to the repository's **Security** tab.
2. Choose **Report a vulnerability**. This opens a private GitHub security advisory that only the maintainers can see.
3. Describe the problem, the macOS version and Headroom version, and how to reproduce it.

You should get a reply within a week. Once a fix is released, the advisory is published and you are credited unless you ask not to be.

## Supported versions

Only the latest release gets security fixes.

## What counts

Headroom runs as you, never asks for admin rights, has no network access and never deletes files. Anything that breaks one of those promises is a security bug. Examples:

* Headroom opens a network connection or loads remote content.
* An action quits an app without the safety check, or force quits without the second confirmation.
* Files are deleted, or moved to the Trash from outside the allowed folders (for example via symlinks or a swapped file).
* Another app or a web page can drive Headroom's actions.
* Personal data leaves the Mac, or is written anywhere except Headroom's own folder.

See [docs/PRIVACY.md](docs/PRIVACY.md) for what Headroom stores.
