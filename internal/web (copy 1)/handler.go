package web

import (
	"encoding/json"
	"flowapp/internal/auth"
	"flowapp/internal/dsl"
	"flowapp/internal/engine"
	"flowapp/internal/logger"
	"flowapp/internal/mailer"
	"flowapp/internal/notifications"
	"flowapp/internal/store"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

var webLog = logger.New("web")

// Handler holds all HTTP handler state: the data store, user store,
// compiled templates, and the login rate limiter.
type Handler struct {
	store      *store.Store
	users      *auth.UserStore
	templates  *template.Template
	loginRL    *auth.RateLimiter
	dataDir    string
	notifStore interface {
		List(userID string) []notifications.Notification
		UnreadCount(userID string) int
		MarkRead(userID, notifID string)
		MarkUnread(userID, notifID string)
		MarkAllRead(userID string)
	}
}

type ctxKey string

const ctxUserKey ctxKey = "user"

// New creates a Handler, compiling all HTML templates from the given glob pattern
// and registering custom template functions used by the views.
func New(s *store.Store, users *auth.UserStore, tmplGlob string, dataDir string) (*Handler, error) {
	tmpl, err := template.New("").Funcs(template.FuncMap{
		// csrfField renders a hidden CSRF input for the given user ID.
		// Usage in templates: {{csrfField .CurrentUser}}
		"csrfField": func(u *auth.User) template.HTML {
			if u == nil {
				return ""
			}
			token := auth.GenerateCSRFToken(u.ID)
			return template.HTML(`<input type="hidden" name="` + auth.CSRFFieldName() + `" value="` + token + `">`)
		},
		"lower":               strings.ToLower,
		"urlenc":              url.PathEscape,
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
			case auth.RoleManager:
				return "Manager"
			case auth.RoleUser:
				return "User"
			case auth.RoleViewer:
				return "Viewer"
			}
			return string(r)
		},
		"isAdmin": func(u *auth.User) bool { return u != nil && u.CanAdmin() },
		// canDeleteInstance returns true if the user may permanently delete instances.
		"canDeleteInstance": func(u *auth.User) bool { return u != nil && u.CanDeleteInstance() },
		// canCloneInstance returns true if the user may clone instances.
		"canCloneInstance": func(u *auth.User) bool { return u != nil && u.CanCloneInstance() },
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
		"canCreateInstance": func(u *auth.User) bool { return u != nil && u.CanCreateInstance() },
		// isPage returns true if the given page name matches the current page.
		// Each handler sets Page on its data struct so the partial can highlight the active nav link.
		"isPage": func(page, current string) bool { return page == current },
		// canDoStep returns true if the user may act on a step.
		// Admins and steps without an assign field are always allowed.
		// Otherwise the user must match at least one assign expression.
		"canDoStep": func(u *auth.User, s *engine.StepState) bool {
			if u == nil || !u.CanCreateInstance() {
				return false
			}
			if u.CanAdmin() || len(s.Assign) == 0 {
				return true
			}
			for _, expr := range s.Assign {
				if expr == "user:"+u.Name || expr == "user:"+u.Email || expr == u.Name || expr == u.Email {
					return true
				}
				if strings.HasPrefix(expr, "role:") {
					roleName := strings.TrimPrefix(expr, "role:")
					if slices.Contains(u.AppRoles, roleName) {
						return true
					}
				}
			}
			return false
		},
	}).ParseGlob(tmplGlob)
	if err != nil {
		return nil, err
	}
	return &Handler{store: s, users: users, templates: tmpl, loginRL: auth.NewRateLimiter(), dataDir: dataDir, notifStore: s.Notifications()}, nil
}

// ── Auth middleware ────────────────────────────────────────────────────────────

// unreadCount returns the number of unread notifications for the current user, or 0 if not logged in.
func (h *Handler) unreadCount(u *auth.User) int {
	if u == nil || h.notifStore == nil {
		return 0
	}
	return h.notifStore.UnreadCount(u.ID)
}
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
		if !u.CanCreateInstance() {
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

// requireCSRF validates the CSRF token on POST requests for authenticated users.
// Should wrap all state-changing POST handlers except /login, /setup, and /approve.
func (h *Handler) requireCSRF(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := h.currentUser(r)
		if u == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		r.ParseForm()
		token := auth.CSRFTokenFromRequest(r)
		if err := auth.ValidateCSRFToken(token, u.ID); err != nil {
			webLog.Warn("CSRF rejected request to %s: %v", r.URL.Path, err)
			h.renderError(w, r, "Invalid or expired form token. Please go back and try again.", http.StatusForbidden)
			return
		}
		next(w, r)
	}
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
	mux.HandleFunc("POST /logout", h.requireCSRF(h.logout))
	mux.HandleFunc("GET /profile", h.requireAuth(h.profilePage))
	mux.HandleFunc("POST /profile", h.requireWrite(h.requireCSRF(h.profileSave)))

	mux.HandleFunc("GET /notifications", h.requireAuth(h.notificationsPage))
	mux.HandleFunc("POST /notifications/mark", h.requireAuth(h.requireCSRF(h.notificationsMark)))
	// main app (auth required)
	mux.HandleFunc("GET /", h.requireAuth(h.board))
	mux.HandleFunc("GET /workflows", h.requireAuth(h.workflowsPage))
	mux.HandleFunc("GET /builder", h.requireAuth(h.builder))
	mux.HandleFunc("GET /archive", h.requireAuth(h.archive))
	mux.HandleFunc("GET /instance/{id}", h.requireAuth(h.instanceDetail))
	mux.HandleFunc("POST /instance", h.requireWrite(h.requireCSRF(h.createInstance)))
	mux.HandleFunc("GET /instance/new/{workflow}", h.requireWrite(h.newInstancePrompt))
	mux.HandleFunc("POST /instance/{id}/edit", h.requireWrite(h.requireCSRF(h.editInstance)))
	mux.HandleFunc("POST /instance/{id}/step", h.requireWrite(h.requireCSRF(h.advanceStep)))
	mux.HandleFunc("POST /instance/{id}/ask", h.requireWrite(h.requireCSRF(h.answerAsk)))
	mux.HandleFunc("POST /instance/{id}/clone", h.requireWrite(h.requireCSRF(h.cloneInstance)))
	mux.HandleFunc("POST /instance/{id}/comment", h.requireWrite(h.requireCSRF(h.addComment)))
	mux.HandleFunc("POST /instance/{id}/stepcomment", h.requireWrite(h.requireCSRF(h.addStepComment)))
	mux.HandleFunc("POST /instance/{id}/delete", h.requireWrite(h.requireCSRF(h.deleteInstance)))
	mux.HandleFunc("POST /instance/{id}/archive", h.requireWrite(h.requireCSRF(h.archiveInstance)))
	mux.HandleFunc("POST /instance/{id}/listitem/toggle", h.requireWrite(h.requireCSRF(h.toggleListItem)))
	mux.HandleFunc("POST /instance/{id}/listitem/add", h.requireWrite(h.requireCSRF(h.addListItem)))
	mux.HandleFunc("POST /instance/{id}/listitem/checkall", h.requireWrite(h.requireCSRF(h.checkAllListItems)))
	mux.HandleFunc("POST /reorder", h.requireWrite(h.requireCSRF(h.reorder)))
	// static assets
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("internal/web/static"))))
	// public API
	mux.HandleFunc("GET /api/workflows", h.apiWorkflows)
	// gate approval — no login required; token is the credential; POST-only for submission
	mux.HandleFunc("GET /approve/{token}", h.approvalPage)
	mux.HandleFunc("POST /approve/{token}", h.approvalSubmit)
	// admin
	mux.HandleFunc("GET /admin/users", h.requireAdmin(h.adminUsers))
	mux.HandleFunc("POST /admin/users", h.requireAdmin(h.requireCSRF(h.adminCreateUser)))
	mux.HandleFunc("POST /admin/users/{id}/edit", h.requireAdmin(h.requireCSRF(h.adminEditUser)))
	mux.HandleFunc("POST /admin/users/{id}/delete", h.requireAdmin(h.requireCSRF(h.adminDeleteUser)))
	mux.HandleFunc("POST /admin/users/{id}/password", h.requireAdmin(h.requireCSRF(h.adminResetPassword)))
	mux.HandleFunc("GET /admin/system", h.requireAdmin(h.adminSystem))
	mux.HandleFunc("POST /admin/full-reset", h.requireAdmin(h.requireCSRF(h.fullReset)))
	mux.HandleFunc("GET /admin/mail", h.requireAdmin(h.adminMailPage))
	mux.HandleFunc("POST /admin/mail", h.requireAdmin(h.requireCSRF(h.adminMailSave)))
	mux.HandleFunc("POST /admin/mail/test", h.requireAdmin(h.requireCSRF(h.adminMailTest)))
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
		webLog.Warn("login failed for %s: %v", email, err)
		h.render(w, r, "login.html", map[string]string{"Error": "Ungültige E-Mail oder Passwort.", "Next": next})
		return
	}
	webLog.Info("login successful for %s (%s)", u.ID, email)
	h.loginRL.Reset(r) // clear rate-limit counter on successful login
	auth.SetSession(w, u.ID)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// logout clears the session cookie and redirects to /login.
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if u := h.currentUser(r); u != nil {
		webLog.Info("logout for user %s (%s)", u.ID, u.Email)
	}
	auth.ClearSession(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// workflowCard is the view model for a single workflow on the /workflows selection page.
type workflowCard struct {
	Name      string
	Priority  string
	Labels    []string
	Vars      []string
	StepCount int
}

// workflowsData is the view model for the workflow selection page.
type workflowsData struct {
	Workflows   []workflowCard
	CurrentUser *auth.User
	Page        string
	UnreadCount int
}

// workflowsPage renders the workflow selection / new-instance page.
func (h *Handler) workflowsPage(w http.ResponseWriter, r *http.Request) {
	u := h.currentUser(r)
	defs := h.store.Definitions()
	// sort alphabetically for stable display
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	cards := make([]workflowCard, 0, len(defs))
	for _, d := range defs {
		if !userCanStartWorkflow(u, d) {
			continue
		}
		steps := 0
		for _, sec := range d.Sections {
			steps += len(sec.Steps)
		}
		cards = append(cards, workflowCard{
			Name:      d.Name,
			Labels:    d.Labels,
			Vars:      d.Vars,
			StepCount: steps,
		})
	}
	h.render(w, r, "workflows.html", workflowsData{
		Workflows:   cards,
		CurrentUser: u,
		Page:        "workflows",
		UnreadCount: h.unreadCount(u),
	})
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
	Page             string
	UnreadCount      int
}

// newInstanceData is the view model for the new-instance prompt page.
type newInstanceData struct {
	WorkflowName string
	Title        string
	Priority     string
	Vars         []string
	CurrentUser  *auth.User
	Page         string
	UnreadCount  int
}

// archiveData is the view model for the archive page.
type archiveData struct {
	Instances   []*engine.Instance
	FilterQ     string
	CurrentUser *auth.User
	Page        string
	UnreadCount int
}

// builderData is the view model for the workflow builder page.
type builderData struct {
	CurrentUser *auth.User
	Page        string
	UnreadCount int
}

// profileData is the view model for the profile page.
type profileData struct {
	CurrentUser *auth.User
	Flash       string
	Page        string
	UnreadCount int
}

// notificationsData is the view model for the notifications page.
type notificationsData struct {
	CurrentUser   *auth.User
	Notifications []notifications.Notification
	UnreadCount   int
	Page          string
}

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
		if !userCanViewInstance(u, inst) {
			continue
		}
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

	allLabels := h.store.AllLabels()
	sort.Strings(allLabels)

	flash := getFlash(w, r)
	h.render(w, r, "board.html", boardData{
		Columns:   []column{*cols["Todo"], *cols["In Progress"], *cols["Done"]},
		AllLabels: allLabels,

		FilterQ: r.URL.Query().Get("q"), FilterPriorities: filterPriorities,
		FilterLabels: filterLabels, FilterDue: filterDue, FilterCreated: filterCreated,
		FilterSort: filterSort, FilterAssign: filterAssign,
		Flash: flash, CurrentUser: u, Page: "board", UnreadCount: h.unreadCount(u),
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
	u := h.currentUser(r)
	for _, d := range defs {
		if d.Name == wfName {
			if !userCanStartWorkflow(u, d) {
				h.renderError(w, r, "Keine Berechtigung zum Starten dieses Workflows.", http.StatusForbidden)
				return
			}
			if len(d.Vars) > 0 {
				h.render(w, r, "new_instance.html", newInstanceData{WorkflowName: wfName, Title: title, Priority: priority, Vars: d.Vars, CurrentUser: u, Page: "", UnreadCount: h.unreadCount(u)})
				return
			}
		}
	}
	// no vars — create directly without showing the prompt page
	if _, err := h.store.CreateInstance(wfName, title, u.ID); err != nil {
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
		if !userCanViewInstance(u, inst) {
			continue
		}
		if q == "" || strings.Contains(strings.ToLower(inst.Title+" "+inst.WorkflowName), q) {
			filtered = append(filtered, inst)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].UpdatedAt.After(filtered[j].UpdatedAt)
	})
	h.render(w, r, "archive.html", archiveData{Instances: filtered, FilterQ: r.URL.Query().Get("q"), CurrentUser: u, Page: "archive", UnreadCount: h.unreadCount(u)})
}

// builder renders the visual workflow builder page.
func (h *Handler) builder(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "builder.html", builderData{CurrentUser: h.currentUser(r), Page: "builder", UnreadCount: h.unreadCount(h.currentUser(r))})
}

// ── Instance ──────────────────────────────────────────────────────────────────

// instanceData is the view model for the instance detail page.
type instanceData struct {
	*engine.Instance
	Flash       string
	CurrentUser *auth.User
	PrevID      string
	NextID      string
	Page        string
	UnreadCount int
}

// instanceDetail renders the detail view for a single workflow instance.
func (h *Handler) instanceDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	inst, ok := h.store.Instance(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	u := h.currentUser(r)
	if !userCanViewInstance(u, inst) {
		h.renderError(w, r, "Keine Berechtigung für diese Instanz.", http.StatusForbidden)
		return
	}
	all := h.store.Instances()
	sort.Slice(all, func(i, j int) bool {
		if all[i].Position != all[j].Position {
			return all[i].Position < all[j].Position
		}
		return all[i].CreatedAt.Before(all[j].CreatedAt)
	})
	var prevID, nextID string
	for i, ins := range all {
		if ins.ID == id {
			if i > 0 {
				prevID = all[i-1].ID
			}
			if i < len(all)-1 {
				nextID = all[i+1].ID
			}
			break
		}
	}
	h.render(w, r, "instance.html", instanceData{Instance: inst, Flash: getFlash(w, r), CurrentUser: h.currentUser(r), PrevID: prevID, NextID: nextID, Page: "instance", UnreadCount: h.unreadCount(h.currentUser(r))})
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
	u := h.currentUser(r)
	for _, d := range h.store.Definitions() {
		if d.Name == wfName && !userCanStartWorkflow(u, d) {
			h.renderError(w, r, "Keine Berechtigung zum Starten dieses Workflows.", http.StatusForbidden)
			return
		}
	}
	inst, err := h.store.CreateInstance(wfName, title, u.ID)
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
// Requires admin or manager role.
func (h *Handler) cloneInstance(w http.ResponseWriter, r *http.Request) {
	u := h.currentUser(r)
	if u == nil || !u.CanCloneInstance() {
		h.renderError(w, r, "Keine Berechtigung zum Klonen von Instanzen.", http.StatusForbidden)
		return
	}
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
// Requires admin or manager role.
func (h *Handler) deleteInstance(w http.ResponseWriter, r *http.Request) {
	u := h.currentUser(r)
	if u == nil || !u.CanDeleteInstance() {
		h.renderError(w, r, "Keine Berechtigung zum Löschen von Instanzen.", http.StatusForbidden)
		return
	}
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
	Page     string
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
			Name:   d.Name,
			Labels: d.Labels,
			Vars:   d.Vars,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// approvalPage renders the external approval page for a gate step.
// No authentication is required; the token in the URL path is the credential.
// If the request comes from an authenticated Viewer, they are redirected to login
// since Viewers are not permitted to approve gate steps.
func (h *Handler) approvalPage(w http.ResponseWriter, r *http.Request) {
	// if a logged-in user visits the page, check they have write access
	if u := h.currentUser(r); u != nil && !u.CanCreateInstance() {
		http.Redirect(w, r, "/login?next="+r.URL.RequestURI(), http.StatusSeeOther)
		return
	}
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

// systemData is the view model for the system administration page.
type systemData struct {
	CurrentUser *auth.User
	Page        string
	UnreadCount int
}

// adminSystem renders the system administration page (danger zone).
func (h *Handler) adminSystem(w http.ResponseWriter, r *http.Request) {
	u := h.currentUser(r)
	h.render(w, r, "admin_system.html", systemData{CurrentUser: u, Page: "system", UnreadCount: h.unreadCount(u)})
}

// adminData is the view model for the user administration page.
type adminData struct {
	Users       []*auth.User
	Flash       string
	CurrentUser *auth.User
	Page        string
	UnreadCount int
}

// adminUsers renders the user administration list.
func (h *Handler) adminUsers(w http.ResponseWriter, r *http.Request) {
	users := h.users.List()
	sort.Slice(users, func(i, j int) bool { return users[i].CreatedAt.Before(users[j].CreatedAt) })
	h.render(w, r, "admin.html", adminData{Users: users, Flash: getFlash(w, r), CurrentUser: h.currentUser(r), Page: "users", UnreadCount: h.unreadCount(h.currentUser(r))})
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

// ── Admin: mail config ────────────────────────────────────────────────────────

type mailAdminData struct {
	Config      *mailer.Config
	Flash       string
	Error       string
	TestOK      bool
	CurrentUser *auth.User
	Page        string
	UnreadCount int
}

// adminMailPage renders the mail configuration page.
func (h *Handler) adminMailPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "admin_mail.html", mailAdminData{
		Config:      h.store.GetMailConfig(),
		Flash:       getFlashOK(w, r),
		CurrentUser: h.currentUser(r),
		Page:        "mail",
		UnreadCount: h.unreadCount(h.currentUser(r)),
	})
}

// adminMailSave saves the mail configuration and reloads the mailer.
func (h *Handler) adminMailSave(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	cfg := &mailer.Config{
		Type:              r.FormValue("type"),
		From:              strings.TrimSpace(r.FormValue("from")),
		SMTPHost:          strings.TrimSpace(r.FormValue("smtp_host")),
		SMTPUsername:      strings.TrimSpace(r.FormValue("smtp_username")),
		SMTPPassword:      r.FormValue("smtp_password"),
		GraphTenantID:     strings.TrimSpace(r.FormValue("graph_tenant_id")),
		GraphClientID:     strings.TrimSpace(r.FormValue("graph_client_id")),
		GraphClientSecret: r.FormValue("graph_client_secret"),
		GraphSenderUPN:    strings.TrimSpace(r.FormValue("graph_sender_upn")),
	}
	if port, err := strconv.Atoi(r.FormValue("smtp_port")); err == nil {
		cfg.SMTPPort = port
	}
	if err := h.store.SaveMailConfig(cfg, h.users.ResolveEmails); err != nil {
		h.render(w, r, "admin_mail.html", mailAdminData{
			Config: cfg, Error: err.Error(), CurrentUser: h.currentUser(r), UnreadCount: h.unreadCount(h.currentUser(r)),
		})
		return
	}
	flashSuccess(w, r, "Mail configuration saved.")
	http.Redirect(w, r, "/admin/mail", http.StatusSeeOther)
}

// adminMailTest sends a test email to the current admin's address.
func (h *Handler) adminMailTest(w http.ResponseWriter, r *http.Request) {
	u := h.currentUser(r)
	cfg := h.store.GetMailConfig()
	if cfg.Type == "" {
		h.render(w, r, "admin_mail.html", mailAdminData{
			Config: cfg, Error: "No mail config saved yet.", CurrentUser: u, UnreadCount: h.unreadCount(u),
		})
		return
	}
	m, err := mailer.NewMailerFromConfig(cfg)
	if err != nil {
		h.render(w, r, "admin_mail.html", mailAdminData{
			Config: cfg, Error: "Mailer init failed: " + err.Error(), CurrentUser: u, UnreadCount: h.unreadCount(u),
		})
		return
	}
	adapter := mailer.EngineAdapter{M: m, From: cfg.From}
	testMsg := mailer.Message{
		From:      cfg.From,
		To:        []string{u.Email},
		Subject:   "[flowapp] Test email",
		PlainBody: "This is a test email from FlowApp. Your mail configuration is working correctly.",
	}
	if err := adapter.M.Send(testMsg); err != nil {
		h.render(w, r, "admin_mail.html", mailAdminData{
			Config: cfg, Error: "Send failed: " + err.Error(), CurrentUser: u, UnreadCount: h.unreadCount(u),
		})
		return
	}
	flashSuccess(w, r, "Test email sent to "+u.Email)
	http.Redirect(w, r, "/admin/mail", http.StatusSeeOther)
}

// fullReset wipes all instances, notifications, users, and rotates the session secret.
// After the reset the user is logged out and redirected to /setup.
func (h *Handler) fullReset(w http.ResponseWriter, r *http.Request) {
	webLog.Info("FullReset initiated by user %s", h.currentUser(r).Email)
	if err := h.store.FullReset(); err != nil {
		webLog.Error("FullReset store error: %v", err)
	}
	if err := h.users.FullReset(); err != nil {
		webLog.Error("FullReset users error: %v", err)
	}
	if err := auth.DeleteSessionSecret(h.dataDir); err != nil {
		webLog.Error("FullReset session secret error: %v", err)
	}
	auth.ClearSession(w)
	http.Redirect(w, r, "/setup", http.StatusSeeOther)
}

// ── Notifications ─────────────────────────────────────────────────────────────

// notificationsPage renders the in-app notifications list for the current user.
func (h *Handler) notificationsPage(w http.ResponseWriter, r *http.Request) {
	u := h.currentUser(r)
	list := h.notifStore.List(u.ID)
	h.render(w, r, "notifications.html", notificationsData{
		CurrentUser:   u,
		Notifications: list,
		UnreadCount:   h.unreadCount(u),
		Page:          "notifications",
	})
}

// notificationsMark handles read/unread toggle and mark-all-read actions.
func (h *Handler) notificationsMark(w http.ResponseWriter, r *http.Request) {
	u := h.currentUser(r)
	r.ParseForm()
	action := r.FormValue("action")
	id := r.FormValue("id")
	switch action {
	case "read":
		h.notifStore.MarkRead(u.ID, id)
	case "unread":
		h.notifStore.MarkUnread(u.ID, id)
	case "all_read":
		h.notifStore.MarkAllRead(u.ID)
	}
	http.Redirect(w, r, "/notifications", http.StatusSeeOther)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// flashSuccess sets a short-lived success cookie.
func flashSuccess(w http.ResponseWriter, r *http.Request, msg string) {
	http.SetCookie(w, &http.Cookie{Name: "flash_ok", Value: msg, Path: "/", MaxAge: 10})
}

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

// getFlashOK reads and immediately clears the flash success cookie.
func getFlashOK(w http.ResponseWriter, r *http.Request) string {
	c, err := r.Cookie("flash_ok")
	if err != nil {
		return ""
	}
	http.SetCookie(w, &http.Cookie{Name: "flash_ok", Path: "/", MaxAge: -1})
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
			if len(s.Assign) == 0 {
				continue
			}
			if string(s.Status) == "done" || string(s.Status) == "skipped" || string(s.Status) == "ended" {
				continue
			}
			for _, expr := range s.Assign {
				if filter == "me" && u != nil {
					if expr == "user:"+u.Name || expr == "user:"+u.Email || expr == u.Name || expr == u.Email {
						return true
					}
					if strings.HasPrefix(expr, "role:") {
						roleName := strings.TrimPrefix(expr, "role:")
						if slices.Contains(u.AppRoles, roleName) {
							return true
						}
					}
				} else if expr == filter {
					return true
				}
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
// Otherwise the user must match at least one assign expression.
func userCanDoStep(u *auth.User, s *engine.StepState) bool {
	if u == nil || !u.CanCreateInstance() {
		return false
	}
	if u.CanAdmin() || len(s.Assign) == 0 {
		return true
	}
	for _, expr := range s.Assign {
		if expr == "user:"+u.Name || expr == "user:"+u.Email || expr == u.Name || expr == u.Email {
			return true
		}
		if strings.HasPrefix(expr, "role:") {
			roleName := strings.TrimPrefix(expr, "role:")
			if slices.Contains(u.AppRoles, roleName) {
				return true
			}
		}
	}
	return false
}

// userCanStartWorkflow returns true if the user may create an instance of the given workflow.
// Admins and managers always can. If AllowedRoles is empty, all CanCreateInstance users can.
// Otherwise the user must have at least one matching app_role.
func userCanStartWorkflow(u *auth.User, wf *dsl.Workflow) bool {
	if u == nil || !u.CanCreateInstance() {
		return false
	}
	if u.Role == auth.RoleAdmin || u.Role == auth.RoleManager {
		return true
	}
	if len(wf.AllowedRoles) == 0 {
		return true
	}
	for _, role := range wf.AllowedRoles {
		role = strings.TrimPrefix(role, "role:")
		if slices.Contains(u.AppRoles, role) {
			return true
		}
	}
	return false
}

// userCanViewInstance returns true if the user may see the given instance.
// Admins and managers always can. Normal users can only see instances where
// their app_role or direct user: expression appears in at least one assign field
// anywhere in the workflow (across all steps, not just active ones).
func userCanViewInstance(u *auth.User, inst *engine.Instance) bool {
	if u == nil {
		return false
	}
	if u.Role == auth.RoleAdmin || u.Role == auth.RoleManager {
		return true
	}
	// creator always sees their own instance
	if inst.CreatedBy == u.ID {
		return true
	}
	for _, sec := range inst.Sections {
		for _, s := range sec.Steps {
			for _, expr := range s.Assign {
				if expr == "user:"+u.Name || expr == "user:"+u.Email || expr == u.Name || expr == u.Email {
					return true
				}
				if strings.HasPrefix(expr, "role:") {
					roleName := strings.TrimPrefix(expr, "role:")
					if slices.Contains(u.AppRoles, roleName) {
						return true
					}
				}
			}
		}
	}
	return false
}

// profilePage renders the current user's profile page.
func (h *Handler) profilePage(w http.ResponseWriter, r *http.Request) {
	u := h.currentUser(r)
	h.render(w, r, "profile.html", profileData{CurrentUser: u, Flash: getFlash(w, r), Page: "profile", UnreadCount: h.unreadCount(u)})
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
