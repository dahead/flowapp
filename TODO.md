# FlowApp — TODO / Roadmap

Tasks Seite:

- **Flache Liste** aller offenen Schritte, die dem aktuellen User zugeordnet sind — quer über alle Instanzen
- **Kontext direkt sichtbar**: Workflow-Name, Instanz-Titel, Section (HR/IT als farbige Badge)
- **Überfällige Tasks** oben, rot abgesetzt
- **Gate-Steps** direkt actionable in der Liste — die Ja/Nein-Buttons erscheinen inline, kein Klick zur Instanz nötig
- **Normale Steps** lassen sich mit einem Check-Button abhaken

Technisch würde ich das so umsetzen:

1. **Neuer Handler** `GET /tasks` — iteriert alle Instanzen, sammelt per `userCanDoStep()` alle relevanten aktiven Steps des aktuellen Users
2. **Neues Template** `tasks.html`
3. **POST-Actions** können dieselben bestehenden Endpoints nutzen (`/instance/{id}/step/...`)

Gefällt dir der Ansatz? Und: soll ich den Code direkt implementieren, oder gibt es Anpassungswünsche am Design/Verhalten?




Fehler:
    - Properties Box bei Archiven laesst Aenderungen zu (In Feldern und via Save Button)
    - Properties Box bei Archiven sieht komisch aus (TExtfelder sind weiss. Schrift ausser "Properties" sieht komisch aus)
    - Archivierte Instances lassen sich erneut archivieren


Sprache:

kannst du noch alles auf default englisch machen? es gibt recht viele deutsche begriffe in menues und so weiter.

baue das ein:

go-i18n

Vollwertige i18n-Lösung

Unterstützt:
Pluralformen
Variablen

JSON/TOML Übersetzungen

👉 Beispielstruktur:

locales/
en.json
de.json

👉 Beispiel (de.json):

{
 }

👉 Code:

bundle := i18n.NewBundle(language.German)
bundle.LoadMessageFile("locales/de.json")

localizer := i18n.NewLocalizer(bundle, "de")

msg, _ := localizer.Localize(&i18n.LocalizeConfig{
MessageID: "hello",
TemplateData: map[string]interface{}{
"Name": "Max",
},
})

fmt.Println(msg)

Server Logging bleibt englisch. Multi language support ist nur fuer UI.


