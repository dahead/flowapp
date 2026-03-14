# FlowApp — TODO / Roadmap

## Mail

- [ ] **Mail config in Admin UI** — currently `~/.config/flowapp/mail-config.json` must be created manually. An admin page at `/admin/mail` should allow setting SMTP/Graph credentials in the browser and test-sending a message.

- [ ] **Overdue notifications** — the background scheduler already ticks every minute and activates scheduled steps. It should also detect steps that have passed their `due` date and send a reminder email to the assigned user or notify address. Configurable: once on breach, or repeat every N hours.

## Other

- [ ] **Graceful shutdown** — on `SIGTERM`/`SIGINT`, wait for any in-flight saves to complete before exiting to prevent corrupt JSON files.

- [ ] **Workflow definition delete via UI** — admin-only page to remove a `.workflow` file from disk (currently requires manual file deletion; the watcher hot-reloads the removal automatically).

- [ ] **Password complexity** — enforce a minimum policy beyond 8 characters (e.g. at least one non-letter character).

- [ ] **Comment timestamps in UI** — comments store `created_at` but the instance detail template does not display it.
