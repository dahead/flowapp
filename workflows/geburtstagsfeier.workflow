workflow "Geburtstagsfeier planen"
label privat
label event
var NAME
var DATUM
var ORT

section "Konzept"
  step "Feier planen"
    note "Für $NAME am $DATUM in / bei $ORT"
    item! "Gästeliste erstellt"
    item! "Budget festgelegt"
    ask "Wo findet die Feier statt?" -> "Zuhause", "Externe Location"

  step "Externe Location"
    needs "Feier planen"
    due 21d
    item! "Mindestens 2 Locations verglichen"
    item! "Location gebucht und bestätigt"
    item "Anzahlung überwiesen"

  step "Zuhause"
    needs "Feier planen"
    item "Platzbedarf geprüft"
    item "Nachbarn informiert"

section "Organisation"
  step "Einladungen verschicken"
    needs "Externe Location", "Zuhause"
    due 14d
    item! "Einladungen raus (digital oder Papier)"
    item! "Antwortfrist gesetzt"

  step "Rückmeldungen sammeln"
    needs "Einladungen verschicken"
    schedule +7d
    due 7d
    item! "Endgültige Gästezahl bekannt"
    item "Unverträglichkeiten / Allergien notiert"

  step "Essen & Trinken"
    needs "Rückmeldungen sammeln"
    due 5d
    item! "Menü / Buffet geplant"
    item! "Einkaufsliste erstellt"
    item "Kuchen / Torte bestellt oder geplant"

  step "Dekoration & Musik"
    needs "Feier planen"
    schedule +7d
    item "Deko besorgt"
    item "Playlist erstellt"
    item "Spiele / Programm überlegt"

section "Durchführung"
  step "Einkauf erledigen"
    needs "Essen & Trinken"
    due 1d
    item! "Lebensmittel eingekauft"
    item! "Getränke besorgt"

  step "Feier durchführen"
    needs "Einkauf erledigen", "Dekoration & Musik", "Rückmeldungen sammeln"
    note "Feier für $NAME am $DATUM"

  step "Aufräumen & Danke sagen"
    needs "Feier durchführen"
    item "Aufgeräumt"
    item "Dankes-Nachrichten verschickt"
    item "Location-Kaution zurückerhalten"
    ends
