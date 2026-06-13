# Security Policy

## Scope

Memento is a local-first application. The server binds to `127.0.0.1` only
and is not designed to be exposed to a network. It reads a local
[msgvault](https://github.com/kenn-io/msgvault) email archive and, when
configured, sends excerpts of archive content to the LLM provider you set in
`.env` (`MEMENTO_MODEL_*`). No other data leaves your machine.

Beyond the loopback bind, the server applies two browser-targeted defenses on
every request (`guardLocal` in `backend/internal/server/server.go`):

- **DNS-rebinding defense:** requests whose `Host` header is not a loopback
  hostname (`127.0.0.1`, `localhost`, `::1`) are rejected.
- **CSRF defense:** state-changing requests (POST/PATCH/DELETE) carrying a
  non-loopback `Origin` are rejected, so a page you visit cannot drive the API.

Things worth knowing before deploying anywhere unusual:

- There is still no per-user authentication. Anything that can reach the
  localhost port *as a local process* can read your archive-derived data and
  trigger LLM calls. The defenses above stop remote/cross-site browsers, not a
  local process you run yourself.
- Do not reverse-proxy Memento onto a shared or public interface. (A proxy that
  rewrites `Host` to a loopback value would defeat the rebinding check.)
- Gravatar avatar URLs are derived from contact email hashes and fetched by
  your browser from gravatar.com. Set your browser/network policy accordingly
  if that is a concern.

## Reporting a Vulnerability

Please report security issues privately via GitHub Security Advisories
("Report a vulnerability" on the repository's Security tab) rather than a
public issue. We aim to acknowledge reports within a week.
