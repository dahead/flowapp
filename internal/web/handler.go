package web

import (
	"encoding/json"
	"flowapp/internal/auth"
	"flowapp/internal/engine"
	"flowapp/internal/store"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Handler holds all HTTP handler state: the data store, user store,
// compiled templates, and the login rate limiter.
type Handler struct {
	store     *store.Store
	users     *auth.UserStore
	templates *template.Template
	loginRL   *auth.RateLimiter
}

type ctxKey string

const ctxUserKey ctxKey = "user"

// New creates a Handler, compiling all HTML templates from the given glob pattern
// and registering custom template functions used by the views.
func New(s *store.Store, users *auth.UserStore, tmplGlob string) (*Handler, error) {
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
		// statusIcon returns a single character representing the step's current status.
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
		// iterate produces a slice [0, 1, ..., n-1] for range loops in templates.
		"iterate": func(n int) []int {
			r := make([]int, n)
			for i := range r {
				r[i] = i
			}
			return r
		},
		"not":   func(b bool) bool { return !b },
		"slice": func(a ...string) []string { return a },
		// visibleNeeds filters out the internal "__ask_target__" sentinel from the needs list.
		"visibleNeeds": func(needs []string) string {
			var out []string
			for _, n := range needs {
				if n != "__ask_target__" {
					out = append(out, n)
				}
			}
			return strings.Join(out, ", ")
		},
		"hasPriority": func(d boardData, p string) bool { return containsStr(d.FilterPriorities, p) },
		"hasLabel":    func(d boardData, l string) bool { return containsStr(d.FilterLabels, strings.ToLower(l)) },
		"hasActiveFilters": func(d boardData) bool {
			return d.FilterQ != "" || len(d.FilterPriorities) > 0 || len(d.FilterLabels) > 0 || d.FilterDue != "" || d.FilterCreated != "" || d.FilterAssign != ""
		},
		// roleLabel returns a display-friendly label for a user role.
		"roleLabel": func(r auth.Role) string {
			switch r {
			case auth.RoleAdmin:
				return "Admin"
			case auth.RoleUser:
				return "User"
			case auth.RoleViewer:
				return "Viewer"
			}
			return string(r)
		},
		"isAdmin": func(u *auth.User) bool { return u != nil && u.CanAdmin() },
		// initial returns the first rune of a string, used for avatar initials.
		"initial": func(s string) string {
			if len(s) == 0 {
				return "?"
			}
			r := []rune(s)
			return string(r[0])
		},
		"add": func(a, b int) int { return a + b },
		"pct": func(done, total int) int {
			if total == 0 {
				return 0
			}
			return done * 100 / total
		},
		// ringOffset computes the SVG stroke-dashoffset for a progress ring.
		// circumference = 2 * π * 22 ≈ 138.2
		"ringOffset": func(done, total int) float64 {
			const circ = 138.2
			if total == 0 {
				return circ
			}
			return circ - (float64(done)/float64(total))*circ
		},
		"canWrite": func(u *auth.User) bool { return u != nil && u.CanWrite() },
		// canDoStep returns true if the user may act on a step.
		// Admins and steps without an assign field are always allowed.
		// Otherwise the user must match the assign expression.
		"canDoStep": func(u *auth.User, s *engine.StepState) bool {
			if u == nil || !u.CanWrite() {
				return false
			}
			if u.CanAdmin() || s.Assign == "" {
				return true
			}
			if s.Assign == "user:"+u.Name || s.Assign == "user:"+u.Email || s.Assign == u.Name || s.Assign == u.Email {
				return true
			}
			if strings.HasPrefix(s.Assign, "role:") {
				roleName := strings.TrimPrefix(s.Assign, "role:")
				return slices.Contains(u.AppRoles, roleName)
			}
			return false
		},
	}).ParseGlob(tmplGlob)
	if err != nil {
		return nil, err
	}
	return &Handler{store: s, users: users, templates: tmpl, loginRL: auth.NewRateLimiter()}, nil
}

// ── Auth middleware ────────────────────────────────────────────────────────────

// currentUser reads the session cookie and returns the authenticated user,
// or nil if the session is missing, invalid, or the user is inactive.
func (h *Handler) currentUser(r *http.Request) *auth.User {
	id, err := auth.GetSessionUserID(r)
	if err != nil {
		return nil
	}
	u, ok := h.users.GetByID(id)
	if !ok || !u.Active {
		return nil
	}
	return u
}

// requireAuth wraps a handler to redirect unauthenticated requests to /login.
func (h *Handler) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := h.currentUser(r)
		if u == nil {
			http.Redirect(w, r, "/login?next="+r.URL.RequestURI(), http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

// requireWrite wraps a handler to deny access to users without write permission.
func (h *Handler) requireWrite(next http.HandlerFunc) http.HandlerFunc {
	return h.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		u := h.currentUser(r)
		if !u.CanWrite() {
			h.renderError(w, r, "Keine Berechtigung für diese Aktion.", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

// requireAdmin wraps a handler to deny access to non-admin users.
func (h *Handler) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return h.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		u := h.currentUser(r)
		if !u.CanAdmin() {
			h.renderError(w, r, "Nur Admins können diese Seite aufrufen.", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

// ── Routes ────────────────────────────────────────────────────────────────────

// RegisterRoutes registers all HTTP routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// first-run setup (only accessible before any users exist)
	mux.HandleFunc("GET /setup", h.setupPage)
	mux.HandleFunc("POST /setup", h.setupSubmit)
	// authentication
	mux.HandleFunc("GET /login", h.loginPage)
	mux.HandleFunc("POST /login", h.loginSubmit)
	mux.HandleFunc("POST /logout", h.logout)
	mux.HandleFunc("GET /profile", h.requireAuth(h.profilePage))
	mux.HandleFunc("POST /profile", h.requireAuth(h.profileSave))
	// main app (auth required)
	mux.HandleFunc("GET /", h.requireAuth(h.board))
	mux.HandleFunc("GET /builder", h.requireAuth(h.builder))
	mux.HandleFunc("GET /archive", h.requireAuth(h.archive))
	mux.HandleFunc("GET /instance/{id}", h.requireAuth(h.instanceDetail))
	mux.HandleFunc("POST /instance", h.requireWrite(h.createInstance))
	mux.HandleFunc("GET /instance/new/{workflow}", h.requireWrite(h.newInstancePrompt))
	mux.HandleFunc("POST /instance/{id}/edit", h.requireWrite(h.editInstance))
	mux.HandleFunc("POST /instance/{id}/step", h.requireWrite(h.advanceStep))
	mux.HandleFunc("POST /instance/{id}/ask", h.requireWrite(h.answerAsk))
	mux.HandleFunc("POST /instance/{id}/clone", h.requireWrite(h.cloneInstance))
	mux.HandleFunc("POST /instance/{id}/comment", h.requireWrite(h.addComment))
	mux.HandleFunc("POST /instance/{id}/stepcomment", h.requireWrite(h.addStepComment))
	mux.HandleFunc("POST /instance/{id}/delete", h.requireWrite(h.deleteInstance))
	mux.HandleFunc("POST /instance/{id}/archive", h.requireWrite(h.archiveInstance))
	mux.HandleFunc("POST /instance/{id}/listitem/toggle", h.requireWrite(h.toggleListItem))
	mux.HandleFunc("POST /instance/{id}/listitem/add", h.requireWrite(h.addListItem))
	mux.HandleFunc("POST /instance/{id}/listitem/checkall", h.requireWrite(h.checkAllListItems))
	mux.HandleFunc("POST /reorder", h.requireWrite(h.reorder))
	// static assets
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("internal/web/static"))))
	// public API
	mux.HandleFunc("GET /api/workflows", h.apiWorkflows)
	// gate approval — no login required; the token is the credential
	mux.HandleFunc("GET /approve/{token}", h.approvalPage)
	mux.HandleFunc("POST /approve/{token}", h.approvalSubmit)
	// admin
	mux.HandleFunc("GET /admin/users", h.requireAdmin(h.adminUsers))
	mux.HandleFunc("POST /admin/users", h.requireAdmin(h.adminCreateUser))
	mux.HandleFunc("POST /admin/users/{id}/edit", h.requireAdmin(h.adminEditUser))
	mux.HandleFunc("POST /admin/users/{id}/delete", h.requireAdmin(h.adminDeleteUser))
	mux.HandleFunc("POST /admin/users/{id}/password", h.requireAdmin(h.adminResetPassword))
}

// ── Setup ─────────────────────────────────────────────────────────────────────

// setupPage renders the first-run admin creation form.
// Redirects to / if users already exist.
func (h *Handler) setupPage(w http.ResponseWriter, r *http.Request) {
	if !h.users.Empty() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	h.render(w, r, "setup.html", map[string]string{"Error": ""})
}

// setupSubmit handles the first-run admin creation form submission.
func (h *Handler) setupSubmit(w http.ResponseWriter, r *http.Request) {
	if !h.users.Empty() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	r.ParseForm()
	email := strings.TrimSpace(r.FormValue("email"))
	name := strings.TrimSpace(r.FormValue("name"))
	password := r.FormValue("password")
	confirm := r.FormValue("confirm")
	if email == "" || name == "" || password == "" {
		h.render(w, r, "setup.html", map[string]string{"Error": "Alle Felder ausfüllen."})
		return
	}
	if password != confirm {
		h.render(w, r, "setup.html", map[string]string{"Error": "Passwörter stimmen nicht überein."})
		return
	}
	if len(password) < 8 {
		h.render(w, r, "setup.html", map[string]string{"Error": "Passwort muss mindestens 8 Zeichen haben."})
		return
	}
	u, err := h.users.Create(email, name, password, auth.RoleAdmin)
	if err != nil {
		h.render(w, r, "setup.html", map[string]string{"Error": err.Error()})
		return
	}
	auth.SetSession(w, u.ID)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// ── Login / Logout ────────────────────────────────────────────────────────────

// loginPage renders the login form.
// Redirects to / if already authenticated, or to /setup if no users exist yet.
func (h *Handler) loginPage(w http.ResponseWriter, r *http.Request) {
	if !h.users.Empty() && h.currentUser(r) != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if h.users.Empty() {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	h.render(w, r, "login.html", map[string]string{"Error": "", "Next": r.URL.Query().Get("next")})
}

// loginSubmit validates credentials and creates a session on success.
// Rate-limited by loginRL to prevent brute-force attacks.
func (h *Handler) loginSubmit(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	next := r.FormValue("next")
	if next == "" {
		next = "/"
	}
	if ok, wait := h.loginRL.Allow(r); !ok {
		mins := int(wait.Minutes()) + 1
		h.render(w, r, "login.html", map[string]string{
			"Error": fmt.Sprintf("Zu viele Fehlversuche. Bitte %d Minuten warten.", mins),
			"Next":  next,
		})
		return
	}
	u, err := h.users.Authenticate(email, password)
	if err != nil {
		log.Printf("[web] login failed for %s: %v", email, err)
		h.render(w, r, "login.html", map[string]string{"Error": "Ungültige E-Mail oder Passwort.", "Next": next})
		return
	}
	log.Printf("[web] login successful for %s (%s)", u.ID, email)
	h.loginRL.Reset(r) // clear rate-limit counter on successful login
	auth.SetSession(w, u.ID)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// logout clears the session cookie and redirects to /login.
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if u := h.currentUser(r); u != nil {
		log.Printf("[web] logout for user %s (%s)", u.ID, u.Email)
	}
	auth.ClearSession(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// ── Board ─────────────────────────────────────────────────────────────────────

// card is the view model for a single instance card on the Kanban board.
type card struct {
	ID, Title, WorkflowName string
	Done, Total, Pct        int
	IsDone                  bool
	Priority                string
	HasOverdue              bool
	Labels                  []string
	CreatedAt, UpdatedAt    string
}

// column groups cards under a Kanban column heading.
type column struct {
	Title string
	Cards []card
}

// boardData is the view model passed to the board template.
type boardData struct {
	Columns          []column
	Definitions      []string
	AllLabels        []string
	FilterQ          string
	FilterPriorities []string
	FilterLabels     []string
	FilterDue        string
	FilterCreated    string
	FilterSort       string
	FilterAssign     string
	Flash            string
	CurrentUser      *auth.User
}

// board renders the main Kanban board, applying any active filters from query parameters.
func (h *Handler) board(w http.ResponseWriter, r *http.Request) {
	u := h.currentUser(r)
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	filterPriorities := r.URL.Query()["priority"]
	filterLabels := r.URL.Query()["label"]
	filterDue := r.URL.Query().Get("due")
	filterCreated := r.URL.Query().Get("created")
	filterSort := r.URL.Query().Get("sort")
	filterAssign := r.URL.Query().Get("assign") // "me" or an explicit assign expression
	for i, l := range filterLabels {
		filterLabels[i] = strings.ToLower(l)
	}

	instances := h.store.Instances()
	sort.Slice(instances, func(i, j int) bool {
		a, b := instances[i], instances[j]
		switch filterSort {
		case "updated":
			return a.UpdatedAt.After(b.UpdatedAt)
		case "priority":
			pa, pb := priorityVal(a.Priority), priorityVal(b.Priority)
			if pa != pb {
				return pa > pb
			}
			return a.UpdatedAt.After(b.UpdatedAt)
		case "created":
			return a.CreatedAt.After(b.CreatedAt)
		default: // position
			if a.Position != b.Position {
				return a.Position < b.Position
			}
			pa, pb := priorityVal(a.Priority), priorityVal(b.Priority)
			if pa != pb {
				return pa > pb
			}
			return a.CreatedAt.Before(b.CreatedAt)
		}
	})

	now := time.Now()
	cols := map[string]*column{
		"Todo":        {Title: "Todo"},
		"In Progress": {Title: "In Progress"},
		"Done":        {Title: "Done"},
	}
	for _, inst := range instances {
		if q != "" && !strings.Contains(strings.ToLower(inst.Title+" "+inst.WorkflowName), q) {
			continue
		}
		if len(filterPriorities) > 0 && !containsStr(filterPriorities, inst.Priority) {
			continue
		}
		if len(filterLabels) > 0 && !hasAnyLabel(inst.Labels, filterLabels) {
			continue
		}
		if filterDue != "" && !matchDueFilter(inst, filterDue, now) {
			continue
		}
		if filterCreated != "" && !matchCreatedFilter(inst, filterCreated, now) {
			continue
		}
		if filterAssign != "" && !matchAssignFilter(inst, filterAssign, u) {
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

	flash := getFlash(w, r)
	h.render(w, r, "board.html", boardData{
		Columns:     []column{*cols["Todo"], *cols["In Progress"], *cols["Done"]},
		Definitions: defNames, AllLabels: allLabels,

		FilterQ: r.URL.Query().Get("q"), FilterPriorities: filterPriorities,
		FilterLabels: filterLabels, FilterDue: filterDue, FilterCreated: filterCreated,
		FilterSort: filterSort, FilterAssign: filterAssign,
		Flash: flash, CurrentUser: u,
	})
}

// addStepComment handles POST requests to append a comment to a step.
func (h *Handler) addStepComment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.ParseForm()
	step := r.FormValue("step")
	if text := strings.TrimSpace(r.FormValue("text")); text != "" {
		if err := h.store.AddStepComment(id, step, text); err != nil {
			flashError(w, r, err.Error())
			return
		}
	}
	http.Redirect(w, r, "/instance/"+id, http.StatusSeeOther)
}

// newInstancePrompt renders the new-instance form, which collects variable values
// if the workflow declares any vars. If there are no vars, the instance is created immediately.
func (h *Handler) newInstancePrompt(w http.ResponseWriter, r *http.Request) {
	wfName := r.PathValue("workflow")
	title := r.URL.Query().Get("title")
	priority := r.URL.Query().Get("priority")
	if title == "" {
		title = wfName
	}
	defs := h.store.Definitions()
	for _, d := range defs {
		if d.Name == wfName && len(d.Vars) > 0 {
			h.render(w, r, "new_instance.html", map[string]interface{}{
				"WorkflowName": wfName,
				"Title":        title,
				"Priority":     priority,
				"Vars":         d.Vars,
				"CurrentUser":  h.currentUser(r),
			})
			return
		}
	}
	// no vars — create directly without showing the prompt page
	if _, err := h.store.CreateInstance(wfName, title, priority); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// archive renders the archive page with optional text search filtering.
func (h *Handler) archive(w http.ResponseWriter, r *http.Request) {
	u := h.currentUser(r)
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	all := h.store.ArchivedInstances()
	var filtered []*engine.Instance
	for _, inst := range all {
		if q == "" || strings.Contains(strings.ToLower(inst.Title+" "+inst.WorkflowName), q) {
			filtered = append(filtered, inst)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].UpdatedAt.After(filtered[j].UpdatedAt)
	})
	h.render(w, r, "archive.html", map[string]interface{}{
		"Instances":   filtered,
		"FilterQ":     r.URL.Query().Get("q"),
		"CurrentUser": u,
	})
}

// builder renders the visual workflow builder page.
func (h *Handler) builder(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "builder.html", map[string]interface{}{"CurrentUser": h.currentUser(r)})
}

// ── Instance ──────────────────────────────────────────────────────────────────

// instanceData is the view model for the instance detail page.
type instanceData struct {
	*engine.Instance
	Flash       string
	CurrentUser *auth.User
}

// instanceDetail renders the detail view for a single workflow instance.
func (h *Handler) instanceDetail(w http.ResponseWriter, r *http.Request) {
	inst, ok := h.store.Instance(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	h.render(w, r, "instance.html", instanceData{inst, getFlash(w, r), h.currentUser(r)})
}

// createInstance handles the POST form that creates a new workflow instance.
// Any var_ prefixed form fields are applied as variable substitutions.
func (h *Handler) createInstance(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	wfName := r.FormValue("workflow")
	title := r.FormValue("title")
	if title == "" {
		title = wfName
	}
	inst, err := h.store.CreateInstance(wfName, title, r.FormValue("priority"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// collect and apply any $VAR substitutions provided in the form
	vars := map[string]string{}
	for key, vals := range r.Form {
		if strings.HasPrefix(key, "var_") && len(vals) > 0 && vals[0] != "" {
			vars[strings.TrimPrefix(key, "var_")] = vals[0]
		}
	}
	if len(vars) > 0 {
		h.store.ApplyVars(inst.ID, vars)
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// editInstance handles the POST form that updates an instance's title, priority, and labels.
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

// advanceStep handles the POST request to complete a ready step.
// Checks that the current user is permitted to act on the step before delegating to the store.
func (h *Handler) advanceStep(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.ParseForm()
	stepName := r.FormValue("step")
	u := h.currentUser(r)
	if inst, ok := h.store.Instance(id); ok {
		if s := inst.StepByName(stepName); s != nil && !userCanDoStep(u, s) {
			flashError(w, r, "Keine Berechtigung für diesen Schritt.")
			http.Redirect(w, r, "/instance/"+id, http.StatusSeeOther)
			return
		}
	}
	if err := h.store.AdvanceStep(id, stepName); err != nil {
		flashError(w, r, err.Error())
		return
	}
	http.Redirect(w, r, "/instance/"+id, http.StatusSeeOther)
}

// answerAsk handles the POST request to resolve an ask step's routing decision.
func (h *Handler) answerAsk(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.ParseForm()
	stepName := r.FormValue("step")
	u := h.currentUser(r)
	if inst, ok := h.store.Instance(id); ok {
		if s := inst.StepByName(stepName); s != nil && !userCanDoStep(u, s) {
			flashError(w, r, "Keine Berechtigung für diesen Schritt.")
			http.Redirect(w, r, "/instance/"+id, http.StatusSeeOther)
			return
		}
	}
	idx, _ := strconv.Atoi(r.FormValue("choice"))
	if err := h.store.AnswerAsk(id, stepName, idx); err != nil {
		flashError(w, r, err.Error())
		return
	}
	http.Redirect(w, r, "/instance/"+id, http.StatusSeeOther)
}

// cloneInstance creates a copy of an existing instance and redirects to it.
func (h *Handler) cloneInstance(w http.ResponseWriter, r *http.Request) {
	inst, err := h.store.CloneInstance(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/instance/"+inst.ID, http.StatusSeeOther)
}

// addComment appends a top-level comment to an instance.
func (h *Handler) addComment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.ParseForm()
	if text := strings.TrimSpace(r.FormValue("text")); text != "" {
		if err := h.store.AddComment(id, text); err != nil {
			flashError(w, r, err.Error())
			return
		}
	}
	http.Redirect(w, r, "/instance/"+id, http.StatusSeeOther)
}

// deleteInstance permanently removes an instance and redirects to the board.
func (h *Handler) deleteInstance(w http.ResponseWriter, r *http.Request) {
	h.store.DeleteInstance(r.PathValue("id"))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// archiveInstance marks an instance as archived and redirects to the board.
func (h *Handler) archiveInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.store.ArchiveInstance(id); err != nil {
		flashError(w, r, err.Error())
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// toggleListItem flips the checked state of a single checklist item.
func (h *Handler) toggleListItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.ParseForm()
	if err := h.store.ToggleListItem(id, r.FormValue("step"), r.FormValue("item_id")); err != nil {
		flashError(w, r, err.Error())
		return
	}
	http.Redirect(w, r, "/instance/"+id, http.StatusSeeOther)
}

// addListItem appends a user-created checklist item to a step.
func (h *Handler) addListItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.ParseForm()
	if text := strings.TrimSpace(r.FormValue("text")); text != "" {
		if err := h.store.AddListItem(id, r.FormValue("step"), text); err != nil {
			flashError(w, r, err.Error())
			return
		}
	}
	http.Redirect(w, r, "/instance/"+id, http.StatusSeeOther)
}

// checkAllListItems marks every checklist item in a step as checked.
func (h *Handler) checkAllListItems(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.ParseForm()
	h.store.CheckAllListItems(id, r.FormValue("step"))
	http.Redirect(w, r, "/instance/"+id, http.StatusSeeOther)
}

// reorder updates the drag-and-drop position of instances on the board.
func (h *Handler) reorder(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	ids := r.Form["ids[]"]
	if err := h.store.ReorderInstances(ids); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// ── Gate approval ─────────────────────────────────────────────────────────────

// approvalData is the view model for the external approval page.
type approvalData struct {
	Token    string
	Step     *engine.StepState
	Instance *engine.Instance
	Done     bool
	Error    string
}

// apiWorkflows returns a JSON list of all available workflow definitions.
// Used by the builder UI to populate the workflow selector.
func (h *Handler) apiWorkflows(w http.ResponseWriter, r *http.Request) {
	type workflowInfo struct {
		Name     string   `json:"name"`
		Priority string   `json:"priority"`
		Labels   []string `json:"labels"`
		Vars     []string `json:"vars"`
	}
	defs := h.store.Definitions()
	list := make([]workflowInfo, 0, len(defs))
	for _, d := range defs {
		list = append(list, workflowInfo{
			Name:     d.Name,
			Priority: d.Priority,
			Labels:   d.Labels,
			Vars:     d.Vars,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// approvalPage renders the external approval page for a gate step.
// No authentication is required; the token in the URL path is the credential.
func (h *Handler) approvalPage(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	inst, step := h.store.FindByToken(token)
	if inst == nil || step == nil {
		h.render(w, r, "approve.html", approvalData{Token: token, Error: "Link nicht gefunden oder bereits verwendet."})
		return
	}
	if step.GateUsed {
		h.render(w, r, "approve.html", approvalData{Token: token, Done: true, Step: step, Instance: inst})
		return
	}
	h.render(w, r, "approve.html", approvalData{Token: token, Step: step, Instance: inst})
}

// approvalSubmit processes the approval form submission and redeems the gate token.
func (h *Handler) approvalSubmit(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	r.ParseForm()
	idx, _ := strconv.Atoi(r.FormValue("choice"))
	inst, step, err := h.store.RedeemGate(token, idx)
	if err != nil {
		h.render(w, r, "approve.html", approvalData{Token: token, Error: err.Error()})
		return
	}
	h.render(w, r, "approve.html", approvalData{Token: token, Done: true, Step: step, Instance: inst})
}

// ── Admin ─────────────────────────────────────────────────────────────────────

// adminData is the view model for the user administration page.
type adminData struct {
	Users       []*auth.User
	Flash       string
	CurrentUser *auth.User
}

// adminUsers renders the user administration list.
func (h *Handler) adminUsers(w http.ResponseWriter, r *http.Request) {
	users := h.users.List()
	sort.Slice(users, func(i, j int) bool { return users[i].CreatedAt.Before(users[j].CreatedAt) })
	h.render(w, r, "admin.html", adminData{Users: users, Flash: getFlash(w, r), CurrentUser: h.currentUser(r)})
}

// adminCreateUser handles the form submission to create a new user.
func (h *Handler) adminCreateUser(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	email := strings.TrimSpace(r.FormValue("email"))
	name := strings.TrimSpace(r.FormValue("name"))
	password := r.FormValue("password")
	role := auth.Role(r.FormValue("role"))
	if _, err := h.users.Create(email, name, password, role); err != nil {
		flashError(w, r, err.Error())
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// adminEditUser handles the form submission to update an existing user's profile.
func (h *Handler) adminEditUser(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	id := r.PathValue("id")
	active := r.FormValue("active") == "1"
	// parse comma-separated app_roles from a hidden form input
	var appRoles []string
	for _, rr := range strings.Split(r.FormValue("app_roles"), ",") {
		rr = strings.TrimSpace(strings.ToLower(rr))
		if rr != "" {
			appRoles = append(appRoles, rr)
		}
	}
	if err := h.users.Update(id, r.FormValue("name"), r.FormValue("email"),
		auth.Role(r.FormValue("role")), appRoles, active); err != nil {
		flashError(w, r, err.Error())
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// adminDeleteUser permanently deletes a user account.
// Prevents an admin from deleting their own account.
func (h *Handler) adminDeleteUser(w http.ResponseWriter, r *http.Request) {
	cu := h.currentUser(r)
	id := r.PathValue("id")
	if cu != nil && cu.ID == id {
		flashError(w, r, "Du kannst deinen eigenen Account nicht löschen.")
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}
	h.users.Delete(id)
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// adminResetPassword sets a new password for a user account.
func (h *Handler) adminResetPassword(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	id := r.PathValue("id")
	pw := r.FormValue("password")
	if len(pw) < 8 {
		flashError(w, r, "Passwort muss mindestens 8 Zeichen haben.")
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}
	if err := h.users.ResetPassword(id, pw); err != nil {
		flashError(w, r, err.Error())
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// flashError sets a short-lived error cookie and redirects back to the referring page.
func flashError(w http.ResponseWriter, r *http.Request, msg string) {
	http.SetCookie(w, &http.Cookie{Name: "flash_error", Value: msg, Path: "/", MaxAge: 10})
	ref := r.Header.Get("Referer")
	if ref == "" {
		ref = "/"
	}
	http.Redirect(w, r, ref, http.StatusSeeOther)
}

// getFlash reads and immediately clears the flash error cookie.
// Returns the message string, or "" if no flash cookie was set.
func getFlash(w http.ResponseWriter, r *http.Request) string {
	c, err := r.Cookie("flash_error")
	if err != nil {
		return ""
	}
	http.SetCookie(w, &http.Cookie{Name: "flash_error", Path: "/", MaxAge: -1})
	return c.Value
}

// render executes a named template into a buffer and writes it to the response.
// Uses a buffer to avoid sending a partial response on template errors.
func (h *Handler) render(w http.ResponseWriter, r *http.Request, name string, data any) {
	var buf strings.Builder
	if err := h.templates.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(buf.String()))
}

// renderError renders the error page template with an HTTP status code.
func (h *Handler) renderError(w http.ResponseWriter, r *http.Request, msg string, code int) {
	w.WriteHeader(code)
	h.render(w, r, "error.html", map[string]interface{}{"Message": msg, "Code": code, "CurrentUser": h.currentUser(r)})
}

// containsStr reports whether s is present in slice.
func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// hasAnyLabel reports whether the instance labels intersect with the filter set.
func hasAnyLabel(labels []string, filters []string) bool {
	for _, f := range filters {
		for _, l := range labels {
			if strings.ToLower(l) == f {
				return true
			}
		}
	}
	return false
}

// matchDueFilter returns true if the instance matches the given due-date filter:
// "overdue" — has at least one overdue step
// "today"   — has a step due before end of today
// "7d"      — has a step due within the next 7 days
func matchDueFilter(inst *engine.Instance, filter string, now time.Time) bool {
	switch filter {
	case "overdue":
		return inst.HasOverdue()
	case "today":
		end := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, now.Location())
		var found bool
		inst.AllStepsDue(func(due time.Time) {
			if !due.After(end) && due.After(now) {
				found = true
			}
		})
		return found
	case "7d":
		end := now.Add(7 * 24 * time.Hour)
		var found bool
		inst.AllStepsDue(func(due time.Time) {
			if due.After(now) && due.Before(end) {
				found = true
			}
		})
		return found
	}
	return true
}

// matchCreatedFilter returns true if the instance was created within the given window:
// "today" — created today, "7d" — last 7 days, "30d" — last 30 days.
func matchCreatedFilter(inst *engine.Instance, filter string, now time.Time) bool {
	switch filter {
	case "today":
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		return inst.CreatedAt.After(start)
	case "7d":
		return inst.CreatedAt.After(now.Add(-7 * 24 * time.Hour))
	case "30d":
		return inst.CreatedAt.After(now.Add(-30 * 24 * time.Hour))
	}
	return true
}

// priorityVal maps a priority string to a numeric sort key (high=3, medium=2, low=1).
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

// matchAssignFilter returns true if the instance has at least one active step
// whose assign field matches the given filter.
// filter=="me" matches the current user by name, email, or app_role.
func matchAssignFilter(inst *engine.Instance, filter string, u *auth.User) bool {
	for _, sec := range inst.Sections {
		for _, s := range sec.Steps {
			if s.Assign == "" {
				continue
			}
			if string(s.Status) == "done" || string(s.Status) == "skipped" || string(s.Status) == "ended" {
				continue
			}
			if filter == "me" && u != nil {
				if s.Assign == "user:"+u.Name || s.Assign == "user:"+u.Email || s.Assign == u.Name || s.Assign == u.Email {
					return true
				}
				if strings.HasPrefix(s.Assign, "role:") {
					roleName := strings.TrimPrefix(s.Assign, "role:")
					if slices.Contains(u.AppRoles, roleName) {
						return true
					}
				}
			} else if s.Assign == filter {
				return true
			}
		}
	}
	return false
}

// hasLabel reports whether the given label (lowercased) is present in labels.
func hasLabel(labels []string, filter string) bool {
	for _, l := range labels {
		if strings.ToLower(l) == filter {
			return true
		}
	}
	return false
}

// userCanDoStep returns true if the user may act on the given step.
// Admins and unassigned steps are always permitted.
// Otherwise the user must match the assign expression by name, email, or app_role.
func userCanDoStep(u *auth.User, s *engine.StepState) bool {
	if u == nil || !u.CanWrite() {
		return false
	}
	if u.CanAdmin() || s.Assign == "" {
		return true
	}
	if s.Assign == "user:"+u.Name || s.Assign == "user:"+u.Email || s.Assign == u.Name || s.Assign == u.Email {
		return true
	}
	if strings.HasPrefix(s.Assign, "role:") {
		roleName := strings.TrimPrefix(s.Assign, "role:")
		return slices.Contains(u.AppRoles, roleName)
	}
	return false
}

// ── Profile ───────────────────────────────────────────────────────────────────

// profilePage renders the current user's profile page.
func (h *Handler) profilePage(w http.ResponseWriter, r *http.Request) {
	u := h.currentUser(r)
	h.render(w, r, "profile.html", map[string]any{
		"CurrentUser": u,
		"Flash":       getFlash(w, r),
	})
}

// profileSave handles the profile edit form, updating the user's display name
// and optionally their password.
func (h *Handler) profileSave(w http.ResponseWriter, r *http.Request) {
	u := h.currentUser(r)
	r.ParseForm()
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		flashError(w, r, "Name darf nicht leer sein.")
		http.Redirect(w, r, "/profile", http.StatusSeeOther)
		return
	}
	pw := r.FormValue("password")
	if pw != "" {
		if len(pw) < 8 {
			flashError(w, r, "Passwort muss mindestens 8 Zeichen haben.")
			http.Redirect(w, r, "/profile", http.StatusSeeOther)
			return
		}
		if err := h.users.ResetPassword(u.ID, pw); err != nil {
			flashError(w, r, err.Error())
			http.Redirect(w, r, "/profile", http.StatusSeeOther)
			return
		}
	}
	if err := h.users.Update(u.ID, name, u.Email, u.Role, u.AppRoles, u.Active); err != nil {
		flashError(w, r, err.Error())
		http.Redirect(w, r, "/profile", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/profile", http.StatusSeeOther)
}
