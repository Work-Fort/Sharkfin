# Passport Onboarding Flow — Design

## Goal

First-run experience for WorkFort: when a user opens the shell for the first time (no users in Passport), they see a setup form to create the initial admin account. After setup, subsequent visits show a sign-in form when unauthenticated.

## Architecture

Passport stays API-only — no frontend. The shell owns all UI. Passport exposes a `setup_mode` flag in its health endpoint so the shell knows which flow to show.

---

## Setup Detection

- Passport's `GET /ui/health` includes `"setup_mode": true` when the user table is empty. When users exist, the field is omitted.
- The BFF's service tracker reads this from the health probe and includes it in the `GET /api/services` response.
- The shell checks the `auth` service for `setup_mode` — if present and `true`, renders the setup flow instead of the normal fort view.
- The check is a `SELECT COUNT(*) FROM user LIMIT 1` on each health probe (runs every 10s via the tracker's polling interval).

---

## Setup Flow (First Run)

1. User opens the BFF (e.g., `http://localhost:16100`)
2. BFF probes Passport at the configured URL, gets `setup_mode: true`
3. Shell renders a setup form with fields: **email**, **username**, **password**, **confirm password**
4. On submit, shell calls `POST /forts/{fort}/api/auth/v1/sign-up/email` through the BFF's auth proxy
5. Passport creates the admin user (first user gets admin role automatically) and returns a session
6. BFF's auth proxy rewrites the `Set-Cookie` header with the fort-scoped path
7. Browser now has the session cookie
8. Shell re-fetches `/api/services` — `setup_mode` is gone — transitions to normal fort view
9. User is authenticated

---

## Sign-In Flow (Returning Users)

1. User opens the shell, BFF has no session cookie (or it expired)
2. Shell detects 401 responses on service API calls
3. Shell shows a sign-in form: **email**, **password**
4. On submit, calls `POST /forts/{fort}/api/auth/v1/sign-in/email` through the BFF auth proxy
5. Passport validates credentials, returns session
6. BFF sets the fort-scoped cookie
7. Shell reloads — services are now accessible

---

## Security

- **Setup mode locks after first user.** Once any user exists, the sign-up endpoint follows normal auth rules (admin-only user creation via Better Auth's `admin` plugin). The open sign-up is only available when `setup_mode: true`.
- **Passport enforces this server-side.** The shell hiding the setup form is cosmetic — Passport must reject unauthenticated sign-ups when users already exist.
- **No tokens in URLs.** Sessions are established via `Set-Cookie` on the sign-up/sign-in response.

---

## Changes By Repo

### Passport
- Add `setup_mode: true` to `/ui/health` response when user table is empty
- Add server-side guard on sign-up: allow unauthenticated registration only when no users exist
- First user created gets admin role automatically

### Scope (BFF + Service Tracker)
- Pass `setup_mode` through in the service tracker's service info response
- No new endpoints — existing auth proxy handles sign-up and sign-in

### Scope (Shell)
- Add setup view: email, username, password, confirm password form
- Add sign-in view: email, password form
- Route logic: `setup_mode` → setup view; 401 on service calls → sign-in view; otherwise → normal fort view

### Sharkfin
- Remove infinite skeleton dead-end when `initApp()` fails
- Handle `connected` prop properly — retry when it flips to `true`
