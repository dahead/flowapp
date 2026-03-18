workflow "Kontowechsel"
priority medium
label finanzen

section "Vorbereitung"
  step "Neues Konto eröffnen"
    note "IBAN und BIC des neuen Kontos notieren"
    due 3d
    list "Personalausweis bereithalten" required
    list "Online-Antrag ausgefüllt" required
    list "Video-Ident oder Filiale" required

  step "Kontowechselservice prüfen"
    needs "Neues Konto eröffnen"
    note "Viele Banken bieten kostenlosen Wechselservice an"
    ask "Wechselservice der neuen Bank nutzen?" -> "Wechselauftrag erteilen", "Daueraufträge übertragen"

section "Automatischer Wechsel"
  step "Wechselauftrag erteilen"
    needs "Kontowechselservice prüfen"
    note "Neue Bank übernimmt Kommunikation mit alter Bank (§ 21 ZAG)"
    due 2d

  step "Wechsel bestätigt"
    needs "Wechselauftrag erteilen"
    ask "Hat die neue Bank den Wechsel bestätigt?" -> "Abschluss", "Nachfassen"

  step "Nachfassen"
    needs "Wechsel bestätigt"
    note "Neue Bank kontaktieren, Status erfragen"
    ends

section "Manueller Wechsel"
  step "Daueraufträge übertragen"
    needs "Kontowechselservice prüfen"
    due 5d
    list "Liste aller Daueraufträge erstellt" required
    list "Daueraufträge beim neuen Konto eingerichtet" required

  step "Lastschriften ummelden"
    needs "Kontowechselservice prüfen"
    due 7d
    list "Arbeitgeber (Gehalt)" required
    list "Vermieter (Miete)" required
    list "Versicherungen" required
    list "Streaming/Abos" optional
    list "Finanzamt" optional

  step "Gläubiger informiert"
    needs "Daueraufträge übertragen", "Lastschriften ummelden"
    ask "Alle relevanten Stellen informiert?" -> "Altes Konto kündigen", "Noch offen"

  step "Noch offen"
    ends

section "Abschluss"
  step "Altes Konto kündigen"
    needs "Gläubiger informiert"
    note "Kündigungsfrist beachten, Restguthaben überweisen"
    due 14d
    list "Kündigungsschreiben abgeschickt" required
    list "Restguthaben übertragen" required
    list "Kreditkarte zurückgegeben" optional

  step "Abschluss"
    needs "Altes Konto kündigen", "Wechsel bestätigt"
    notify "role:finance"
