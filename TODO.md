# FlowApp — TODO / Roadmap


der admin soll auf der user verwaltung ebenfalls die unread notifications pro user sehen.
    so kann ich einfacher debuggen.



**Kleinere Verbesserungen**

- Gate-Approval-Seite zeigt den Workflow-Namen und Schritt, aber nicht die Instanz-Beschreibung/Note

- Der Builder hat keinen Live-Preview des generierten `.workflow`-Codes während man baut

**Neue Features die Sinn machen**

- **Due-Date-Reminder** per Mail kurz vor Ablauf (z.B. 24h vorher) — `OverdueNotified` ist schon da, ein `DueSoonNotified`-Flag wäre analog
- **Instanz-Suche über alle Felder** — aktuell nur Titel + Workflow-Name, aber nicht Step-Namen oder Kommentare
- **Wiederkehrende Workflows** — eine Instanz die sich nach Abschluss selbst neu startet (z.B. monatlicher Check)


- [ ] **Password complexity** — enforce a minimum policy beyond 8 characters (e.g. at least one non-letter character).

- [ ] **Overdue repeat notifications** — currently an overdue step notifies once (on first detection by the scheduler). Add a configurable repeat interval (e.g. every 24h) for steps that remain overdue.

- [ ] **Workflow definition management** — admin-only page to list, rename, and delete `.workflow` files from disk via the UI. Currently requires manual file operations; the hot-reload watcher picks up changes automatically.
