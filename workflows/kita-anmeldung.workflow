workflow "KiTa-Anmeldung"
priority high
label kita
label verwaltung
var KIND_NAME
var GEBURTSDATUM
var ELTERN_EMAIL

section "Eingang"
  step "Anmeldung eingegangen"
    note "Kind: $KIND_NAME, geb. $GEBURTSDATUM"
    notify "$ELTERN_EMAIL"
    item! "Anmeldeformular vollständig"
    item! "Geburtsurkunde vorhanden"
    item "Impfpass gesichtet"

section "Prüfung"
  step "Platzverfügbarkeit prüfen"
    needs "Anmeldung eingegangen"
    assign "role:verwaltung"
    due 3d
    ask "Platz verfügbar?" -> "Zusage vorbereiten", "Warteliste"

  step "Warteliste"
    needs "Platzverfügbarkeit prüfen"
    note "Familie $KIND_NAME auf Warteliste gesetzt."
    notify "$ELTERN_EMAIL"
    ends

section "Zusage"
  step "Zusage vorbereiten"
    needs "Platzverfügbarkeit prüfen"
    assign "role:leitung"
    due 2d
    item! "Zusageschreiben erstellt"
    item! "Betreuungsvertrag vorbereitet"

  step "Leitung genehmigt Aufnahme"
    needs "Zusage vorbereiten"
    ask "Aufnahme bestätigen?" -> "Eltern benachrichtigen", "Zurück zur Prüfung"
    gate
    notify "leitung@kita.de"

  step "Zurück zur Prüfung"
    needs "Leitung genehmigt Aufnahme"
    assign "role:verwaltung"
    ends

section "Abschluss"
  step "Eltern benachrichtigen"
    needs "Leitung genehmigt Aufnahme"
    notify "$ELTERN_EMAIL"
    note "Zusage + Vertrag an Familie $KIND_NAME versenden"

  step "Vertrag unterschrieben zurück"
    needs "Eltern benachrichtigen"
    schedule +14d
    due 3d
    item! "Betreuungsvertrag unterschrieben erhalten"

  step "Aufnahme abgeschlossen"
    needs "Vertrag unterschrieben zurück"
    notify "$ELTERN_EMAIL"
    ends
