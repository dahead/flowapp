**Todo:**

**Konkrete Lücken im Code**

1. **CSRF fehlt komplett** — alle POST-Formulare (Step abschließen, User löschen, Passwort ändern) sind ohne CSRF-Token. Ein eingebettetes Bild auf einer fremden Seite kann im Hintergrund beliebige POST-Requests im Namen eines eingeloggten Users abschicken. SameSite=Lax schützt teilweise, aber nicht vollständig.

2. **Gate-Token in der URL** — `/approve/abc123` taucht in Server-Logs, Browser-History und Referer-Headern auf. Besser wäre POST-only Einlösung oder zumindest ein kurzes TTL das bereits gesetzt ist.

3. **Session-Secret ephemeral** — bei jedem Neustart werden alle Sessions ungültig. Für Prod sollte `SESSION_SECRET` als Env-Var gesetzt sein, sonst muss sich jeder nach jedem Deployment neu einloggen.

4. **Keine Passwort-Komplexität** — nur "mindestens 8 Zeichen" wird geprüft. Triviale Passwörter wie `12345678` sind erlaubt.

5. **Kein Brute-Force-Schutz auf `/approve`** — der Login hat RateLimiting, aber Gate-Tokens können unbegrenzt schnell ausprobiert werden. Bei 48 Hex-Zeichen ist das kein echtes Risiko, aber konsequent wäre es trotzdem.

6. **Viewer-Rolle kann Gate-Links einlösen** — `/approve/{token}` hat keine Auth, was by design ist. Aber auch eingeloggte Viewer könnten theoretisch den Link verwenden wenn sie ihn kennen.

7. **`data/` Verzeichnispermissions** — Instanz-JSON-Dateien werden mit `0644` gespeichert, sind also für alle lokalen User lesbar. Enthält potenziell sensitive Workflow-Daten.

8. **Kein Rate-Limit auf Kommentare/Items** — ein eingeloggter User kann tausende Kommentare oder Checklist-Items in Sekunden anlegen und die JSON-Dateien aufblähen.

9. **Flash-Cookie unverschlüsselt** — Fehlermeldungen landen als Klartext im Cookie. Kein echtes Sicherheitsproblem, aber könnte bei langen Fehlermeldungen mit internen Details unschön sein.

---

Soll ich eines oder mehrere davon implementieren?

**Done:**

**Bugs / Logikfehler**

1. **`runScheduler` injiziert nicht** — Der Scheduler ruft `inst.TickScheduled()` auf, aber `inject(inst)` fehlt davor. Scheduled steps lösen also keine E-Mails aus.

2. **`CreateInstance` injiziert nicht** — `engine.NewInstance` ruft `activateReady` im Konstruktor auf. Steps die sofort aktiv werden (kein `needs`, kein `schedule`) feuern Notifications beim Erstellen, aber ohne Mailer, weil `inject` erst danach kommt. Für Gate-Steps mit sofortiger Aktivierung geht die Approve-Mail verloren.

3. **`ApplyVars` injiziert nicht** — Vars können `Notify`/`Assign` befüllen (z.B. `$EMAIL`). Aber wenn `ApplyVars` danach `activateReady` indirekt triggert (es tut's aktuell nicht, aber es substituiert die Felder), ist der Mailer trotzdem nicht gesetzt. Kein direkter Bug heute, aber fragil.

4. **Approval-Link in `fireNotify` ist kaputt** — Die URL wird zusammengebaut als `inst.WorkflowName + "/approve/" + token` statt nur `/approve/token`. Der Empfänger bekommt einen falschen Link wie `Onboarding/approve/abc123`.

5. **Gate-Step ohne `ask` kann nicht eingelöst werden** — `RedeemGate` prüft `found.Ask == nil` und gibt dann Fehler zurück. Ein Gate-Step der keine `ask`-Buttons hat (nur Bestätigung, kein Routing) ist damit komplett blockiert. Sollte bei `Ask == nil` mit `chosenIdx=0` durchlassen oder einfach completen.

6. **`ChosenIdx = 0` ist mehrdeutig** — `ChosenIdx int` mit JSON default `0` bedeutet: nach dem Laden aus JSON kann man nicht unterscheiden ob "erste Option gewählt" oder "noch keine Wahl getroffen". Ein `*int` oder ein `-1`-Sentinel wäre sicherer.

---

**Mailer-spezifisch**

7. **`From`-Header fehlt in ausgehenden Mails** — `MailMessage` hat ein `From`-Feld, aber `fireNotify`/`fireAssignNotify` setzen es nie. Der SMTP-Mailer schreibt dann `From: ` leer, was viele Mailserver ablehnen. Die `cfg.From` muss irgendwie durchgereicht werden.

8. **Graph-Mailer fetcht Token bei jedem Send** — Pro E-Mail ein HTTP-Request an den Token-Endpoint. Token sind 1h gültig, sollten gecacht werden.

9. **`buildMIME` (SMTP) schreibt `Content-Type: multipart/mixed` doppelt** — Im Attachment-Pfad wird der Header zweimal geschrieben (einmal direkt, einmal vom Multipart-Writer). Das erzeugt invalides MIME.

---

**Architektur / Robustheit**

10. **Notifications blockieren den Lock** — `fireNotify` wird unter dem Store-Mutex aufgerufen (in `AdvanceStep` → `inst.AdvanceStep` → `completeStep` → `fireNotify`). Ein langsamer SMTP-Server friert die ganze App ein. Sollte per `go fireNotify(...)` oder in eine Queue.

11. **Instance-ID ist `UnixNano` als String** — Kollisionen bei sehr schnellen Requests (Tests, Klonen) sind theoretisch möglich. Besser eine echte UUID/random-ID wie bei User-IDs.

12. **`loadInstances` überspringt `users.json` per Hardcode** — Funktioniert nur wenn `dataDir == setupDir`. Wenn beides separat liegt, kein Problem — aber der Check ist fragil.