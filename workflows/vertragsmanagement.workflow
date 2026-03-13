workflow "Vertragsmanagement"
priority high
label finance
label legal

section "Erstellung"
  step "Vertragsentwurf"
    note "Ersten Entwurf nach Vorlage erstellen"
    due 5d
    list "Parteien definiert" required
    list "Leistungsumfang beschrieben" required
    list "Laufzeit festgelegt" required
    list "Vergütung definiert" required

  step "Internes Review"
    needs "Vertragsentwurf"
    ask "Internes Review abgeschlossen?" -> "Externe Abstimmung", "Überarbeiten"
    due 3d
    notify "legal@firma.de"

  step "Überarbeiten"
    needs "Internes Review"
    note "Feedback einarbeiten und erneut prüfen lassen"
    ends

section "Abstimmung"
  step "Externe Abstimmung"
    needs "Internes Review"
    due 10d
    note "Vertrag an Gegenpartei senden"

  step "Verhandlung"
    needs "Externe Abstimmung"
    ask "Verhandlung abgeschlossen?" -> "Freigabe", "Überarbeiten"
    gate
    notify "legal@firma.de"

section "Freigabe"
  step "Geschäftsführer Freigabe"
    needs "Verhandlung"
    ask "Vertrag vom GF freigegeben?" -> "Unterzeichnung", "Abgelehnt"
    gate
    notify "gf@firma.de"

  step "Abgelehnt"
    needs "Geschäftsführer Freigabe"
    ends

section "Abschluss"
  step "Unterzeichnung"
    needs "Geschäftsführer Freigabe"
    due 5d
    list "Eigene Unterschrift" required
    list "Gegenpartei Unterschrift" required

  step "Archivierung"
    needs "Unterzeichnung"
    list "Original gescannt" required
    list "Im Vertragsordner abgelegt" required
    list "Erinnerung Verlängerung eingestellt" optional
    notify "legal@firma.de"
