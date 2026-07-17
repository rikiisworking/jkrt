# Auth, cookies, and Cloudflare Tunnel

Phase 5 ops guide. Product rules: single Learner, password + HMAC session cookie, **never expose the app publicly with `JKRT_AUTH=off`**.

## Session cookie

| Item | Value |
|------|--------|
| Name | `jkrt_session` |
| Signing | HMAC-SHA256 over `userID\|expUnix` (base64url cookie payload) |
| Algorithm for password | bcrypt cost ≥ 10 |
| TTL | `JKRT_SESSION_TTL` (default **`168h`** = 7 days) |
| Flags | **`HttpOnly`**, **`SameSite=Lax`**, path `/` |
| **`Secure`** | Set when the request is HTTPS **or** `X-Forwarded-Proto: https` (Cloudflare Tunnel does this) |
| Expiry | Cookie `Expires` + `MaxAge` match TTL; server also rejects expired payloads even if the browser still sends the cookie |

Unauthenticated access when auth is on:

- HTML routes → **302** to `/login`
- `/api/*` or `Accept: application/json` → **401** `{"error":"unauthorized"}`

Logout clears the cookie and redirects to `/login`. Changing `JKRT_SESSION_SECRET` and restarting invalidates **all** existing sessions (signatures no longer match).

### Env (auth on)

```bash
export JKRT_AUTH=on                                    # default
export JKRT_PASSWORD='…'                               # only needed to bootstrap user 1 once
export JKRT_SESSION_SECRET="$(openssl rand -hex 32)"   # ≥32 bytes; keep secret
export JKRT_SESSION_TTL=168h                           # optional override
```

`JKRT_AUTH=off` is **local development only**. Do not use it with any public hostname or tunnel.

## Password rotate

Bootstrap (`JKRT_PASSWORD` on first start) only creates user id=1 when missing. It does **not** change an existing password. To rotate without wiping Cards/Reviews:

```bash
# Server can be stopped or running (SQLite single-user; prefer stopped if unsure).
go run ./cmd/setpassword -db ./jkrt.db
# prompts twice for the new password (or pass as first arg / stdin)
```

What this does:

1. bcrypt-hashes the new password  
2. `UPDATE users SET password_hash = ? WHERE id = 1`  

What it does **not** do:

- Invalidate existing cookies (log out on devices, or change `JKRT_SESSION_SECRET` and restart)  
- Delete learning data  

After a suspected compromise: rotate password **and** set a new `JKRT_SESSION_SECRET`, then restart.

## Cloudflare Tunnel (iPhone / remote)

Goal: reach the local JKRT server from your phone over HTTPS without opening inbound ports. Auth must stay **on**.

### Never do this

```bash
# WRONG — public URL with open app
export JKRT_AUTH=off
cloudflared tunnel --url http://127.0.0.1:8080
```

Anyone who finds the URL can scrape, review, and read your SQLite-backed data.

### Recommended flow

1. **Auth on** with a strong password and random session secret (see above).  
2. Start JKRT bound to localhost:

   ```bash
   export JKRT_AUTH=on
   export JKRT_PASSWORD='…'          # if user not bootstrapped yet
   export JKRT_SESSION_SECRET="$(openssl rand -hex 32)"
   export JKRT_ADDR=127.0.0.1:8080   # avoid LAN exposure
   go run ./cmd/server
   ```

3. Install [cloudflared](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/install-and-setup/installation/) and create a tunnel (named tunnel + public hostname in Zero Trust dashboard, **or** quick tunnel for a temporary test URL).

   Quick test (ephemeral URL; still requires login):

   ```bash
   cloudflared tunnel --url http://127.0.0.1:8080
   ```

   Prefer a **named tunnel** + your own hostname for daily use so the URL is stable and you control DNS/access policies.

4. On the phone: open `https://…` → `/login` → password → use dashboard / Review.

5. Confirm cookie behaviour: after login over HTTPS, DevTools (or request inspection) should show `jkrt_session` with **Secure; HttpOnly; SameSite=Lax**.

### Checklist before sharing the hostname with yourself on mobile

- [ ] `JKRT_AUTH=on`  
- [ ] User bootstrapped; password not the placeholder `change-me` from `.env.example`  
- [ ] `JKRT_SESSION_SECRET` is long random and not committed  
- [ ] Server listens on `127.0.0.1` (or only reachable by cloudflared)  
- [ ] Tunnel points at that local address only  
- [ ] Optional: Cloudflare Access in front of the hostname for a second factor  

### Health without login

`GET /health` is intentionally public (`{"status":"ok"}`) for liveness. It does not expose learning data. Everything else that matters sits behind auth when auth is on.

## Related code

| Piece | Location |
|-------|----------|
| Config | `internal/config` (`JKRT_AUTH`, secret, TTL) |
| Session sign/parse | `internal/auth/session.go` |
| Password hash / rotate | `internal/auth/password.go`, `SetPassword`, `cmd/setpassword` |
| Cookie flags on login | `internal/http/handlers.go` (`handleLoginPost`) |
| Middleware | `requireAuth` — 302 HTML / 401 API |
