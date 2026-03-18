workflow "Product Launch"
allowed_roles role:management role:design role:engineering
label demo

section "Planning"
  step "Define Scope"
    note "Align on MVP features with stakeholders"
    assign "role:management"
    due 1w
    list "Feature list finalized" required
    list "Success metrics defined" required
    list "Budget approved" required

  step "Assemble Team"
    needs "Define Scope"
    assign "role:management"
    due 3d
    list "Product owner assigned" required
    list "Engineering lead assigned" required
    list "Design lead assigned" required
    list "Marketing contact assigned" optional

section "Development"
  step "Design Mockups"
    needs "Assemble Team"
    assign "role:design"
    due 2w

  step "Engineering Kickoff"
    needs "Assemble Team"
    assign "role:engineering"
    notify "role:engineering"
    due 1d

  step "Build Feature"
    needs "Design Mockups", "Engineering Kickoff"
    assign "role:engineering"
    due 4w

  step "Internal Review"
    needs "Build Feature"
    assign "role:product"
    notify "role:product"
    ask "Internal review passed?" -> "Pilot Test", "Rework"
    due 1w

  step "Rework"
    needs "Internal Review"
    assign "role:engineering"
    ends

  step "Pilot Test"
    needs "Internal Review"
    assign "role:product"
    notify "role:product"
    due 2w
    list "Test users recruited" required
    list "Feedback collected" required
    list "Critical bugs fixed" required

section "Marketing"
  step "Press Kit"
    needs "Internal Review"
    assign "role:marketing"
    due 1w
    list "Product screenshots" required
    list "Press release" required
    list "Demo video" optional

section "Launch"
  step "Marketing Prep"
    needs "Pilot Test", "Press Kit"
    assign "role:marketing"
    due 1w
    list "Landing page live" required
    list "Announcement drafted" required
    list "Social posts scheduled" optional

  step "Launch Approval"
    needs "Marketing Prep"
    ask "Ready to launch?" -> "Go Live", "Hold Launch"
    gate
    notify "role:management"

  step "Hold Launch"
    needs "Launch Approval"
    notify "role:management"
    notify "role:product"
    note "Document blockers and reschedule"
    ends

  step "Go Live"
    needs "Launch Approval"
    notify "role:all"
