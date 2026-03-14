workflow "Shopping List"
priority low
label shopping

section "Planning"
  step "Create List"
    note "Add your shopping items below"

section "Shopping"
  step "Go Shopping"
    needs "Create List"

  step "Pay"
    needs "Go Shopping"
    ask "All items purchased?" -> "Done", "Missing Items"

  step "Missing Items"
    needs "Pay"
    note "Note what was missing for next time"
    ends

  step "Done"
    needs "Pay"
