package web

import (
	"encoding/json"
	"flowapp/internal/engine"
	"flowapp/internal/store"
	"html/template"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

type Handler struct {
	store     *store.Store
	templates *template.Template
}

func New(s *store.Store, tmplGlob string) (*Handler, error) {
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"lower":               strings.ToLower,
		"dueLabel":            func(s *engine.StepState) string { return s.DueLabel() },
		"isOverdue":           func(s *engine.StepState) bool { return s.IsOverdue() },
		"requiredListBlocked": func(s *engine.StepState) bool { return s.RequiredListBlocked() },
		"join":                strings.Join,
		"isReady":             func(s *engine.StepState) bool { return s.Status == engine.StatusReady },
		"isAsk":               func(s *engine.StepState) bool { return s.Status == engine.StatusAsk },
		"isGate":              func(s *engine.StepState) bool { return s.Status == engine.StatusGate },
		"isDone":              func(s *engine.StepState) bool { return s.Status == engine.StatusDone || s.Status == engine.StatusEnded },
		"isSkipped":           func(s *engine.StepState) bool { return s.Status == engine.StatusSkipped },
		"isPending":           func(s *engine.StepState) bool { return s.Status == engine.StatusPending },
		"statusIcon": func(s *engine.StepState) string {
			switch s.Status {
			case engine.StatusDone, engine.StatusEnded:
				return "✓"
			case engine.StatusSkipped:
				return "–"
			case engine.StatusReady:
				return "▶"
			case engine.StatusAsk:
				return "?"
			case engine.StatusGate:
				return "🔑"
			default:
				return "○"
			}
		},
		"iterate": func(n int) []int {
			r := make([]int, n)
			for i := range r {
				r[i] = i
			}
			return r
		},
		"not": func(b bool) bool { return !b },
		"visibleNeeds": func(needs []string) string {
			var out []string
			for _, n := range needs {
				if n != "__ask_target__" {
					out = append(out, n)
				}
			}
			return strings.Join(out, ", ")
		},
		"slice": func(a ...string) []string { return a },
	}).ParseGlob(tmplGlob)
	if err != nil {
		return nil, err
	}
	return &Handler{store: s, templates: tmpl}, nil
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /", h.board)
	mux.HandleFunc("GET /builder", h.builder)
	mux.HandleFunc("GET /instance/{id}", h.instanceDetail)
	mux.HandleFunc("POST /instance", h.createInstance)
	mux.HandleFunc("POST /instance/{id}/edit", h.editInstance)
	mux.HandleFunc("POST /instance/{id}/step", h.advanceStep)
	mux.HandleFunc("POST /instance/{id}/ask", h.answerAsk)
	mux.HandleFunc("POST /instance/{id}/clone", h.cloneInstance)
	mux.HandleFunc("POST /instance/{id}/comment", h.addComment)
	mux.HandleFunc("POST /instance/{id}/delete", h.deleteInstance)
	mux.HandleFunc("POST /instance/{id}/listitem/toggle", h.toggleListItem)
	mux.HandleFunc("POST /instance/{id}/listitem/add", h.addListItem)
	mux.HandleFunc("POST /instance/{id}/listitem/checkall", h.checkAllListItems)
	// external gate approval
	mux.HandleFunc("GET /debug/{id}", h.debug)
	mux.HandleFunc("GET /approve/{token}", h.approvalPage)
	mux.HandleFunc("POST /approve/{token}", h.approvalSubmit)
}

// --- board ---

type card struct {
	ID, Title, WorkflowName string
	Done, Total, Pct        int
	IsDone                  bool
	Priority                string
	HasOverdue              bool
	Labels                  []string
	CreatedAt               string
	UpdatedAt               string
}

type column struct {
	Title string
	Cards []card
}

type boardData struct {
	Columns        []column
	Definitions    []string
	AllLabels      []string
	FilterQ        string
	FilterPriority string
	FilterLabel    string
}

func (h *Handler) board(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	filterPriority := r.URL.Query().Get("priority")
	filterLabel := strings.ToLower(r.URL.Query().Get("label"))

	instances := h.store.Instances()
	sort.Slice(instances, func(i, j int) bool {
		pi, pj := priorityVal(instances[i].Priority), priorityVal(instances[j].Priority)
		if pi != pj {
			return pi > pj
		}
		return instances[i].CreatedAt.Before(instances[j].CreatedAt)
	})

	cols := map[string]*column{
		"Todo":        {Title: "Todo"},
		"In Progress": {Title: "In Progress"},
		"Done":        {Title: "Done"},
	}
	for _, inst := range instances {
		if q != "" && !strings.Contains(strings.ToLower(inst.Title+" "+inst.WorkflowName), q) {
			continue
		}
		if filterPriority != "" && inst.Priority != filterPriority {
			continue
		}
		if filterLabel != "" && !hasLabel(inst.Labels, filterLabel) {
			continue
		}
		done, total := inst.Progress()
		pct := 0
		if total > 0 {
			pct = done * 100 / total
		}
		c := card{
			ID: inst.ID, Title: inst.Title, WorkflowName: inst.WorkflowName,
			Done: done, Total: total, Pct: pct,
			IsDone:   string(inst.Status) == "done",
			Priority: inst.Priority, HasOverdue: inst.HasOverdue(),
			Labels:    inst.Labels,
			CreatedAt: inst.CreatedAt.Format("02.01.2006 15:04"),
			UpdatedAt: inst.UpdatedAt.Format("02.01.2006 15:04"),
		}
		switch {
		case string(inst.Status) == "done":
			cols["Done"].Cards = append(cols["Done"].Cards, c)
		case done == 0:
			cols["Todo"].Cards = append(cols["Todo"].Cards, c)
		default:
			cols["In Progress"].Cards = append(cols["In Progress"].Cards, c)
		}
	}

	defs := h.store.Definitions()
	var defNames []string
	for _, d := range defs {
		defNames = append(defNames, d.Name)
	}
	sort.Strings(defNames)
	allLabels := h.store.AllLabels()
	sort.Strings(allLabels)

	h.render(w, "board.html", boardData{
		Columns:     []column{*cols["Todo"], *cols["In Progress"], *cols["Done"]},
		Definitions: defNames, AllLabels: allLabels,
		FilterQ: r.URL.Query().Get("q"), FilterPriority: filterPriority, FilterLabel: filterLabel,
	})
}

func (h *Handler) builder(w http.ResponseWriter, r *http.Request) {
	h.render(w, "builder.html", nil)
}

func (h *Handler) instanceDetail(w http.ResponseWriter, r *http.Request) {
	inst, ok := h.store.Instance(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	h.render(w, "instance.html", inst)
}

func (h *Handler) createInstance(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	wfName := r.FormValue("workflow")
	title := r.FormValue("title")
	if title == "" {
		title = wfName
	}
	if _, err := h.store.CreateInstance(wfName, title, r.FormValue("priority")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) editInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.ParseForm()
	if err := h.store.UpdateInstance(id, strings.TrimSpace(r.FormValue("title")),
		r.FormValue("priority"), r.FormValue("labels")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/instance/"+id, http.StatusSeeOther)
}

func (h *Handler) advanceStep(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.ParseForm()
	if err := h.store.AdvanceStep(id, r.FormValue("step")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/instance/"+id, http.StatusSeeOther)
}

func (h *Handler) answerAsk(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.ParseForm()
	idx, _ := strconv.Atoi(r.FormValue("choice"))
	if err := h.store.AnswerAsk(id, r.FormValue("step"), idx); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/instance/"+id, http.StatusSeeOther)
}

func (h *Handler) cloneInstance(w http.ResponseWriter, r *http.Request) {
	inst, err := h.store.CloneInstance(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/instance/"+inst.ID, http.StatusSeeOther)
}

func (h *Handler) addComment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.ParseForm()
	if text := strings.TrimSpace(r.FormValue("text")); text != "" {
		h.store.AddComment(id, text)
	}
	http.Redirect(w, r, "/instance/"+id, http.StatusSeeOther)
}

func (h *Handler) deleteInstance(w http.ResponseWriter, r *http.Request) {
	h.store.DeleteInstance(r.PathValue("id"))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) toggleListItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.ParseForm()
	if err := h.store.ToggleListItem(id, r.FormValue("step"), r.FormValue("item_id")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/instance/"+id, http.StatusSeeOther)
}

func (h *Handler) addListItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.ParseForm()
	if text := strings.TrimSpace(r.FormValue("text")); text != "" {
		h.store.AddListItem(id, r.FormValue("step"), text)
	}
	http.Redirect(w, r, "/instance/"+id, http.StatusSeeOther)
}

func (h *Handler) checkAllListItems(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.ParseForm()
	h.store.CheckAllListItems(id, r.FormValue("step"))
	http.Redirect(w, r, "/instance/"+id, http.StatusSeeOther)
}

// --- Gate approval pages ---

type approvalData struct {
	Token    string
	Step     *engine.StepState
	Instance *engine.Instance
	Done     bool
	Error    string
}

func (h *Handler) approvalPage(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	inst, step := h.store.FindByToken(token)
	if inst == nil || step == nil {
		h.render(w, "approve.html", approvalData{Token: token, Error: "Link not found or already used."})
		return
	}
	if step.GateUsed {
		h.render(w, "approve.html", approvalData{Token: token, Done: true, Step: step, Instance: inst})
		return
	}
	h.render(w, "approve.html", approvalData{Token: token, Step: step, Instance: inst})
}

func (h *Handler) approvalSubmit(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	r.ParseForm()
	idx, _ := strconv.Atoi(r.FormValue("choice"))
	inst, step, err := h.store.RedeemGate(token, idx)
	if err != nil {
		h.render(w, "approve.html", approvalData{Token: token, Error: err.Error()})
		return
	}
	h.render(w, "approve.html", approvalData{Token: token, Done: true, Step: step, Instance: inst})
}

func (h *Handler) debug(w http.ResponseWriter, r *http.Request) {
	inst, ok := h.store.Instance(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(inst)
}

func (h *Handler) render(w http.ResponseWriter, name string, data any) {
	var buf strings.Builder
	if err := h.templates.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(buf.String()))
}

func priorityVal(p string) int {
	switch p {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	}
	return 2
}

func hasLabel(labels []string, filter string) bool {
	for _, l := range labels {
		if strings.ToLower(l) == filter {
			return true
		}
	}
	return false
}
