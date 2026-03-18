workflow "Employee Offboarding"
priority high
label hr

section "Initiation"
  step "Offboarding Initiated"
    note "Last working day confirmed with HR and manager"
    notify "role:hr"
    notify "role:it"
    notify "role:management"

section "HR"
  step "Exit Interview"
    needs "Offboarding Initiated"
    assign "role:hr"
    due 3d
    ask "Exit interview completed?" -> "Process Final Payroll", "Skip Exit Interview"

  step "Skip Exit Interview"
    needs "Exit Interview"
    note "Document reason for skipping"

  step "Process Final Payroll"
    needs "Exit Interview"
    assign "role:payroll"
    due 7d
    notify "role:payroll"
    list "Final salary calculated" required
    list "Vacation payout included" required
    list "Bonus prorated" optional

  step "HR Closed"
    needs "Process Final Payroll", "IT Offboarded", "Assets Returned"
    notify "role:hr"
    notify "role:management"

section "IT"
  step "Revoke Access"
    needs "Offboarding Initiated"
    assign "role:it"
    due 1d
    list "Email disabled" required
    list "VPN revoked" required
    list "GitHub removed" required
    list "Slack deactivated" required
    list "SaaS tools removed" required

  step "Data Backup"
    needs "Offboarding Initiated"
    assign "role:it"
    due 2d
    note "Archive email and shared drive content"

  step "IT Offboarded"
    needs "Revoke Access", "Data Backup"
    notify "role:it"

section "Assets"
  step "Return Assets"
    needs "Offboarding Initiated"
    assign "role:facilities"
    due 5d
    list "Laptop returned" required
    list "Access card returned" required
    list "Company phone returned" optional

  step "Assets Returned"
    needs "Return Assets"
    ask "All assets returned and checked?" -> "HR Closed", "Assets Pending"

  step "Assets Pending"
    needs "Assets Returned"
    notify "role:hr"
    notify "role:facilities"
    ends
