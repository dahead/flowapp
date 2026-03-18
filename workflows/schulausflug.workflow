workflow "Schulausflug planen"
allowed_roles role:lehrer role:sekretariat
label schule
label organisation
var ZIEL
var DATUM
var KLASSE

section "Planung"
  step "Ausflug vorschlagen"
    note "Ziel: $ZIEL — Datum: $DATUM — Klasse: $KLASSE"
    assign "role:lehrer"
    item! "Ziel und Datum festgelegt"
    item! "Lehrplanbezug notiert"
    item "Alternative bei Schlechtwetter überlegt"

  step "Schulleitung genehmigt"
    needs "Ausflug vorschlagen"
    ask "Ausflug genehmigt?" -> "Eltern informieren", "Überarbeiten"
    gate
    notify "role:schulleitung"

  step "Überarbeiten"
    needs "Schulleitung genehmigt"
    assign "role:lehrer"
    ends

section "Organisation"
  step "Eltern informieren"
    needs "Schulleitung genehmigt"
    assign "role:lehrer"
    notify "role:elternbeirat"
    item! "Elternbrief verschickt"
    item! "Einverständniserklärung beigefügt"

  step "Einverständnisse einsammeln"
    needs "Eltern informieren"
    schedule +7d
    due 7d
    assign "role:lehrer"
    item! "Alle Einverständnisse unterschrieben"
    item "Besondere Hinweise (Allergien etc.) notiert"

  step "Transport buchen"
    needs "Schulleitung genehmigt"
    assign "role:sekretariat"
    due 14d
    item! "Bus / Bahn gebucht"
    item! "Kosten bestätigt"

  step "Eintrittskarten / Anmeldung"
    needs "Schulleitung genehmigt"
    assign "role:lehrer"
    due 10d

section "Durchführung"
  step "Ausflug durchführen"
    needs "Einverständnisse einsammeln", "Transport buchen", "Eintrittskarten / Anmeldung"
    note "Ausflug $ZIEL mit Klasse $KLASSE am $DATUM"

  step "Nachbereitung"
    needs "Ausflug durchführen"
    assign "role:lehrer"
    item "Fotos gesichtet und freigegeben"
    item "Kurzbericht für Schulzeitung"
    ends
