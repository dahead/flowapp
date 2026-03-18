workflow "Invoice Payment"
allowed_roles role:warehouse role:finance
label finance

section "Order"
  step "Receive Order"
    note "Check order completeness before accepting"
    notify "role:warehouse"

  step "Validate Order"
    needs "Receive Order"
    assign "role:warehouse"
    ask "Is the order valid and complete?" -> "Fill Order", "Reject Order"

  step "Fill Order"
    needs "Validate Order"
    assign "role:warehouse"
    notify "role:warehouse"
    due 2d

  step "Reject Order"
    needs "Validate Order"
    notify "role:finance"
    ends

  step "Ship Order"
    needs "Fill Order"
    assign "role:warehouse"

  step "Close Order"
    needs "Ship Order", "Accept Payment"
    notify "role:finance"
    notify "role:management"

section "Transaction"
  step "Send Invoice"
    needs "Fill Order"
    assign "role:finance"
    notify "role:finance"
    note "Attach PDF invoice to email"

  step "Process Payment"
    needs "Send Invoice"
    assign "role:finance"
    due 14d

  step "Accept Payment"
    needs "Process Payment"
    ask "Has payment cleared in the bank?" -> "Close Order", "Escalate Payment"

  step "Escalate Payment"
    needs "Accept Payment"
    notify "role:finance"
    notify "role:management"
    ends
