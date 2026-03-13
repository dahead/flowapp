workflow "Invoice Payment"
priority medium
label finance

section "Order"
  step "Receive Order"
    note "Check order completeness before accepting"

  step "Validate Order"
    needs "Receive Order"
    ask "Is the order valid and complete?" -> "Fill Order", "Reject Order"

  step "Fill Order"
    notify "warehouse@company.com"
    due 2d

  step "Reject Order"
    needs "Validate Order"
    ends

  step "Ship Order"
    needs "Fill Order"

  step "Close Order"
    needs "Ship Order", "Accept Payment"

section "Transaction"
  step "Send Invoice"
    needs "Fill Order"
    notify "finance@company.com"
    note "Attach PDF invoice to email"

  step "Process Payment"
    needs "Send Invoice"
    due 14d

  step "Accept Payment"
    needs "Process Payment"
    ask "Has payment cleared in the bank?" -> "Close Order", "Escalate Payment"

  step "Escalate Payment"
    needs "Accept Payment"
    notify "finance@company.com"
    ends
