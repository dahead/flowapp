# FlowApp v2

A lightweight workflow management app written in Go. Define processes as `.workflow` files, run instances, track parallel tasks, and collect external approvals via tokenized links.

## Quick Start

```bash
go run ./cmd/server
# → http://localhost:8080
```

## DSL Reference

Workflow files live in `workflows/`. Hot-reloaded on save.

```
workflow "Name"
priority high          # low | medium | high (default: medium)
label finance          # multiple labels allowed

section "Name"
  step "Name"
    note "Hint text shown on the step"
    due 2d             # 2h | 3d | 1w — deadline starts when step becomes ready
    notify "email"     # logged to notifications.log on completion
    needs "Step A", "Step B"   # AND-join: all must be done first
    list "Item text"   # checklist item (default: required)
    list "Item text" optional
    ask "Question?" -> "Option A", "Option B", "Option C"
    gate               # step waits for external click via token link
    ends               # terminal step — this path ends here
```

### Keywords

| Keyword | Scope | Description |
|---|---|---|
| `workflow` | top | Workflow name |
| `priority` | top | low / medium / high |
| `label` | top | Tag for filtering (multiple allowed) |
| `section` | top | Visual group — no runtime semantics |
| `step` | section | A task. Name must be unique within the workflow |
| `note` | step | Description text |
| `due` | step | Relative deadline from when step becomes ready |
| `notify` | step | Writes to notifications.log; gate steps include approval link |
| `needs` | step | Comma-separated step names — all must be done/skipped |
| `list` | step | Checklist item; `required` (default) blocks completion |
| `ask` | step | Presents N buttons; routes to chosen target step |
| `gate` | step | Waits for external approval via `/approve/{token}` |
| `ends` | step | Marks this path as terminated |

### Flow Rules

- Steps with no `needs` start immediately when the instance is created
- `needs "A", "B"` = AND-join: waits for **all** listed steps
- `ask "?" -> "X", "Y"` = XOR-split: chosen target activates, others are skipped
- `gate` generates a one-time token; combine with `notify` to send approval link by email
- Cross-section `needs` are fully supported

### Example: Parallel + Join

```
workflow "Parallel Demo"

section "Parallel Work"
  step "Start"

  step "Task A"
    needs "Start"

  step "Task B"
    needs "Start"

  step "Review"
    needs "Task A", "Task B"   # waits for both
```

### External Approvals (gate)

```
step "Manager Approval"
  ask "Approve?" -> "Proceed", "Reject"
  gate
  notify "manager@company.com"
```

When this step becomes ready:
1. A one-time token is generated
2. `notifications.log` receives: `approval link: /approve/{token}`
3. The recipient opens the link — no login required
4. They click a button; the workflow continues

## HTTP Routes

| Method | Path | Description |
|---|---|---|
| GET | / | Kanban board |
| GET | /builder | Visual workflow builder |
| GET | /instance/{id} | Instance detail |
| POST | /instance | Create new instance |
| POST | /instance/{id}/edit | Update title/priority/labels |
| POST | /instance/{id}/step | Complete a ready step |
| POST | /instance/{id}/ask | Answer an ask step |
| POST | /instance/{id}/clone | Clone instance |
| POST | /instance/{id}/delete | Delete instance |
| POST | /instance/{id}/comment | Add comment |
| POST | /instance/{id}/listitem/toggle | Toggle checklist item |
| POST | /instance/{id}/listitem/add | Add dynamic checklist item |
| POST | /instance/{id}/listitem/checkall | Check all items |
| GET | /approve/{token} | External approval page |
| POST | /approve/{token} | Submit external approval |

## Included Workflows

| File | Labels | Description |
|---|---|---|
| invoice.workflow | finance | Order → ship → invoice → payment |
| onboarding.workflow | hr | Parallel HR/IT/Office/Buddy tracks with gate approvals |
| offboarding.workflow | hr | Exit interview, payroll, IT revocation, asset return |
| reisekostenabrechnung.workflow | finance | Expense report submission and approval |
| vertragsmanagement.workflow | finance, legal | Contract drafting → review → sign |
| product-launch.workflow | product | Design → build → pilot → launch approval |
| shopping.workflow | shopping | Simple shopping list workflow |

## Step Statuses

| Status | Meaning |
|---|---|
| `pending` | Needs not yet satisfied |
| `ready` | Can be completed |
| `ask` | Waiting for UI choice |
| `gate` | Waiting for external token redemption |
| `done` | Completed |
| `skipped` | Bypassed by ask routing |
| `ended` | Terminal step reached |

## Auth Setup

Before first run after adding auth, update dependencies:
```bash
go get golang.org/x/crypto@v0.21.0
go mod tidy
```

On first start with no `data/users.json`, FlowApp redirects to `/setup` to create the admin account.
