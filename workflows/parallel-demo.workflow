workflow "Parallel Demo"
priority low
label demo

section "Parallel Work"
  step "Start"

  step "Task A"
    needs "Start"

  step "Task B"
    needs "Start"

  step "Review"
    needs "Task A", "Task B"
    note "Waits for both Task A and Task B"
