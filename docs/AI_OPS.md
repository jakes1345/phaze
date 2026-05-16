# AI-driven operations loop

Phaze ships with two scheduled GitHub Actions that run an LLM agent
against the repo + the production server:

| Workflow | Cron | What it does |
|----------|------|--------------|
| `ai-maintenance.yml` | daily 06:13 UTC | Reviews Dependabot PRs, kills dead code, fixes doc drift, opens PRs labeled `ai-maintenance` for human merge. |
| `ai-triage.yml` | every 30 min | Pulls pending abuse reports from `/api/v1/admin/reports`, classifies each, auto-resolves only the unambiguous false positives, opens a single GitHub issue summarising everything that needs human review. |

Dependabot (`.github/dependabot.yml`) opens dependency-bump PRs every
day. The maintenance workflow reviews and comments on them; you merge.

## What the loop will NEVER do without a human

- **Ban a user.** The triage script generates a ready-to-run `curl` for
  you in the escalation issue, but it never POSTs to `/ban` itself.
- **Push to `master` directly.** Maintenance always opens a PR.
- **Touch crypto, auth, or DB schema.** The maintenance prompt
  explicitly forbids it; the LLM flags those instead of editing.

## Required GitHub Secrets

Set these once in **Repo → Settings → Secrets and variables → Actions**:

| Secret | Value | Used by |
|--------|-------|---------|
| `ANTHROPIC_API_KEY` | Key from <https://console.anthropic.com> | Both workflows |
| `NEXUS_BASE_URL` | `https://phazechat.world` (or your relay) | Triage |
| `NEXUS_ADMIN_TOKEN` | A long-lived admin session token (see below) | Triage |

### Minting the admin bearer

1. Make sure your username is in the server's `PHAZE_ADMIN_USERS`
   secret. If not, set it and redeploy nexus.
2. Log into the web client. Open DevTools → Application →
   Local Storage → copy the value of `phaze_session_token_v1`.
3. Paste it into the `NEXUS_ADMIN_TOKEN` GitHub secret.

Tokens are 30-day TTL on the server. Re-mint and paste a new one
when the workflow starts 401-ing.

## Cost ceiling

- **Maintenance**: ~1 run/day × ~$0.30 worst case ≈ **$10/month**.
- **Triage**: 48 runs/day × ~5 reports/run × ~$0.001/classification
  ≈ **$8/month** at moderate report volume; near $0 when the queue is
  empty (the script bails out before any API call).

The triage script caps at `MAX_REPORTS_PER_RUN = 50` per invocation
so a report flood can't bankrupt the API budget in a single run.

## Disabling

Either workflow can be paused without code changes:
**Actions tab → workflow → ⋯ menu → Disable workflow.**

Dependabot can be disabled by deleting `.github/dependabot.yml`.

## What to do when triage flags something serious

The escalation issue's table includes the report ID, the AI's category
guess, and a `curl` block. Workflow:

1. Open the issue, read the AI's reasoning column.
2. If the report is genuine: copy the **ban** `curl`, fill in the
   reason, run it. Then resolve the report with the **resolve** `curl`.
3. If the AI was wrong: just resolve the report. Close the issue.
4. **Always** close the issue when done so the next run doesn't duplicate.

The `ai-flagged` label on a PR or issue means the LLM noticed
something it wasn't allowed to fix itself (security-adjacent code,
suspicious recent diffs). Treat those as priority — that's the AI
declining to act on something it didn't trust itself with.
