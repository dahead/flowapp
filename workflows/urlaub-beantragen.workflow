workflow "Urlaub beantragen"
priority low
label hr
label personal

section "Planung"
  step "Urlaubszeitraum festlegen"
    note "Resturlaub prüfen und Kollegen abstimmen"
    list "Urlaubsanspruch geprüft" required
    list "Vertretung geklärt" required
    list "Persönliche Termine berücksichtigt" optional

  step "Antrag stellen"
    needs "Urlaubszeitraum festlegen"
    note "Schriftlich per Tool, E-Mail oder Formular — je nach Firma"
    due 2d

  step "Genehmigung"
    needs "Antrag stellen"
    ask "Urlaub genehmigt?" -> "Übergabe organisieren", "Alternativtermin suchen"
    gate
    notify "vorgesetzte@firma.de"

  step "Alternativtermin suchen"
    needs "Genehmigung"
    ends

section "Vorbereitung"
  step "Übergabe organisieren"
    needs "Genehmigung"
    due 3d
    list "Offene Aufgaben dokumentiert" required
    list "Vertretung eingewiesen" required
    list "Automatische E-Mail-Abwesenheit eingerichtet" required
    list "Weiterleitung Telefon" optional

  step "Abwesenheit im Kalender eintragen"
    needs "Genehmigung"
    list "Teamkalender aktualisiert" required
    list "Projekttools aktualisiert" optional

  step "Reise buchen"
    needs "Genehmigung"
    list "Flug/Bahn gebucht" optional
    list "Unterkunft gebucht" optional
    list "Reisekrankenversicherung" optional

  step "Alles erledigt"
    needs "Übergabe organisieren", "Abwesenheit im Kalender eintragen"
    note "Guten Urlaub! 🌴"
