workflow "Umzug organisieren"
label haushalt
label privat
var NEUE_ADRESSE
var UMZUGSDATUM

section "Vorbereitung"
  step "Umzug beschlossen"
    note "Neue Adresse: $NEUE_ADRESSE — Datum: $UMZUGSDATUM"
    item! "Neuer Mietvertrag unterschrieben"
    item! "Kündigungsfrist alter Wohnung geprüft"

  step "Alte Wohnung kündigen"
    needs "Umzug beschlossen"
    due 7d
    item! "Kündigung schriftlich abgeschickt"
    item! "Kündigungsbestätigung erhalten"

  step "Umzugshelfer organisieren"
    needs "Umzug beschlossen"
    due 14d
    ask "Wie wird umgezogen?" -> "Umzugsfirma beauftragen", "Privat mit Helfern"

  step "Umzugsfirma beauftragen"
    needs "Umzugshelfer organisieren"
    item! "Mindestens 2 Angebote eingeholt"
    item! "Firma gebucht und bestätigt"
    item "Versicherung für Umzugsgut geprüft"

  step "Privat mit Helfern"
    needs "Umzugshelfer organisieren"
    item! "Helfer zugesagt (mind. 3)"
    item! "Transporter gemietet"
    item "Verpflegung organisiert"

section "Ummeldungen"
  step "Ummeldungen vorbereiten"
    needs "Umzug beschlossen"
    schedule +7d
    item! "Liste aller Stellen erstellt"
    item "Bank"
    item "Arbeitgeber"
    item "Krankenkasse"
    item "Versicherungen"
    item "Abonnements"

  step "Einwohnermeldeamt"
    needs "Umzug beschlossen"
    schedule +14d
    due 14d
    note "Muss innerhalb 14 Tage nach Einzug erfolgen"
    item! "Termin beim Einwohnermeldeamt gemacht"
    item! "Ummeldung erfolgt"

section "Umzugstag"
  step "Umzug durchführen"
    needs "Alte Wohnung kündigen", "Umzugsfirma beauftragen"
    note "Umzugstag: $UMZUGSDATUM nach $NEUE_ADRESSE"
    item! "Alle Kartons beschriftet"
    item! "Möbel abgebaut"
    item! "Neue Wohnung bezugsfertig"

  step "Alte Wohnung übergeben"
    needs "Umzug durchführen"
    due 7d
    item! "Wohnung besenrein gereinigt"
    item! "Übergabeprotokoll unterschrieben"
    item! "Schlüssel zurückgegeben"
    item "Kaution zurückgefordert"

  step "Umzug abgeschlossen"
    needs "Alte Wohnung übergeben", "Einwohnermeldeamt"
    ends
