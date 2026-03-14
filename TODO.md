# FlowApp — TODO / Roadmap

- [ ] **Password complexity** — enforce a minimum policy beyond 8 characters (e.g. at least one non-letter character).

- [ ] **Overdue repeat notifications** — currently an overdue step notifies once (on first detection by the scheduler). Add a configurable repeat interval (e.g. every 24h) for steps that remain overdue.

- [ ] **Workflow definition management** — admin-only page to list, rename, and delete `.workflow` files from disk via the UI. Currently requires manual file operations; the hot-reload watcher picks up changes automatically.
