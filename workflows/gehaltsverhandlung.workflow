workflow "Gehaltsverhandlung"
allowed_roles role:management
label business

section "Vorbereitung"
  step "Marktwert recherchieren"
    note "Gehaltsvergleiche: Stepstone, Kununu, Glassdoor, Bundesagentur für Arbeit"
    due 5d
    list "Branchenübliche Gehälter recherchiert" required
    list "Regionale Unterschiede berücksichtigt" required
    list "Erfahrungslevel einbezogen" required

  step "Eigene Leistungen dokumentieren"
    needs "Marktwert recherchieren"
    due 3d
    list "Erfolge der letzten 12 Monate gelistet" required
    list "Mehrwert für das Unternehmen quantifiziert" required
    list "Weiterbildungen und neue Skills notiert" optional
    list "Positives Feedback von Kollegen/Kunden gesammelt" optional

  step "Zielgehalt festlegen"
    needs "Eigene Leistungen dokumentieren"
    note "Wunschgehalt + 10-15% als Einstiegsforderung; klare Untergrenze definieren"
    list "Wunschgehalt definiert" required
    list "Untergrenze definiert" required
    list "Argumentation vorbereitet" required

  step "Gespräch anfragen"
    needs "Zielgehalt festlegen"
    note "Konkreten Termin anfragen, nicht zwischen Tür und Angel"
    due 2d
    notify "role:hr"
    ask "Termin bestätigt?" -> "Gespräch vorbereiten", "Nachfassen"

  step "Nachfassen"
    needs "Gespräch anfragen"
    ends

section "Gespräch"
  step "Gespräch vorbereiten"
    needs "Gespräch anfragen"
    due 1d
    list "Argumente nochmal durchgegangen" required
    list "Zahlen und Fakten griffbereit" required
    list "Mögliche Einwände antizipiert" optional

  step "Gehaltsverhandlung führen"
    needs "Gespräch vorbereiten"
    assign "role:management"
    note "Ruhig bleiben, konkret fordern, Stille aushalten"
    ask "Wie ist das Gespräch verlaufen?" -> "Zusage erhalten", "Absage erhalten", "Bedenkzeit erbeten"

section "Nachbereitung"
  step "Zusage erhalten"
    needs "Gehaltsverhandlung führen"
    note "Schriftliche Bestätigung oder Vertragsänderung anfordern"
    list "Schriftliche Bestätigung erhalten" required
    list "Neues Gehalt im Vertrag / Nachtrag" required
    notify "role:hr"

  step "Bedenkzeit erbeten"
    needs "Gehaltsverhandlung führen"
    note "Frist setzen: max. 1-2 Wochen"
    ask "Entscheidung nach Bedenkzeit?" -> "Zusage erhalten", "Absage erhalten"
    gate
    assign "role:management"
    notify "role:hr"

  step "Absage erhalten"
    needs "Gehaltsverhandlung führen"
    note "Gründe erfragen, nächsten Reviewtermin vereinbaren oder Alternativen prüfen"
    notify "role:hr"
    list "Gründe für Absage erfragt" optional
    list "Nächsten Termin in 6 Monaten vereinbart" optional
    list "Markt sondieren / Alternativen prüfen" optional
