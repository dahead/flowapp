package dsl

import (
	"bufio"
	"fmt"
	"strings"
)

type Workflow struct {
	Name     string
	Priority string
	Labels   []string
	Vars     []string // variable names to prompt on instance creation
	Sections []*Section
}

type Section struct {
	Name  string
	Steps []*Step
}

type Step struct {
	Name      string
	Note      string
	Notify    string
	Assign    string
	Schedule  string // absolute date "2025-12-01" or relative "+3d"
	Due       string
	Needs     []string // AND-join: all must be done
	ListItems []ListItem
	Ask       *AskDef // nil if not an ask step
	Gate      bool    // waits for external approval via token link
	Ends      bool    // terminal step, no successors
}

type AskDef struct {
	Question string
	Targets  []string // ordered: button[0] -> target[0]
}

type ListItem struct {
	Text     string
	Required bool
}

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
			continue
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

		case "notify":
			if currentStep == nil {
				return nil, fmt.Errorf("line %d: 'notify' must be inside a step", lineNum)
			}
			currentStep.Notify = strings.Join(args, " ")

		case "due":
			if currentStep == nil {
				return nil, fmt.Errorf("line %d: 'due' must be inside a step", lineNum)
			}
			if len(args) == 0 {
				return nil, fmt.Errorf("line %d: 'due' requires a duration", lineNum)
			}
			currentStep.Due = strings.ToLower(args[0])

		case "needs":
			if currentStep == nil {
				return nil, fmt.Errorf("line %d: 'needs' must be inside a step", lineNum)
			}
			// parse comma-separated quoted names: needs "A", "B", "C"
			currentStep.Needs = parseNameList(args)

		case "list":
			if currentStep == nil {
				return nil, fmt.Errorf("line %d: 'list' must be inside a step", lineNum)
			}
			if len(args) == 0 {
				return nil, fmt.Errorf("line %d: 'list' requires item text", lineNum)
			}
			required := true
			text := strings.Join(args, " ")
			last := strings.ToLower(args[len(args)-1])
			if last == "optional" {
				required = false
				text = strings.Join(args[:len(args)-1], " ")
			} else if last == "required" {
				text = strings.Join(args[:len(args)-1], " ")
			}
			currentStep.ListItems = append(currentStep.ListItems, ListItem{Text: text, Required: required})

		case "ask":
			if currentStep == nil {
				return nil, fmt.Errorf("line %d: 'ask' must be inside a step", lineNum)
			}
			// parse: "Question?" -> "Target1", "Target2"
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
			return nil, fmt.Errorf("line %d: unknown keyword '%s'", lineNum, keyword)
		}
	}

	if wf.Name == "" {
		return nil, fmt.Errorf("workflow must have a name")
	}
	if wf.Priority == "" {
		wf.Priority = "medium"
	}
	return wf, scanner.Err()
}

// parseAsk parses: "Question?" -> "Target1", "Target2"
func parseAsk(args []string, lineNum int) (*AskDef, error) {
	// rejoin and split on "->"
	raw := strings.Join(args, " ")
	parts := strings.SplitN(raw, "->", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("line %d: 'ask' requires format: \"Question?\" -> \"TargetA\", \"TargetB\"", lineNum)
	}
	question := strings.Trim(strings.TrimSpace(parts[0]), "\"")
	targetTokens := tokenize(parts[1])
	targets := parseNameList(targetTokens)
	if len(targets) == 0 {
		return nil, fmt.Errorf("line %d: 'ask' requires at least one target", lineNum)
	}
	return &AskDef{Question: question, Targets: targets}, nil
}

// parseNameList parses comma-separated quoted names from tokens
func parseNameList(tokens []string) []string {
	// re-join and split by comma, strip quotes
	raw := strings.Join(tokens, " ")
	parts := strings.Split(raw, ",")
	var names []string
	for _, p := range parts {
		name := strings.Trim(strings.TrimSpace(p), "\"")
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func tokenize(line string) []string {
	var tokens []string
	var cur strings.Builder
	inQ := false
	for _, ch := range line {
		switch {
		case ch == '"':
			inQ = !inQ
			// don't write the quote — tokens come out unquoted
		case (ch == ' ' || ch == '\t') && !inQ:
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

// DetectCycles returns an error if the workflow has circular needs dependencies.
func DetectCycles(wf *Workflow) error {
	// build adjacency: step → steps it needs
	deps := map[string][]string{}
	all := map[string]bool{}
	for _, sec := range wf.Sections {
		for _, s := range sec.Steps {
			all[s.Name] = true
			deps[s.Name] = s.Needs
		}
	}
	// DFS cycle detection
	const (
		white, grey, black = 0, 1, 2
	)
	color := map[string]int{}
	var path []string
	var visit func(name string) error
	visit = func(name string) error {
		color[name] = grey
		path = append(path, name)
		for _, dep := range deps[name] {
			if !all[dep] {
				continue
			}
			switch color[dep] {
			case grey:
				// find cycle start
				for i, n := range path {
					if n == dep {
						return fmt.Errorf("circular dependency: %s", strings.Join(append(path[i:], dep), " → "))
					}
				}
				return fmt.Errorf("circular dependency involving %q", dep)
			case white:
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		path = path[:len(path)-1]
		color[name] = black
		return nil
	}
	for name := range all {
		if color[name] == white {
			if err := visit(name); err != nil {
				return err
			}
		}
	}
	return nil
}
