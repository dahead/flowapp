workflow Demo: Schedule & Assign
allowed_roles role:finance role:hr
label demo

section Vorbereitung

step Antrag einreichen
  note Bitte alle Unterlagen beifügen.
  assign "user:suzie@mailbox.org"

step Erste Prüfung
  note Wird automatisch nach 3 Tagen aktiviert.
  assign "role:finance"
  schedule +3d
  needs "Antrag einreichen"

section Genehmigung

step Freigabe durch Vorgesetzten
  assign "user:anna"
  needs "Erste Prüfung"

step Erinnerung Frist
  note Automatische Erinnerung nach 7 Tagen falls noch nicht genehmigt.
  schedule +7d
  assign "role:hr"
  needs "Antrag einreichen"

step Abschluss
  needs "Freigabe durch Vorgesetzten"
  ends
