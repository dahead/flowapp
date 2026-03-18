# FlowApp

A lightweight workflow management app written in Go. Define processes as `.workflow` files, run instances, track parallel tasks, collect external approvals via tokenized links, and get notified by email.

## Overview

![FlowApp screenshot main page overview](/screenshots/overview-dark-v2.png)

New workflow:

![FlowApp new workflow](/screenshots/new-workflow-assistant-v2.png)

Workflow in detail:

![FlowApp workflow](/screenshots/workflow-instance-v2.png)

User menu:

![FlowApp workflow with user menu](/screenshots/workflow-instance-menu-v2.png)

Users see only their own workflows:

![FlowApp screenshot](/screenshots/workflow-instances-different-user-v2.png)

Workflow builder:

![FlowApp screenshot](/screenshots/builder-loaded-v2.png)

Administration User management:

![FlowApp screenshot](/screenshots/user-management-v2.png)

User login:

![FlowApp screenshot](/screenshots/login-v2.png)

Filter / search:

![FlowApp screenshot search](/screenshots/overview-dark-search-v2.png)



## Quick Start

```bash
go run ./cmd/server
# → http://localhost:8080
```

On first start FlowApp creates `data/` and `config/` automatically and redirects to `/setup` to create the first admin account.

## Directory Layout

```
data/
  instances/       runtime workflow instances (one JSON per instance)
  notifications/   in-app notifications (one JSON per user)
  users.json       user accounts
  session-secret   HMAC key for session cookies (auto-generated)
config/
  mail.json        mail backend configuration (optional)
workflows/
  *.workflow       workflow definitions (hot-reloaded on save)
```

Everything needed to back up or migrate FlowApp is in `data/` and `config/`.

## Command Line Flags

```
./flowapp [flags]

  --port       int     HTTP listen port (default: 8080)
  --data       string  Directory for all runtime data — instances, users, notifications, session secret (default: "data")
  --config     string  Directory for configuration files — mail.json (default: "config")
  --workflows  string  Directory for .workflow definition files (default: "workflows")
```

Example:

```bash
./flowapp --port 9090 --data /var/flowapp/data --config /etc/flowapp/config --workflows /etc/flowapp/workflows
```

## Session Secret

FlowApp generates a random session secret on first start and persists it to `data/session-secret` so sessions survive restarts.

Override via environment variable (takes highest priority):

```bash
SESSION_SECRET=your-32-byte-secret ./flowapp
```

## Notifications

FlowApp delivers notifications in two ways: **in-app** (bell icon, 🔔) and **email**. Both use the same target expressions in the workflow DSL.

### Notification targets

| Expression | In-app | Email |
|---|---|---|
| `role:finance` | All active users with App Role `finance` | Their email addresses |
| `user:anna` or `user:anna@example.com` | That specific user | Their email address |
| `anna@example.com` (bare email) | Only if a user with that address exists | Always sent |
| Admins | Always receive a copy of every notification | — |

Both `assign` and `notify` trigger in-app notifications. `assign` additionally restricts who may complete the step. In-app notification fan-out works independently of any mail configuration.

### In-app notifications

All matched users see the notification under the 🔔 bell icon. The unread count is shown in the user menu. Notifications are stored in `data/notifications/{userID}.json`.

### Email notifications

Create `config/mail.json` to enable email delivery (or configure via Admin → Mail):

**SMTP:**
```json
{
  "type": "smtp",
  "from": "flowapp@example.com",
  "smtp_host": "mailrelay.example.com",
  "smtp_port": 587,
  "smtp_username": "user",
  "smtp_password": "secret"
}
```

**Microsoft Graph (Office 365):**
```json
{
  "type": "graph",
  "from": "flowapp@example.com",
  "graph_tenant_id": "...",
  "graph_client_id": "...",
  "graph_client_secret": "...",
  "graph_sender_upn": "flowapp@example.com"
}
```

Without a mail config, notifications are in-app only and written to the server log.

## DSL Reference

Workflow files live in `workflows/`. They are hot-reloaded on save — no restart needed.

```
workflow "Name"
priority high          # low | medium | high (default: medium)
label finance          # multiple labels allowed
allowed_roles role:hr role:finance  # who may start this workflow (empty = all users with write access)
var "Employee Name"    # prompts for a value when creating an instance ($Employee Name)

section "Name"
  step "Name"
    note "Hint text shown on the step"
    due 2d             # 2h | 3d | 1w — deadline starts when step becomes ready
    assign "user:anna"          # assign to a specific user (by name or email)
    assign "role:finance"       # assign to all users with this app role
    notify "role:finance"         # notify all users with this app role
    notify "manager@example.com"   # or a bare email address
    needs "Step A", "Step B"    # AND-join: all must be done first
    schedule +3d        # activate 3 days after instance creation (also: +2w, +4h, 2025-12-01)
    item "Required item"        # checklist item — blocks completion until checked
    item "Optional item" optional
    ask "Question?" -> "Option A", "Option B"
    gate                # waits for external approval via token link
    ends                # terminal step — this path ends here
```

### Keywords

| Keyword | Scope | Description |
|---|---|---|
| `workflow` | top | Workflow name |
| `priority` | top | `low` / `medium` / `high` |
| `label` | top | Tag for filtering (multiple allowed) |
| `allowed_roles` | top | Roles permitted to start this workflow, e.g. `role:hr role:finance`. Empty = all users with create-instance permission. Admins and managers always bypass this. |
| `var` | top | Variable name — prompted at instance creation, substituted as `$Name` |
| `section` | top | Visual group — no runtime effect |
| `step` | section | A task. Name must be unique within the workflow |
| `note` | step | Description text shown on the step card |
| `due` | step | Relative deadline from when step becomes ready |
| `assign` | step | Assign to `user:<name/email>` or `role:<rolename>` — restricts who can complete the step, and sends an in-app notification + email to the assigned user(s) |
| `notify` | step | Who to notify when the step fires — `role:<name>`, `user:<name/email>`, or a bare email. Gate steps include the approval link. Multiple `notify` lines allowed. |
| `needs` | step | Comma-separated step names — all must be done/skipped before this activates |
| `schedule` | step | Delay activation: relative (`+3d`, `+2w`, `+4h`) or absolute (`2025-12-01`) |
| `item` | step | Checklist entry; `optional` makes it non-blocking (required by default) |
| `ask` | step | Presents N buttons; routes to the chosen target step, skips the rest |
| `gate` | step | Waits for external approval via `/approve/{token}` — no login required |
| `ends` | step | Marks this path as terminated — instance completes when all paths end |

### Flow Rules

- Steps with no `needs` start immediately when the instance is created
- `needs "A", "B"` = AND-join: waits for **all** listed steps
- `ask "?" -> "X", "Y"` = XOR-split: chosen target activates, others are skipped
- `gate` generates a one-time token; combine with `notify` to send the approval link by email
- `gate` + `ask` = external routing decision (e.g. Approve / Reject)
- `schedule` delays activation even if all `needs` are already satisfied
- Cross-section `needs` are fully supported

### Variables

```
workflow "Onboarding"
var "Employee Name"
var "Start Date"

section "Preparation"
  step "Prepare workspace for $Employee Name"
    note "Start date: $Start Date"
```

Variable values are entered when creating a new instance and substituted throughout.

### External Approvals (gate)

```
step "Manager Approval"
  ask "Approve request?" -> "Approved", "Rejected"
  gate
  notify "role:management"
```

When this step becomes ready:
1. A one-time token is generated
2. All matched users receive the approval link via in-app notification and email
3. The recipient opens `/approve/{token}` — no login required
4. They click a button; the workflow continues on the chosen path

Simple gate without routing (just a confirmation):

```
step "Final Sign-off"
  gate
  notify "role:management"
```

## User Roles

| Role | See instances | Complete steps | Archive | Clone | Delete | Admin |
|---|---|---|---|---|---|---|
| Viewer | assigned only | – | – | – | – | – |
| User | assigned only | ✓ (own steps) | ✓ | – | – | – |
| Manager | all | ✓ | ✓ | ✓ | ✓ | – |
| Admin | all | ✓ | ✓ | ✓ | ✓ | ✓ |

**App Roles** (e.g. `hr`, `finance`) are separate from the site role. They control two things:

1. **Visibility** — a User only sees instances where their app role (or a direct `user:` expression) appears in at least one `assign` field of the workflow.
2. **Step completion** — a User may only complete a step if their app role or direct user expression matches the step's `assign` field.

**Starting workflows** — Admins and Managers can always start any workflow. A `User` can only start a workflow if their app role matches at least one entry in the workflow's `allowed_roles` list (or if `allowed_roles` is empty).

## Kanban Board

Instances are displayed in three columns: **Todo**, **In Progress**, **Done**.

**Filters:**
- Free text search (title + workflow name)
- Priority: `low` / `medium` / `high`
- Labels
- Due: `overdue` / `today` / `7d`
- Created: `today` / `7d` / `30d`
- Assign: `me` (steps assigned to the current user)

**Sort:** position (drag & drop) / updated / priority / created

## HTTP Routes

| Method | Path | Auth | Description |
|---|---|---|---|
| GET | `/` | user+ | Kanban board |
| GET | `/builder` | user+ | Visual workflow builder |
| GET | `/archive` | user+ | Archived instances |
| GET | `/instance/{id}` | user+ | Instance detail |
| POST | `/instance` | user+ | Create new instance |
| POST | `/instance/{id}/edit` | user+ | Update title/priority/labels |
| POST | `/instance/{id}/step` | user+ | Complete a ready step |
| POST | `/instance/{id}/ask` | user+ | Answer an ask step |
| POST | `/instance/{id}/archive` | user+ | Archive instance |
| POST | `/instance/{id}/clone` | manager+ | Clone instance |
| POST | `/instance/{id}/delete` | manager+ | Delete instance |
| POST | `/instance/{id}/comment` | user+ | Add comment |
| POST | `/instance/{id}/stepcomment` | user+ | Add step comment |
| POST | `/instance/{id}/listitem/toggle` | user+ | Toggle checklist item |
| POST | `/instance/{id}/listitem/add` | user+ | Add dynamic checklist item |
| POST | `/instance/{id}/listitem/checkall` | user+ | Check all items |
| POST | `/reorder` | user+ | Drag & drop reorder |
| GET | `/notifications` | user+ | In-app notification list |
| POST | `/notifications/mark` | user+ | Mark notification read/unread |
| GET | `/approve/{token}` | none | External approval page |
| POST | `/approve/{token}` | none | Submit external approval |
| GET | `/api/workflows` | none | JSON list of workflow definitions |
| GET | `/admin/users` | admin | User management |

## Step Statuses

| Status | Meaning |
|---|---|
| `pending` | Waiting for needs or schedule |
| `ready` | Can be completed |
| `ask` | Waiting for a routing decision |
| `gate` | Waiting for external token redemption |
| `done` | Completed |
| `skipped` | Bypassed by ask routing |
| `ended` | Terminal step reached |

## Security

- Session cookies: HMAC-SHA256 signed, HTTP-only, SameSite=Lax, 7-day TTL
- CSRF protection: session-bound tokens on all state-changing POST requests
- Login rate limiting: 5 attempts per 15 minutes, 30-minute lockout
- Gate approval pages check that logged-in Viewers cannot approve steps
- Session secret persisted across restarts (or set via `SESSION_SECRET` env var)

## Included Workflows

| File | Labels | Description |
|---|---|---|
| `invoice.workflow` | finance | Order → ship → invoice → payment |
| `onboarding.workflow` | hr | Parallel HR/IT/Office/Buddy tracks with gate approvals |
| `offboarding.workflow` | hr | Exit interview, payroll, IT revocation, asset return |
| `reisekostenabrechnung.workflow` | finance | Expense report submission and approval |
| `vertragsmanagement.workflow` | finance, legal | Contract drafting → review → sign |
| `product-launch.workflow` | product | Design → build → pilot → launch approval |
| `krankmeldung.workflow` | hr | Sick leave notification flow |
| `urlaub-beantragen.workflow` | hr | Holiday request with approval gate |
| `gehaltsverhandlung.workflow` | hr | Salary review process |
| `kontowechsel.workflow` | finance | Bank account change request |
| `shopping.workflow` | — | Shopping list with parallel tracks |
| `shopping-simple.workflow` | — | Simple linear shopping list |
| `parallel-demo.workflow` | — | Parallel + join pattern demo |
| `demo-schedule-assign.workflow` | — | Schedule and assign feature demo |
