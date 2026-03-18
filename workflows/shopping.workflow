workflow "Shopping List"
label shopping
label privat

section "Planning"
  step "Create List"
    list "Fruits & Vegetables" optional
    list "Dairy" optional
    list "Bread & Bakery" optional
    list "Meat & Fish" optional
    list "Pantry" optional
    list "Cleaning" optional
    list "Personal Care" optional

  step "Check Budget"
    needs "Create List"
    ask "Budget checked and list finalized?" -> "Go Shopping", "Revise List"

  step "Revise List"
    needs "Check Budget"
    ends

section "Shopping"
  step "Go Shopping"
    needs "Check Budget"

  step "Pay"
    needs "Go Shopping"
    ask "All items purchased?" -> "Done", "Missing Items"

  step "Missing Items"
    needs "Pay"
    note "Note what was missing for next time"
    ends

  step "Done"
    needs "Pay"
