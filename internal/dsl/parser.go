package dsl

import (
	"bufio"
	"fmt"
	"strings"
)

// Workflow is the parsed representation of a .workflow file.
type Workflow struct {
	Name     string
	Priority string
	Labels   []string
	Vars     []string // variable names prompted at instance creation (e.g. "Employee Name")
	Sections []*Section
}

// Section groups a set of related steps under a named heading.
type Section struct {
	Name  string
	Steps []*Step
}

// Step is a single task within a section.
type Step struct {
	Name      string
	Note      string
	Notify    string   // email address to notify when this step fires
	Assign    string   // assign expression: "user:<n>", "role:<r>", or bare email
	Schedule  string   // activation schedule: absolute "2025-12-01" or relative "+3d"
	Due       string   // time-to-complete deadline: "2h", "3d", "1w"
	Needs     []string // AND-join: all listed steps must be done before this activates
	ListItems []ListItem
	Ask       *AskDef // nil if this is not a branching step
	Gate      bool    // if true, step waits for external approval via a token link
	Ends      bool    // if true, completing this step terminates the workflow
}

// AskDef defines a branching decision within a step.
// Each button label in the UI corresponds to a routing target step name.
type AskDef struct {
	Question string
	Targets  []string // ordered: button[i] → target step name[i]
}

// ListItem is a predefined checklist entry declared in the workflow file.
type ListItem struct {
	Text     string
	Required bool // if true, the item must be checked before the step can be advanced
}

// Parse reads a workflow definition from its DSL text representation and returns
// the parsed Workflow, or an error with a line number if the syntax is invalid.
func Parse(input string) (*Workflow, error) {
	scanner := bufio.NewScanner(strings.NewReader(input))
	wf := &Workflow{}
	var currentSection *Section
	var currentStep *Step
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue // skip blank lines and comments
		}
		tokens := tokenize(trimmed)
		if len(tokens) == 0 {
			continue
		}
		keyword := strings.ToLower(tokens[0])
		args := tokens[1:]

		switch keyword {
		case "workflow":
			if len(args) == 0 {
				return nil, fmt.Errorf("line %d: 'workflow' requires a name", lineNum)
			}
			wf.Name = strings.Join(args, " ")

		case "priority":
			if len(args) == 0 {
				return nil, fmt.Errorf("line %d: 'priority' requires low/medium/high", lineNum)
			}
			wf.Priority = strings.ToLower(args[0])

		case "label":
			if len(args) == 0 {
				return nil, fmt.Errorf("line %d: 'label' requires a name", lineNum)
			}
			wf.Labels = append(wf.Labels, strings.ToLower(strings.Join(args, " ")))

		case "var":
			if len(args) == 0 {
				return nil, fmt.Errorf("line %d: 'var' requires a variable name", lineNum)
			}
			wf.Vars = append(wf.Vars, strings.Join(args, " "))

		case "section":
			if len(args) == 0 {
				return nil, fmt.Errorf("line %d: 'section' requires a name", lineNum)
			}
			currentSection = &Section{Name: strings.Join(args, " ")}
			wf.Sections = append(wf.Sections, currentSection)
			currentStep = nil

		case "step":
			if currentSection == nil {
				return nil, fmt.Errorf("line %d: 'step' must be inside a section", lineNum)
			}
			if len(args) == 0 {
				return nil, fmt.Errorf("line %d: 'step' requires a name", lineNum)
			}
			currentStep = &Step{Name: strings.Join(args, " ")}
			currentSection.Steps = append(currentSection.Steps, currentStep)

		case "note":
			if currentStep == nil {
				return nil, fmt.Errorf("line %d: 'note' must be inside a step", lineNum)
			}
			currentStep.Note = strings.Join(args, " ")

		case "assign":
			if currentStep == nil {
				return nil, fmt.Errorf("line %d: 'assign' must be inside a step", lineNum)
			}
			currentStep.Assign = strings.Join(args, " ")

		case "schedule":
			if currentStep == nil {
				return nil, fmt.Errorf("line %d: 'schedule' must be inside a step", lineNum)
			}
			if len(args) == 0 {
				return nil, fmt.Errorf("line %d: 'schedule' requires a date or duration", lineNum)
			}
			currentStep.Schedule = strings.Join(args, " ")

		case "due":
			if currentStep == nil {
				return nil, fmt.Errorf("line %d: 'due' must be inside a step", lineNum)
			}
			currentStep.Due = strings.Join(args, " ")

		case "notify":
			if currentStep == nil {
				return nil, fmt.Errorf("line %d: 'notify' must be inside a step", lineNum)
			}
			currentStep.Notify = strings.Join(args, " ")

		case "item", "- ":
			// checklist item: item "Text" or item! "Required text"
			if currentStep == nil {
				return nil, fmt.Errorf("line %d: 'item' must be inside a step", lineNum)
			}
			required := strings.HasSuffix(keyword, "!")
			if len(args) == 0 {
				return nil, fmt.Errorf("line %d: 'item' requires text", lineNum)
			}
			currentStep.ListItems = append(currentStep.ListItems, ListItem{
				Text: strings.Join(args, " "), Required: required,
			})

		case "item!":
			if currentStep == nil {
				return nil, fmt.Errorf("line %d: 'item!' must be inside a step", lineNum)
			}
			if len(args) == 0 {
				return nil, fmt.Errorf("line %d: 'item!' requires text", lineNum)
			}
			currentStep.ListItems = append(currentStep.ListItems, ListItem{
				Text: strings.Join(args, " "), Required: true,
			})

		case "needs":
			if currentStep == nil {
				return nil, fmt.Errorf("line %d: 'needs' must be inside a step", lineNum)
			}
			// parse comma-separated quoted names: needs "A", "B", "C"
			currentStep.Needs = parseNameList(args)

		case "ask":
			if currentStep == nil {
				return nil, fmt.Errorf("line %d: 'ask' must be inside a step", lineNum)
			}
			ask, err := parseAsk(args, lineNum)
			if err != nil {
				return nil, err
			}
			currentStep.Ask = ask

		case "gate":
			if currentStep == nil {
				return nil, fmt.Errorf("line %d: 'gate' must be inside a step", lineNum)
			}
			currentStep.Gate = true

		case "ends":
			if currentStep == nil {
				return nil, fmt.Errorf("line %d: 'ends' must be inside a step", lineNum)
			}
			currentStep.Ends = true

		default:
			// unknown keywords are silently ignored to allow forward compatibility
		}
	}
	return wf, scanner.Err()
}

// parseAsk parses the ask directive arguments.
// Expected format: "Question?" -> "Target1", "Target2"
func parseAsk(args []string, lineNum int) (*AskDef, error) {
	if len(args) < 3 {
		return nil, fmt.Errorf("line %d: 'ask' requires: \"Question?\" -> \"Target1\", ...", lineNum)
	}
	question := strings.Trim(args[0], `"`)
	// args[1] should be "->"
	targets := parseNameList(args[2:])
	if len(targets) == 0 {
		return nil, fmt.Errorf("line %d: 'ask' requires at least one target", lineNum)
	}
	return &AskDef{Question: question, Targets: targets}, nil
}

// parseNameList extracts quoted step names from a token slice,
// stripping surrounding quotes and ignoring commas and the "->" arrow.
func parseNameList(tokens []string) []string {
	var out []string
	for _, t := range tokens {
		t = strings.TrimRight(t, ",")
		t = strings.Trim(t, `"`)
		if t != "" && t != "->" {
			out = append(out, t)
		}
	}
	return out
}

// tokenize splits a DSL line into tokens, treating double-quoted strings as
// single tokens (preserving internal spaces).
func tokenize(line string) []string {
	var tokens []string
	var cur strings.Builder
	inQuote := false
	for _, ch := range line {
		switch {
		case ch == '"':
			inQuote = !inQuote
			cur.WriteRune(ch)
		case (ch == ' ' || ch == '\t') && !inQuote:
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(ch)
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

// DetectCycles returns an error if the workflow contains circular needs dependencies
// (which would cause the engine to deadlock). Uses depth-first search.
func DetectCycles(wf *Workflow) error {
	// build adjacency map: step name → steps it depends on
	deps := map[string][]string{}
	for _, sec := range wf.Sections {
		for _, step := range sec.Steps {
			deps[step.Name] = step.Needs
		}
	}

	visited := map[string]bool{}
	inStack := map[string]bool{}

	var visit func(name string) error
	visit = func(name string) error {
		if inStack[name] {
			return fmt.Errorf("cycle detected involving step '%s'", name)
		}
		if visited[name] {
			return nil
		}
		visited[name] = true
		inStack[name] = true
		for _, dep := range deps[name] {
			if dep == "__ask_target__" {
				continue
			}
			if err := visit(dep); err != nil {
				return err
			}
		}
		inStack[name] = false
		return nil
	}

	for name := range deps {
		if err := visit(name); err != nil {
			return err
		}
	}
	return nil
}
