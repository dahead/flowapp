workflow "Product Launch"
priority high
label product

section "Planning"
  step "Define Scope"
    note "Align on MVP features with stakeholders"
    due 1w
    list "Feature list finalized" required
    list "Success metrics defined" required
    list "Budget approved" required

  step "Assemble Team"
    needs "Define Scope"
    due 3d
    list "Product owner assigned" required
    list "Engineering lead assigned" required
    list "Design lead assigned" required
    list "Marketing contact assigned" optional

section "Development"
  step "Design Mockups"
    needs "Assemble Team"
    due 2w

  step "Engineering Kickoff"
    needs "Assemble Team"
    due 1d

  step "Build Feature"
    needs "Design Mockups", "Engineering Kickoff"
    due 4w

  step "Internal Review"
    needs "Build Feature"
    ask "Internal review passed?" -> "Pilot Test", "Rework"
    due 1w

  step "Rework"
    needs "Internal Review"
    ends

  step "Pilot Test"
    needs "Internal Review"
    due 2w
    notify "product@company.com"
    list "Test users recruited" required
    list "Feedback collected" required
    list "Critical bugs fixed" required

section "Launch"
  step "Marketing Prep"
    needs "Pilot Test", "Press Kit"
    due 1w
    list "Landing page live" required
    list "Announcement drafted" required
    list "Social posts scheduled" optional

  step "Launch Approval"
    needs "Marketing Prep"
    ask "Ready to launch?" -> "Go Live", "Hold Launch"
    gate
    notify "ceo@company.com"

  step "Hold Launch"
    needs "Launch Approval"
    note "Document blockers and reschedule"
    ends

  step "Go Live"
    needs "Launch Approval"
    notify "all@company.com"

section "Marketing"
  step "Press Kit"
    needs "Internal Review"
    due 1w
    list "Product screenshots" required
    list "Press release" required
    list "Demo video" optional
