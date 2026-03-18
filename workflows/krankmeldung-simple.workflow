workflow "Krankmeldung"
allowed_roles role:hr
label personal
label business

section "Krank?"
  step "Status prüfen"
    ask "Bist du krank?" -> "Sofortmaßnahmen", "Nicht krank"

  step "Nicht krank"
    needs "Status prüfen"
    ends

section "Sofortmaßnahmen"
  step "Sofortmaßnahmen"
    needs "Status prüfen"
    note "Vor Arbeitsbeginn erledigen"
    due 4h
    list "Vorgesetzte/n informiert (Anruf oder Nachricht)" required
    list "Out-of-Office eingerichtet" required
    list "Heutige Termine abgesagt oder delegiert" optional
    list "Wichtige laufende Aufgaben übergeben" optional

  step "Attest nötig?"
    needs "Sofortmaßnahmen"
    ask "Dauert die Erkrankung länger als 3 Tage?" -> "Arzt aufsuchen", "Keine AU nötig"

section "Ohne Attest"
  step "Keine AU nötig"
    needs "Attest nötig?"
    note "Bei 1–3 Krankheitstagen i.d.R. kein Attest erforderlich"
    ends

section "Mit Attest"
  step "Arzt aufsuchen"
    needs "Attest nötig?"
    due 1d
    list "Krankenversicherungskarte" required
    list "Personalausweis" optional

  step "AU einreichen"
    needs "Arzt aufsuchen"
    note "Original an Arbeitgeber — oder elektronisch via eAU"
    due 1d
    list "AU an Arbeitgeber übermittelt" required
    list "AU an Krankenkasse (falls nicht eAU)" optional
