workflow "Employee Onboarding"
priority high
label hr

var EMPLOYEE_NAME
var START_DATE

section "Trigger"
  step "Start Onboarding"
    note "New employee $EMPLOYEE_NAME starts on $START_DATE"
    notify "role:hr"
    notify "role:it"
    notify "role:facilities"

section "HR"
  step "Send Welcome Email"
    needs "Start Onboarding"
    assign "role:hr"
    due 1d
    note "Welcome email to $EMPLOYEE_NAME"

  step "Create Employee Record"
    needs "Start Onboarding"
    assign "role:hr"
    due 1d
    list "Personal data verified" required
    list "Tax documents" required
    list "Bank details" required

  step "Prepare Employment Contract"
    needs "Create Employee Record"
    assign "role:hr"
    due 3d

  step "Contract Review"
    needs "Prepare Employment Contract"
    ask "Contract signed by $EMPLOYEE_NAME?" -> "Onboarding Complete", "Escalate HR"
    gate
    notify "role:hr"

  step "Escalate HR"
    needs "Contract Review"
    notify "role:hr-lead"
    ends

  step "Onboarding Complete"
    needs "Contract Review", "IT Ready", "Office Ready", "Buddy Introduction"
    notify "role:hr"
    notify "role:management"
    note "Send first-day schedule to $EMPLOYEE_NAME"

section "IT"
  step "Create Accounts"
    needs "Start Onboarding"
    assign "role:it"
    due 2d
    list "Email account" required
    list "Slack" required
    list "GitHub" required
    list "VPN access" required
    list "Password manager" optional

  step "Prepare Hardware"
    needs "Start Onboarding"
    assign "role:it"
    due 3d
    list "Laptop configured" required
    list "Monitor" required
    list "Headset" optional
    list "Desk phone" optional

  step "IT Approval"
    needs "Create Accounts", "Prepare Hardware"
    ask "IT setup complete for $EMPLOYEE_NAME?" -> "IT Ready", "IT Issues"
    gate
    notify "role:it"

  step "IT Issues"
    needs "IT Approval"
    notify "role:it-lead"
    ends

  step "IT Ready"
    needs "IT Approval"

section "Office"
  step "Prepare Workspace"
    needs "Start Onboarding"
    assign "role:facilities"
    due 2d
    list "Desk assigned" required
    list "Access card" required
    list "Building tour scheduled" required
    list "Parking spot" optional

  step "Office Approval"
    needs "Prepare Workspace"
    ask "Workspace ready for $EMPLOYEE_NAME?" -> "Office Ready", "Office Issues"
    gate
    notify "role:facilities"

  step "Office Issues"
    needs "Office Approval"
    notify "role:facilities"
    ends

  step "Office Ready"
    needs "Office Approval"

section "Buddy Program"
  step "Assign Buddy"
    needs "Start Onboarding"
    assign "role:hr"
    due 1d

  step "Buddy Introduction"
    needs "Assign Buddy"
    due 1d
    note "30-min intro call before first day"
