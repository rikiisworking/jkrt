# Auth, cookies, and Cloudflare Tunnel

Ops guide for password auth and reaching JKRT from a phone over HTTPS.  
Product rule: **never expose the app publicly with `JKRT_AUTH=off`.**

| Section | When to read |
|---------|----------------|
| [Session cookie](#session-cookie) | How login works |
| [Password rotate](#password-rotate) | Change password without wiping Cards |
| [Cloudflare Tunnel (detailed)](#cloudflare-tunnel-detailed) | Phone / remote access step-by-step |
| [Checklist](#checklist-before-mobile-use) | Final safety pass |

---

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
export JKRT_ADDR=127.0.0.1:8080                        # recommended when tunneling
```

Or use the Makefile after `make env` and editing `.env`:

```bash
make run-auth
```

`JKRT_AUTH=off` is **local development only**. Do not use it with any public hostname or tunnel.

---

## Password rotate

Bootstrap (`JKRT_PASSWORD` on first start) only creates user id=1 when missing. It does **not** change an existing password. To rotate without wiping Cards/Reviews:

```bash
# Prefer server stopped if you are unsure about SQLite concurrent writers.
go run ./cmd/setpassword -db ./jkrt.db
# or: make setpassword
# prompts twice for the new password (or pass as first arg / stdin)
```

What this does:

1. bcrypt-hashes the new password  
2. `UPDATE users SET password_hash = ? WHERE id = 1`  

What it does **not** do:

- Invalidate existing cookies (log out on devices, or change `JKRT_SESSION_SECRET` and restart)  
- Delete learning data  

After a suspected compromise: rotate password **and** set a new `JKRT_SESSION_SECRET`, then restart.

---

## Cloudflare Tunnel (detailed)

### What this solves

JKRT runs on your laptop (or home machine). Your phone is on another network (mobile data, cafe Wi‑Fi). You want:

- HTTPS in Safari / Chrome without opening router ports  
- No public IP required  
- Traffic that only reaches `127.0.0.1:8080` on your machine  

[Cloudflare Tunnel](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/) (`cloudflared`) opens an **outbound** connection from your machine to Cloudflare. Cloudflare then serves a public HTTPS hostname that forwards to your local server. Nothing needs to listen on `0.0.0.0` on the open internet.

```
Phone  →  HTTPS  →  Cloudflare edge  →  tunnel  →  cloudflared on laptop  →  http://127.0.0.1:8080  →  JKRT
```

### Hard rule

```bash
# WRONG — public URL with open app
export JKRT_AUTH=off
cloudflared tunnel --url http://127.0.0.1:8080
```

Anyone who finds the URL can scrape, review, and read your SQLite-backed data.

**Always** start JKRT with `JKRT_AUTH=on` before any tunnel.

### Prerequisites

| Item | Notes |
|------|--------|
| JKRT running locally | Auth **on**, bound to loopback |
| [cloudflared](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/install-and-setup/installation/) | CLI connector |
| Cloudflare account | Free tier is enough |
| Domain (named tunnel only) | Domain added to Cloudflare DNS; not required for quick try |

Install `cloudflared` (examples — pick one for your OS):

```bash
# Debian/Ubuntu package (see Cloudflare docs for current package repo)
# macOS (Homebrew)
brew install cloudflared

# Or download a binary from Cloudflare’s install page and put it on PATH
cloudflared --version
```

### Step 0 — Start JKRT safely (all tunnel paths)

Terminal 1:

```bash
# From the repo root
make env                    # first time only; edit JKRT_PASSWORD in .env
# Set a real password (not change-me), keep the generated JKRT_SESSION_SECRET

make run-auth               # JKRT_AUTH=on, prefers 127.0.0.1:8080
# log: jkrt listening on 127.0.0.1:8080 (auth=true)
```

Manual equivalent:

```bash
export JKRT_AUTH=on
export JKRT_PASSWORD='your-strong-password'   # first bootstrap only
export JKRT_SESSION_SECRET="$(openssl rand -hex 32)"
export JKRT_ADDR=127.0.0.1:8080
go run ./cmd/server
```

Verify on the same machine:

```bash
curl -sS http://127.0.0.1:8080/health
# → {"status":"ok"}

curl -sI http://127.0.0.1:8080/ | head -n1
# → HTTP/1.1 302 Found  (redirect to /login when auth is on)
```

Leave this process running.

---

### Path A — Quick tunnel (temporary test URL)

**Best for:** “Does phone login work?” once.  
**Not for:** daily use (URL changes every time; random `*.trycloudflare.com` hostname).

Terminal 2:

```bash
make tunnel-quick
# or:
cloudflared tunnel --url http://127.0.0.1:8080
```

`cloudflared` prints a line like:

```text
https://random-words-here.trycloudflare.com
```

On the phone:

1. Open that `https://…` URL.  
2. You should land on `/login` (or be redirected).  
3. Enter the password you set in `.env` / `JKRT_PASSWORD`.  
4. Use dashboard, scrape, Review as usual.

Stop: `Ctrl+C` on the cloudflared process. The public URL dies with it.

---

### Path B — Named tunnel via Cloudflare dashboard (recommended daily)

**Best for:** stable hostname on your domain (e.g. `jkrt.example.com`) and phone home-screen bookmark.  
Cloudflare currently steers most people to **remotely managed** tunnels: config lives in the dashboard; the laptop only needs a **token**.

#### B1. Create the tunnel in the dashboard

1. Log in to the [Cloudflare dashboard](https://dash.cloudflare.com/).  
2. Open **Zero Trust** / **Networks** → **Tunnels** (labels move occasionally; look for **Tunnels** under Cloudflare One / Networking).  
   Direct deep link pattern: `https://dash.cloudflare.com/` → your account → **Tunnels**.  
3. **Create a tunnel**.  
4. Choose connector type **Cloudflared**.  
5. Name it something clear, e.g. `jkrt-laptop`.  
6. Cloudflare shows an install / run command that includes a long **token**.  
   - The token is a **secret**. Do not commit it to git or paste it into public chats.  
   - Example shape (do not copy this fake token):

   ```bash
   cloudflared tunnel run --token eyJhIjoi...very-long...
   ```

#### B2. Run the connector on the machine that hosts JKRT

On the **same machine** where `make run-auth` is listening:

```bash
cloudflared tunnel run --token <paste-token-from-dashboard>
```

Optional: install as a system service so the tunnel restarts on reboot (dashboard often shows OS-specific install commands; Linux may use `cloudflared service install` with the token). Only do this if you understand the token will live on that host.

Keep this process (or service) running whenever you want the phone URL to work.

#### B3. Publish a hostname → local JKRT

Still in the tunnel UI:

1. **Add a public hostname** (or “Published application”).  
2. **Subdomain** + **Domain**: e.g. `jkrt` + `example.com` → `jkrt.example.com`.  
   - Domain must already be on Cloudflare (nameservers pointed at Cloudflare).  
3. **Service / URL type**: HTTP  
4. **URL**: `http://127.0.0.1:8080`  
   - Must match `JKRT_ADDR` (loopback is correct; cloudflared runs locally and dials JKRT).  
5. Save. Cloudflare creates the DNS CNAME for you in most setups.

#### B4. Use from the phone

1. Open `https://jkrt.example.com` (your hostname).  
2. Login page → enter JKRT password.  
3. After login, the session cookie should be **Secure; HttpOnly; SameSite=Lax** (Safari Web Inspector / desktop DevTools if you debug).  

#### B5. Day-to-day workflow

```text
1. Start JKRT:        make run-auth
2. Start connector:   cloudflared tunnel run --token …   (or systemd service)
3. Phone:             https://jkrt.example.com
```

If either process stops, the phone cannot reach the app.

---

### Path C — Named tunnel via CLI + local config.yml

**Best for:** infrastructure-as-code / you want ingress rules on disk.  
Official flow: [Create a locally-managed tunnel](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/do-more-with-tunnels/local-management/create-local-tunnel/).

```bash
# 1) Authenticate (opens browser; pick the zone/domain)
cloudflared tunnel login

# 2) Create a named tunnel
cloudflared tunnel create jkrt-laptop
# → writes credentials JSON under ~/.cloudflared/<TUNNEL-UUID>.json

# 3) DNS route (hostname → tunnel)
cloudflared tunnel route dns jkrt-laptop jkrt.example.com

# 4) Config file ~/.cloudflared/config.yml
```

Example `~/.cloudflared/config.yml` (replace UUID and paths):

```yaml
tunnel: <TUNNEL-UUID>
credentials-file: /home/YOU/.cloudflared/<TUNNEL-UUID>.json

ingress:
  - hostname: jkrt.example.com
    service: http://127.0.0.1:8080
  - service: http_status:404
```

```bash
# 5) Run
cloudflared tunnel run jkrt-laptop
```

Do **not** commit credentials JSON or tokens into the JKRT git repo.

---

### Optional: Cloudflare Access (second door)

JKRT already requires a password. For a second layer (email OTP, IdP, device posture):

1. Zero Trust → **Access** → **Applications**.  
2. Add a **Self-hosted** app for `jkrt.example.com`.  
3. Policy: e.g. allow only your email.  

Then phone flow is: Access challenge → JKRT `/login` → review.  
Optional; not required if you trust a strong JKRT password and secret.

---

### Cookie and HTTPS notes

| Topic | Behaviour |
|-------|-----------|
| HTTPS termination | Cloudflare edge; laptop sees plain HTTP from cloudflared |
| `Secure` cookie | JKRT sets Secure when it sees HTTPS or `X-Forwarded-Proto: https` (Tunnel sends this) |
| `GET /health` | Public by design (`{"status":"ok"}`) — no learning data |
| Everything else | Behind auth when `JKRT_AUTH=on` |

---

### Troubleshooting

| Symptom | Things to check |
|---------|------------------|
| Phone “can’t connect” / 502 | Is `make run-auth` still running? Is cloudflared running? Is public hostname service URL exactly `http://127.0.0.1:8080`? |
| 502 but health works locally | Tunnel points at wrong host/port; or JKRT bound only to another interface |
| Redirect loop / login never sticks | Mixed content unlikely over full HTTPS; try private window; confirm Secure cookie after login |
| Immediate “unauthorized” after login | Clock skew rare; more often wrong password or multiple instances with different `JKRT_SESSION_SECRET` |
| Works on laptop URL, not phone | Phone must use the **Cloudflare HTTPS hostname**, not `http://192.168.…` (unless you intend LAN-only) |
| `JKRT_SESSION_SECRET is required` | Auth default is **on**; set secret (≥32 chars) or use `make env` / `make run-dev` for local-only |
| Easy RSS scrape fails | Expected until `JKRT_NHK_EASY_RSS_URL` is set; main feed needs network on the host |

---

### Checklist before mobile use

- [ ] `JKRT_AUTH=on`  
- [ ] User bootstrapped; password **not** the placeholder `change-me` from `.env.example`  
- [ ] `JKRT_SESSION_SECRET` is long random and **not** committed  
- [ ] Server listens on `127.0.0.1` (or only reachable by cloudflared)  
- [ ] Tunnel points at that local address only  
- [ ] Token / credentials JSON not in git  
- [ ] Optional: Cloudflare Access in front of the hostname  

---

## Related code

| Piece | Location |
|-------|----------|
| Config | `internal/config` (`JKRT_AUTH`, secret, TTL) |
| Session sign/parse | `internal/auth/session.go` |
| Password hash / rotate | `internal/auth/password.go`, `SetPassword`, `cmd/setpassword` |
| Cookie flags on login | `internal/http/handlers.go` (`handleLoginPost`) |
| Middleware | `requireAuth` — 302 HTML / 401 API |
| Makefile helpers | `run-auth`, `tunnel-quick`, `env`, `setpassword` |
