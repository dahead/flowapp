workflow "Krankmeldung"
priority high
label personal
label hr

section "Erster Krankheitstag"
  step "Arbeitgeber informieren"
    note "Spätestens vor Arbeitsbeginn anrufen oder schreiben — gesetzliche Pflicht"
    due 4h
    notify "role:management"
    notify "role:hr"
    list "Vorgesetzte/n benachrichtigt" required
    list "Team informiert (optional)" optional

  step "Arzttermin vereinbaren"
    needs "Arbeitgeber informieren"
    ask "Brauche ich eine Krankmeldung (AU)?" -> "Arzt aufsuchen", "Kein Attest nötig"

  step "Kein Attest nötig"
    needs "Arzttermin vereinbaren"
    note "Ab dem 4. Tag ist ein Attest i.d.R. Pflicht"
    ends

section "Arztbesuch"
  step "Arzt aufsuchen"
    needs "Arzttermin vereinbaren"
    due 1d
    list "Krankenversicherungskarte" required
    list "Personalausweis" optional

  step "AU erhalten"
    needs "Arzt aufsuchen"
    ask "Krankschreibung erhalten?" -> "AU einreichen", "Verlängerung nötig"

  step "Verlängerung nötig"
    needs "AU erhalten"
    note "Erneuten Arzttermin vereinbaren"
    ends

section "Einreichung"
  step "AU einreichen"
    needs "AU erhalten"
    note "Original an Arbeitgeber, Kopie an Krankenkasse — oder elektronisch (eAU)"
    due 1d
    notify "role:hr"
    list "AU an Arbeitgeber übermittelt" required
    list "AU an Krankenkasse (falls nicht eAU)" optional

  step "Krankentagegeld prüfen"
    needs "AU einreichen"
    note "Ab 6. Woche zahlt die Krankenkasse Krankengeld (70% Brutto)"
    ask "Dauert die Erkrankung länger als 6 Wochen?" -> "Krankengeld beantragen", "Genesung"

  step "Genesung"
    needs "Krankentagegeld prüfen"
    notify "role:hr"
    notify "role:management"
    ends

section "Langzeiterkrankung"
  step "Krankengeld beantragen"
    needs "Krankentagegeld prüfen"
    assign "role:hr"
    notify "role:hr"
    due 3d
    list "Antrag bei Krankenkasse gestellt" required
    list "Lohnbescheinigung vom Arbeitgeber" required

  step "Wiedereingliederung planen"
    needs "Krankengeld beantragen"
    assign "role:hr"
    note "Hamburger Modell: schrittweise Rückkehr möglich"
    ask "Stufenweise Wiedereingliederung gewünscht?" -> "Wiedereingliederungsplan erstellen", "Normale Rückkehr"

  step "Wiedereingliederungsplan erstellen"
    needs "Wiedereingliederung planen"
    assign "role:hr"
    notify "role:hr"
    notify "role:management"

  step "Normale Rückkehr"
    needs "Wiedereingliederung planen"
    notify "role:hr"
    ends
