workflow "Reisekostenabrechnung"
priority medium
label finance

section "Einreichung"
  step "Belege sammeln"
    note "Alle Quittungen und Belege zusammentragen"
    due 5d
    list "Hotel" optional
    list "Bahn/Flug" optional
    list "Taxi/Mietwagen" optional
    list "Verpflegung" optional
    list "Sonstiges" optional

  step "Abrechnung erstellen"
    needs "Belege sammeln"
    due 2d
    note "Formular ausfüllen und Belege anhängen"

  step "Einreichen"
    needs "Abrechnung erstellen"
    ask "Abrechnung vollständig und eingereicht?" -> "Prüfung", "Nachbesserung nötig"
    notify "role:buchhaltung"

  step "Nachbesserung nötig"
    needs "Einreichen"
    ends

section "Prüfung"
  step "Prüfung"
    assign "role:buchhaltung"
    ask "Abrechnung geprüft und genehmigt?" -> "Auszahlung", "Abgelehnt"
    gate
    notify "role:buchhaltung"
    due 5d

  step "Abgelehnt"
    needs "Prüfung"
    notify "role:hr"
    ends

section "Auszahlung"
  step "Auszahlung"
    needs "Prüfung"
    assign "role:buchhaltung"
    due 7d
    notify "role:buchhaltung"

  step "Abgeschlossen"
    needs "Auszahlung"
    ask "Auszahlung auf Konto bestätigt?" -> "Archivieren", "Nachfragen"

  step "Nachfragen"
    needs "Abgeschlossen"
    notify "role:buchhaltung"
    ends

  step "Archivieren"
    needs "Abgeschlossen"
    assign "role:buchhaltung"
