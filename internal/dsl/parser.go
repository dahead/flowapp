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

func trimQuotes(s string) string {
	return strings.Trim(s, "\"")
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
			wf.Name = trimQuotes(strings.Join(args, " "))

		case "priority":
			if len(args) == 0 {
				return nil, fmt.Errorf("line %d: 'priority' requires low/medium/high", lineNum)
			}
			wf.Priority = strings.ToLower(args[0])

		case "label":
			if len(args) == 0 {
				return nil, fmt.Errorf("line %d: 'label' requires a name", lineNum)
			}
			wf.Labels = append(wf.Labels, strings.ToLower(trimQuotes(strings.Join(args, " "))))

		case "section":
			if len(args) == 0 {
				return nil, fmt.Errorf("line %d: 'section' requires a name", lineNum)
			}
			currentSection = &Section{Name: trimQuotes(strings.Join(args, " "))}
			wf.Sections = append(wf.Sections, currentSection)
			currentStep = nil

		case "step":
			if currentSection == nil {
				return nil, fmt.Errorf("line %d: 'step' must be inside a section", lineNum)
			}
			if len(args) == 0 {
				return nil, fmt.Errorf("line %d: 'step' requires a name", lineNum)
			}
			currentStep = &Step{Name: trimQuotes(strings.Join(args, " "))}
			currentSection.Steps = append(currentSection.Steps, currentStep)

		case "note":
			if currentStep == nil {
				return nil, fmt.Errorf("line %d: 'note' must be inside a step", lineNum)
			}
			currentStep.Note = trimQuotes(strings.Join(args, " "))

		case "notify":
			if currentStep == nil {
				return nil, fmt.Errorf("line %d: 'notify' must be inside a step", lineNum)
			}
			currentStep.Notify = trimQuotes(strings.Join(args, " "))

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
			text := trimQuotes(strings.Join(args, " "))
			last := strings.ToLower(trimQuotes(args[len(args)-1]))
			if last == "optional" {
				required = false
				text = trimQuotes(strings.Join(args[:len(args)-1], " "))
			} else if last == "required" {
				text = trimQuotes(strings.Join(args[:len(args)-1], " "))
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

// parseNameList parses comma-separated names from tokens.
// It handles quoted names that might contain commas.
func parseNameList(tokens []string) []string {
	raw := strings.Join(tokens, " ")
	var names []string
	var cur strings.Builder
	inQ := false
	for _, ch := range raw {
		switch {
		case ch == '"':
			inQ = !inQ
			// We don't write the quote to the name itself
		case ch == ',' && !inQ:
			name := strings.TrimSpace(cur.String())
			if name != "" {
				names = append(names, name)
			}
			cur.Reset()
		default:
			cur.WriteRune(ch)
		}
	}
	name := strings.TrimSpace(cur.String())
	if name != "" {
		names = append(names, name)
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
			cur.WriteRune(ch) // Keep the quote
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
