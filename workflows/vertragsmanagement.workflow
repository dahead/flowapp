workflow "Vertragsmanagement"
allowed_roles role:legal role:management
label finance
label legal

section "Erstellung"
  step "Vertragsentwurf"
    note "Ersten Entwurf nach Vorlage erstellen"
    assign "role:legal"
    due 5d
    list "Parteien definiert" required
    list "Leistungsumfang beschrieben" required
    list "Laufzeit festgelegt" required
    list "Vergütung definiert" required

  step "Internes Review"
    needs "Vertragsentwurf"
    ask "Internes Review abgeschlossen?" -> "Externe Abstimmung", "Überarbeiten"
    due 3d
    assign "role:legal"
    notify "role:legal"
    notify "role:finance"

  step "Überarbeiten"
    needs "Internes Review"
    assign "role:legal"
    note "Feedback einarbeiten und erneut prüfen lassen"
    ends

section "Abstimmung"
  step "Externe Abstimmung"
    needs "Internes Review"
    assign "role:legal"
    due 10d
    note "Vertrag an Gegenpartei senden"

  step "Verhandlung"
    needs "Externe Abstimmung"
    ask "Verhandlung abgeschlossen?" -> "Freigabe", "Überarbeiten"
    gate
    notify "role:legal"
    notify "role:management"

section "Freigabe"
  step "Geschäftsführer Freigabe"
    needs "Verhandlung"
    ask "Vertrag vom GF freigegeben?" -> "Unterzeichnung", "Abgelehnt"
    gate
    notify "role:management"

  step "Abgelehnt"
    needs "Geschäftsführer Freigabe"
    notify "role:legal"
    ends

section "Abschluss"
  step "Unterzeichnung"
    needs "Geschäftsführer Freigabe"
    assign "role:management"
    due 5d
    list "Eigene Unterschrift" required
    list "Gegenpartei Unterschrift" required

  step "Archivierung"
    needs "Unterzeichnung"
    assign "role:legal"
    list "Original gescannt" required
    list "Im Vertragsordner abgelegt" required
    list "Erinnerung Verlängerung eingestellt" optional
    notify "role:legal"
    notify "role:finance"
    notify "role:management"
