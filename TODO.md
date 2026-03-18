# FlowApp — TODO / Roadmap

User Dashboard with notifications



**Broken — muss sofort repariert werden**

- `approve.html`, `login.html`, `error.html`, `setup.html`, `archive.html`, `admin.html`, `instance.html`, `admin_mail.html`, `profile.html`, `builder.html` — alle haben noch `{{template "head-base" .}}` und/oder `{{template "menu-js" .}}` vom gescheiterten Partials-Refactor. Du hast gesagt der Rollback hat funktioniert — aber diese Templates sind offenbar **nicht** auf die alte Version zurückgesetzt worden. Ich vermute dass `board.html` funktioniert weil du es manuell repariert hast, aber die anderen Templates noch den kaputten Stand haben. Das `partials.html` definiert `head-base` und `menu-js` jetzt zwar wieder — aber nur weil ich sie neu hinzugefügt habe. Das ist ein Zufallstreffer.

**Totes Code — muss weg**

- `fileNames map[string]string` in `store.go` — deklariert, nie initialisiert, nie befüllt, nie benutzt. Hängt von der halbfertigen "Workflow löschen"-Feature-Session.

**Inkonsistent — sollte vereinheitlicht werden**

- `adapter.go` (27 Zeilen) und `mailer.go` (19 Zeilen) — beide winzig, beide könnten in `config.go` oder `smtp.go` stehen
- `csrf.go` (76 Zeilen) — könnte in `session.go`, da beide das `sessionSecret` nutzen
- Anonyme Structs für `archive`, `builder`, `profile`, `new_instance` — sollten benannte Structs sein wie alle anderen
- `getFlash` / `getFlashOK` — zwei verschiedene Cookie-Namen für Flash, inkonsistent; `admin.html` zeigt Fehler-Flash als `flash-error`-CSS aber der Inhalt kann auch Erfolg sein
- `mailAdminData` ohne `CurrentUser` in Fehler-Render-Pfaden — `adminMailSave` und `adminMailTest` übergeben `CurrentUser` nur teilweise

**Architektur-Frage**

- `store.go` importiert jetzt `mailer` — das bedeutet Store kennt das Mailer-Package. Geht, aber es wäre sauberer wenn `main.go` die Konfiguration hält und nur den fertigen `EngineAdapter` an den Store gibt. `SaveMailConfig` gehört eigentlich nicht in den Store.

---

Was soll ich zuerst angehen — die kaputten Templates oder den toten Code und die Inkonsistenzen?




**Sollte raus / korrigiert werden**

- `new_instance.html` hat das Theme-CSS inline hardcoded statt `theme-init.js` zu nutzen — Dark Mode funktioniert dort inkonsistent
- Die Admin-Mail-Seite hat keinen "active"-Link-Zustand im Menü wenn man sich auf `/admin/mail` befindet (alle anderen Seiten markieren den aktiven Link)
- Das `assign`-Feld in der Filterzeile heißt "Meine Aufgaben" aber ist auf Deutsch in einer sonst englischen UI

**Kleinere Verbesserungen**

- Instanz-Detail hat keinen direkten Link zur nächsten/vorherigen Instanz — man muss immer zurück aufs Board
- Gate-Approval-Seite zeigt den Workflow-Namen und Schritt, aber nicht die Instanz-Beschreibung/Note

- Der Builder hat keinen Live-Preview des generierten `.workflow`-Codes während man baut

**Neue Features die Sinn machen**

- **Due-Date-Reminder** per Mail kurz vor Ablauf (z.B. 24h vorher) — `OverdueNotified` ist schon da, ein `DueSoonNotified`-Flag wäre analog
- **Instanz-Suche über alle Felder** — aktuell nur Titel + Workflow-Name, aber nicht Step-Namen oder Kommentare
- **Wiederkehrende Workflows** — eine Instanz die sich nach Abschluss selbst neu startet (z.B. monatlicher Check)


- [ ] **Password complexity** — enforce a minimum policy beyond 8 characters (e.g. at least one non-letter character).

- [ ] **Overdue repeat notifications** — currently an overdue step notifies once (on first detection by the scheduler). Add a configurable repeat interval (e.g. every 24h) for steps that remain overdue.

- [ ] **Workflow definition management** — admin-only page to list, rename, and delete `.workflow` files from disk via the UI. Currently requires manual file operations; the hot-reload watcher picks up changes automatically.
