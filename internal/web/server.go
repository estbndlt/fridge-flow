package web

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/estbndlt/fridge-flow/internal/config"
	"github.com/estbndlt/fridge-flow/internal/models"
	"github.com/estbndlt/fridge-flow/internal/security"
	"github.com/estbndlt/fridge-flow/internal/service"
)

const (
	sessionCookieName = "ff_session"
	csrfCookieName    = "ff_csrf"
	stateCookieName   = "ff_google_state"
)

type authService interface {
	GoogleAuthURL(state string) string
	CompleteGoogleLogin(ctx context.Context, code string) (models.CurrentUser, string, error)
	CurrentUser(ctx context.Context, rawSession string) (models.CurrentUser, error)
	Logout(ctx context.Context, rawSession string) error
}

type shoppingService interface {
	ListStores(ctx context.Context, householdID int64) ([]models.Store, error)
	CreateStore(ctx context.Context, householdID, createdByUserID int64, name string) (models.Store, error)
	ArchiveStore(ctx context.Context, householdID, storeID int64) error
	ListActiveItems(ctx context.Context, householdID int64) ([]models.GroceryItem, error)
	ListFocusItems(ctx context.Context, householdID, storeID int64) (models.Store, []models.GroceryItem, error)
	ListHistory(ctx context.Context, householdID int64) ([]models.GroceryItem, error)
	CreateItem(ctx context.Context, householdID, addedByUserID int64, input models.CreateItemInput) (models.GroceryItem, error)
	UpdateItem(ctx context.Context, householdID, itemID int64, input models.UpdateItemInput) (models.GroceryItem, error)
	PurchaseItem(ctx context.Context, householdID, itemID, purchasedByUserID int64) error
	RestoreItem(ctx context.Context, householdID, itemID, addedByUserID int64) (models.GroceryItem, error)
	DeleteItem(ctx context.Context, householdID, itemID int64) error
	ListMembers(ctx context.Context, householdID int64) ([]models.Member, []models.HouseholdInvite, error)
	InviteMember(ctx context.Context, householdID, invitedByUserID int64, input models.InviteMemberInput) (models.HouseholdInvite, error)
	RemoveMember(ctx context.Context, currentUser models.CurrentUser, memberUserID int64) error
}

type Server struct {
	cfg      config.Config
	auth     authService
	shopping shoppingService
	logger   *log.Logger
	tpl      *template.Template
}

type pageMeta struct {
	AppName     string
	Title       string
	CurrentUser *models.CurrentUser
	ActiveTab   string
	CSRFToken   string
	PollSeconds int
}

type loginPageData struct {
	pageMeta
	Error string
}

type unauthorizedPageData struct {
	pageMeta
	Email string
}

type homePageData struct {
	pageMeta
	Error  string
	Stores []models.Store
	Groups []storeGroup
}

type focusPageData struct {
	pageMeta
	Error string
	Store models.Store
	Items []models.GroceryItem
}

type historyPageData struct {
	pageMeta
	Error string
	Items []models.GroceryItem
}

type storesPageData struct {
	pageMeta
	Error  string
	Stores []models.Store
}

type membersPageData struct {
	pageMeta
	Error   string
	IsOwner bool
	Members []models.Member
	Invites []models.HouseholdInvite
}

type itemsFragmentData struct {
	CSRFToken string
	Stores    []models.Store
	Groups    []storeGroup
}

type focusFragmentData struct {
	CSRFToken string
	Store     models.Store
	Items     []models.GroceryItem
}

type historyFragmentData struct {
	CSRFToken string
	Items     []models.GroceryItem
}

type storesFragmentData struct {
	CSRFToken string
	Stores    []models.Store
}

type membersFragmentData struct {
	CSRFToken string
	IsOwner   bool
	Members   []models.Member
	Invites   []models.HouseholdInvite
}

type storeGroup struct {
	StoreID   int64
	StoreName string
	Items     []models.GroceryItem
}

func NewServer(cfg config.Config, authSvc authService, shoppingSvc shoppingService, logger *log.Logger) (*Server, error) {
	tpl, err := template.New("root").Funcs(template.FuncMap{
		"formatTimestamp": func(t time.Time) string {
			return t.Local().Format("Jan 2, 3:04 PM")
		},
		"formatMaybeTimestamp": func(t *time.Time) string {
			if t == nil || t.IsZero() {
				return ""
			}
			return t.Local().Format("Jan 2, 3:04 PM")
		},
		"storeLabel": func(quantity, name string) string {
			quantity = strings.TrimSpace(quantity)
			if quantity == "" {
				return name
			}
			return quantity + " · " + name
		},
		"isOwner": func(user *models.CurrentUser) bool {
			return user != nil && user.Role == models.RoleOwner
		},
	}).ParseGlob(filepath.Join(cfg.TemplateDir, "*.html"))
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	return &Server{
		cfg:      cfg,
		auth:     authSvc,
		shopping: shoppingSvc,
		logger:   logger,
		tpl:      tpl,
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir(s.cfg.StaticDir))))
	mux.HandleFunc("GET /favicon.svg", s.handleFavicon)
	mux.HandleFunc("GET /sw.js", s.handleServiceWorker)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /login", s.handleLogin)
	mux.HandleFunc("GET /auth/google/start", s.handleGoogleStart)
	mux.HandleFunc("GET /auth/google/callback", s.handleGoogleCallback)
	mux.HandleFunc("POST /auth/logout", s.handleLogout)

	mux.HandleFunc("GET /", s.requireAuth(s.handleHome))
	mux.HandleFunc("GET /items/fragment", s.requireAuth(s.handleItemsFragment))
	mux.HandleFunc("POST /items", s.requireAuth(s.handleCreateItem))
	mux.HandleFunc("POST /items/{id}/update", s.requireAuth(s.handleUpdateItem))
	mux.HandleFunc("POST /items/{id}/purchase", s.requireAuth(s.handlePurchaseItem))
	mux.HandleFunc("POST /items/{id}/restore", s.requireAuth(s.handleRestoreItem))
	mux.HandleFunc("POST /items/{id}/delete", s.requireAuth(s.handleDeleteItem))
	mux.HandleFunc("GET /stores/{id}/focus", s.requireAuth(s.handleStoreFocus))
	mux.HandleFunc("GET /history", s.requireAuth(s.handleHistory))
	mux.HandleFunc("GET /stores", s.requireAuth(s.handleStores))
	mux.HandleFunc("POST /stores", s.requireAuth(s.handleCreateStore))
	mux.HandleFunc("POST /stores/{id}/archive", s.requireAuth(s.handleArchiveStore))
	mux.HandleFunc("GET /settings/members", s.requireAuth(s.handleMembers))
	mux.HandleFunc("POST /settings/members/invite", s.requireAuth(s.handleInviteMember))
	mux.HandleFunc("POST /settings/members/{id}/remove", s.requireAuth(s.handleRemoveMember))

	return s.logRequests(s.securityHeaders(mux))
}

func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, filepath.Join(s.cfg.StaticDir, "icons", "icon.svg"))
}

func (s *Server) handleServiceWorker(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	http.ServeFile(w, r, filepath.Join(s.cfg.StaticDir, "sw.js"))
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.sessionValue(r); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	data := loginPageData{
		pageMeta: pageMeta{
			AppName: s.cfg.AppName,
			Title:   "Login",
		},
		Error: strings.TrimSpace(r.URL.Query().Get("error")),
	}
	s.render(w, http.StatusOK, "login", data)
}

func (s *Server) handleGoogleStart(w http.ResponseWriter, r *http.Request) {
	state, err := security.RandomToken(32)
	if err != nil {
		s.internalError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})
	http.Redirect(w, r, s.auth.GoogleAuthURL(state), http.StatusSeeOther)
}

func (s *Server) handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie(stateCookieName)
	if err != nil || strings.TrimSpace(r.URL.Query().Get("state")) == "" ||
		!security.SecureCompare(strings.TrimSpace(r.URL.Query().Get("state")), stateCookie.Value) {
		http.Redirect(w, r, "/login?error="+url.QueryEscape("Google sign-in expired. Please try again."), http.StatusSeeOther)
		return
	}

	http.SetCookie(w, expiredCookie(stateCookieName))

	currentUser, rawSession, err := s.auth.CompleteGoogleLogin(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		if errors.Is(err, service.ErrUnauthorized) {
			data := unauthorizedPageData{
				pageMeta: pageMeta{
					AppName: s.cfg.AppName,
					Title:   "Invite required",
				},
			}
			s.render(w, http.StatusForbidden, "unauthorized", data)
			return
		}
		s.internalError(w, err)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    rawSession,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(s.cfg.SessionTTL / time.Second),
	})

	s.logger.Printf("signed in email=%s household=%d", currentUser.Email, currentUser.HouseholdID)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if !s.validateCSRF(r) {
		http.Error(w, "invalid csrf token", http.StatusForbidden)
		return
	}
	if rawSession, ok := s.sessionValue(r); ok {
		if err := s.auth.Logout(r.Context(), rawSession); err != nil {
			s.logger.Printf("logout error: %v", err)
		}
	}
	http.SetCookie(w, expiredCookie(sessionCookieName))
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request, currentUser models.CurrentUser) {
	csrf := s.ensureCSRFToken(w, r)
	stores, items, err := s.homeData(r.Context(), currentUser.HouseholdID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	data := homePageData{
		pageMeta: pageMeta{
			AppName:     s.cfg.AppName,
			Title:       "Shared list",
			CurrentUser: &currentUser,
			ActiveTab:   "home",
			CSRFToken:   csrf,
			PollSeconds: int(s.cfg.PollInterval.Seconds()),
		},
		Error:  strings.TrimSpace(r.URL.Query().Get("error")),
		Stores: visibleStores(stores),
		Groups: groupItems(items),
	}
	s.render(w, http.StatusOK, "home", data)
}

func (s *Server) handleItemsFragment(w http.ResponseWriter, r *http.Request, currentUser models.CurrentUser) {
	csrf := s.ensureCSRFToken(w, r)
	stores, items, err := s.homeData(r.Context(), currentUser.HouseholdID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	data := itemsFragmentData{
		CSRFToken: csrf,
		Stores:    visibleStores(stores),
		Groups:    groupItems(items),
	}
	s.render(w, http.StatusOK, "items_fragment", data)
}

func (s *Server) handleCreateItem(w http.ResponseWriter, r *http.Request, currentUser models.CurrentUser) {
	if !s.validateCSRF(r) {
		s.writeMutationError(w, r, "/?error=", http.StatusForbidden, "invalid csrf token")
		return
	}
	if err := r.ParseForm(); err != nil {
		s.writeMutationError(w, r, "/?error=", http.StatusBadRequest, "invalid form")
		return
	}
	storeID, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("store_id")), 10, 64)
	if err != nil {
		s.writeMutationError(w, r, "/?error=", http.StatusBadRequest, "store is required")
		return
	}

	_, err = s.shopping.CreateItem(r.Context(), currentUser.HouseholdID, currentUser.UserID, models.CreateItemInput{
		Name:         r.FormValue("name"),
		QuantityText: r.FormValue("quantity_text"),
		Notes:        r.FormValue("notes"),
		StoreID:      storeID,
	})
	if err != nil {
		s.writeMutationError(w, r, "/?error=", statusForError(err), friendlyError(err))
		return
	}
	s.writeMutationSuccess(w, r, "/")
}

func (s *Server) handleUpdateItem(w http.ResponseWriter, r *http.Request, currentUser models.CurrentUser) {
	if !s.validateCSRF(r) {
		s.writeMutationError(w, r, "/?error=", http.StatusForbidden, "invalid csrf token")
		return
	}
	if err := r.ParseForm(); err != nil {
		s.writeMutationError(w, r, "/?error=", http.StatusBadRequest, "invalid form")
		return
	}
	itemID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.writeMutationError(w, r, "/?error=", http.StatusBadRequest, "invalid item")
		return
	}
	storeID, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("store_id")), 10, 64)
	if err != nil {
		s.writeMutationError(w, r, "/?error=", http.StatusBadRequest, "store is required")
		return
	}
	_, err = s.shopping.UpdateItem(r.Context(), currentUser.HouseholdID, itemID, models.UpdateItemInput{
		Name:         r.FormValue("name"),
		QuantityText: r.FormValue("quantity_text"),
		Notes:        r.FormValue("notes"),
		StoreID:      storeID,
	})
	if err != nil {
		s.writeMutationError(w, r, s.redirectTarget(r, "/"), statusForError(err), friendlyError(err))
		return
	}
	s.writeMutationSuccess(w, r, s.redirectTarget(r, "/"))
}

func (s *Server) handlePurchaseItem(w http.ResponseWriter, r *http.Request, currentUser models.CurrentUser) {
	if !s.validateCSRF(r) {
		s.writeMutationError(w, r, s.redirectTarget(r, "/"), http.StatusForbidden, "invalid csrf token")
		return
	}
	itemID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.writeMutationError(w, r, s.redirectTarget(r, "/"), http.StatusBadRequest, "invalid item")
		return
	}
	if err := s.shopping.PurchaseItem(r.Context(), currentUser.HouseholdID, itemID, currentUser.UserID); err != nil {
		s.writeMutationError(w, r, s.redirectTarget(r, "/"), statusForError(err), friendlyError(err))
		return
	}
	s.writeMutationSuccess(w, r, s.redirectTarget(r, "/"))
}

func (s *Server) handleRestoreItem(w http.ResponseWriter, r *http.Request, currentUser models.CurrentUser) {
	if !s.validateCSRF(r) {
		s.writeMutationError(w, r, "/history?error=", http.StatusForbidden, "invalid csrf token")
		return
	}
	itemID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.writeMutationError(w, r, "/history?error=", http.StatusBadRequest, "invalid item")
		return
	}
	if _, err := s.shopping.RestoreItem(r.Context(), currentUser.HouseholdID, itemID, currentUser.UserID); err != nil {
		s.writeMutationError(w, r, "/history?error=", statusForError(err), friendlyError(err))
		return
	}
	s.writeMutationSuccess(w, r, "/history")
}

func (s *Server) handleDeleteItem(w http.ResponseWriter, r *http.Request, currentUser models.CurrentUser) {
	if !s.validateCSRF(r) {
		s.writeMutationError(w, r, s.redirectTarget(r, "/"), http.StatusForbidden, "invalid csrf token")
		return
	}
	itemID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.writeMutationError(w, r, s.redirectTarget(r, "/"), http.StatusBadRequest, "invalid item")
		return
	}
	if err := s.shopping.DeleteItem(r.Context(), currentUser.HouseholdID, itemID); err != nil {
		s.writeMutationError(w, r, s.redirectTarget(r, "/"), statusForError(err), friendlyError(err))
		return
	}
	s.writeMutationSuccess(w, r, s.redirectTarget(r, "/"))
}

func (s *Server) handleStoreFocus(w http.ResponseWriter, r *http.Request, currentUser models.CurrentUser) {
	csrf := s.ensureCSRFToken(w, r)
	storeID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	store, items, err := s.shopping.ListFocusItems(r.Context(), currentUser.HouseholdID, storeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.internalError(w, err)
		return
	}
	if r.URL.Query().Get("fragment") == "1" {
		s.render(w, http.StatusOK, "focus_fragment", focusFragmentData{
			CSRFToken: csrf,
			Store:     store,
			Items:     items,
		})
		return
	}
	data := focusPageData{
		pageMeta: pageMeta{
			AppName:     s.cfg.AppName,
			Title:       store.Name,
			CurrentUser: &currentUser,
			ActiveTab:   "home",
			CSRFToken:   csrf,
			PollSeconds: int(s.cfg.PollInterval.Seconds()),
		},
		Error: strings.TrimSpace(r.URL.Query().Get("error")),
		Store: store,
		Items: items,
	}
	s.render(w, http.StatusOK, "focus", data)
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request, currentUser models.CurrentUser) {
	csrf := s.ensureCSRFToken(w, r)
	items, err := s.shopping.ListHistory(r.Context(), currentUser.HouseholdID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	if r.URL.Query().Get("fragment") == "1" {
		s.render(w, http.StatusOK, "history_fragment", historyFragmentData{
			CSRFToken: csrf,
			Items:     items,
		})
		return
	}
	data := historyPageData{
		pageMeta: pageMeta{
			AppName:     s.cfg.AppName,
			Title:       "History",
			CurrentUser: &currentUser,
			ActiveTab:   "history",
			CSRFToken:   csrf,
			PollSeconds: int(s.cfg.PollInterval.Seconds()),
		},
		Error: strings.TrimSpace(r.URL.Query().Get("error")),
		Items: items,
	}
	s.render(w, http.StatusOK, "history", data)
}

func (s *Server) handleStores(w http.ResponseWriter, r *http.Request, currentUser models.CurrentUser) {
	csrf := s.ensureCSRFToken(w, r)
	stores, err := s.shopping.ListStores(r.Context(), currentUser.HouseholdID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	if r.URL.Query().Get("fragment") == "1" {
		s.render(w, http.StatusOK, "stores_fragment", storesFragmentData{
			CSRFToken: csrf,
			Stores:    stores,
		})
		return
	}
	data := storesPageData{
		pageMeta: pageMeta{
			AppName:     s.cfg.AppName,
			Title:       "Stores",
			CurrentUser: &currentUser,
			ActiveTab:   "stores",
			CSRFToken:   csrf,
			PollSeconds: int(s.cfg.PollInterval.Seconds()),
		},
		Error:  strings.TrimSpace(r.URL.Query().Get("error")),
		Stores: stores,
	}
	s.render(w, http.StatusOK, "stores", data)
}

func (s *Server) handleCreateStore(w http.ResponseWriter, r *http.Request, currentUser models.CurrentUser) {
	if !s.validateCSRF(r) {
		s.writeMutationError(w, r, "/stores?error=", http.StatusForbidden, "invalid csrf token")
		return
	}
	if err := r.ParseForm(); err != nil {
		s.writeMutationError(w, r, "/stores?error=", http.StatusBadRequest, "invalid form")
		return
	}
	if _, err := s.shopping.CreateStore(r.Context(), currentUser.HouseholdID, currentUser.UserID, r.FormValue("name")); err != nil {
		s.writeMutationError(w, r, "/stores?error=", statusForError(err), friendlyError(err))
		return
	}
	s.writeMutationSuccess(w, r, "/stores")
}

func (s *Server) handleArchiveStore(w http.ResponseWriter, r *http.Request, currentUser models.CurrentUser) {
	if !s.validateCSRF(r) {
		s.writeMutationError(w, r, "/stores?error=", http.StatusForbidden, "invalid csrf token")
		return
	}
	storeID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.writeMutationError(w, r, "/stores?error=", http.StatusBadRequest, "invalid store")
		return
	}
	if err := s.shopping.ArchiveStore(r.Context(), currentUser.HouseholdID, storeID); err != nil {
		s.writeMutationError(w, r, "/stores?error=", statusForError(err), friendlyError(err))
		return
	}
	s.writeMutationSuccess(w, r, "/stores")
}

func (s *Server) handleMembers(w http.ResponseWriter, r *http.Request, currentUser models.CurrentUser) {
	csrf := s.ensureCSRFToken(w, r)
	members, invites, err := s.shopping.ListMembers(r.Context(), currentUser.HouseholdID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	if r.URL.Query().Get("fragment") == "1" {
		s.render(w, http.StatusOK, "members_fragment", membersFragmentData{
			CSRFToken: csrf,
			IsOwner:   currentUser.Role == models.RoleOwner,
			Members:   members,
			Invites:   invites,
		})
		return
	}
	data := membersPageData{
		pageMeta: pageMeta{
			AppName:     s.cfg.AppName,
			Title:       "Members",
			CurrentUser: &currentUser,
			ActiveTab:   "members",
			CSRFToken:   csrf,
			PollSeconds: int(s.cfg.PollInterval.Seconds()),
		},
		Error:   strings.TrimSpace(r.URL.Query().Get("error")),
		IsOwner: currentUser.Role == models.RoleOwner,
		Members: members,
		Invites: invites,
	}
	s.render(w, http.StatusOK, "members", data)
}

func (s *Server) handleInviteMember(w http.ResponseWriter, r *http.Request, currentUser models.CurrentUser) {
	if currentUser.Role != models.RoleOwner {
		s.writeMutationError(w, r, "/settings/members?error=", http.StatusForbidden, "only the household owner can invite members")
		return
	}
	if !s.validateCSRF(r) {
		s.writeMutationError(w, r, "/settings/members?error=", http.StatusForbidden, "invalid csrf token")
		return
	}
	if err := r.ParseForm(); err != nil {
		s.writeMutationError(w, r, "/settings/members?error=", http.StatusBadRequest, "invalid form")
		return
	}
	if _, err := s.shopping.InviteMember(r.Context(), currentUser.HouseholdID, currentUser.UserID, models.InviteMemberInput{
		Email: r.FormValue("email"),
	}); err != nil {
		s.writeMutationError(w, r, "/settings/members?error=", statusForError(err), friendlyError(err))
		return
	}
	s.writeMutationSuccess(w, r, "/settings/members")
}

func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request, currentUser models.CurrentUser) {
	if !s.validateCSRF(r) {
		s.writeMutationError(w, r, "/settings/members?error=", http.StatusForbidden, "invalid csrf token")
		return
	}
	memberID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.writeMutationError(w, r, "/settings/members?error=", http.StatusBadRequest, "invalid member")
		return
	}
	if err := s.shopping.RemoveMember(r.Context(), currentUser, memberID); err != nil {
		s.writeMutationError(w, r, "/settings/members?error=", statusForError(err), friendlyError(err))
		return
	}
	s.writeMutationSuccess(w, r, "/settings/members")
}

func (s *Server) homeData(ctx context.Context, householdID int64) ([]models.Store, []models.GroceryItem, error) {
	stores, err := s.shopping.ListStores(ctx, householdID)
	if err != nil {
		return nil, nil, err
	}
	items, err := s.shopping.ListActiveItems(ctx, householdID)
	if err != nil {
		return nil, nil, err
	}
	return stores, items, nil
}

func (s *Server) requireAuth(next func(http.ResponseWriter, *http.Request, models.CurrentUser)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rawSession, ok := s.sessionValue(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		currentUser, err := s.auth.CurrentUser(r.Context(), rawSession)
		if err != nil {
			http.SetCookie(w, expiredCookie(sessionCookieName))
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r, currentUser)
	}
}

func (s *Server) render(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := s.tpl.ExecuteTemplate(w, name, data); err != nil {
		s.logger.Printf("render %s: %v", name, err)
	}
}

func (s *Server) internalError(w http.ResponseWriter, err error) {
	s.logger.Printf("internal error: %v", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

func (s *Server) ensureCSRFToken(w http.ResponseWriter, r *http.Request) string {
	if cookie, err := r.Cookie(csrfCookieName); err == nil && strings.TrimSpace(cookie.Value) != "" {
		return cookie.Value
	}
	token, err := security.RandomToken(32)
	if err != nil {
		s.logger.Printf("generate csrf token: %v", err)
		return ""
	}
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((365 * 24 * time.Hour) / time.Second),
	})
	return token
}

func (s *Server) validateCSRF(r *http.Request) bool {
	formToken := strings.TrimSpace(r.FormValue("csrf_token"))
	if formToken == "" {
		return false
	}
	cookie, err := r.Cookie(csrfCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return false
	}
	return security.SecureCompare(formToken, cookie.Value)
}

func (s *Server) sessionValue(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return "", false
	}
	return cookie.Value, true
}

func expiredCookie(name string) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	}
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	baseURL, _ := url.Parse(s.cfg.AppBaseURL)
	secureBase := strings.EqualFold(baseURL.Scheme, "https")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if secureBase && !requestIsSecure(r, s.cfg.TrustProxyHeaders) {
			target := *r.URL
			target.Scheme = "https"
			target.Host = baseURL.Host
			http.Redirect(w, r, target.String(), http.StatusPermanentRedirect)
			return
		}

		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: https://lh3.googleusercontent.com; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; font-src 'self'; base-uri 'self'; frame-ancestors 'none'; form-action 'self' https://accounts.google.com")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if secureBase {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		next.ServeHTTP(w, r)
	})
}

func requestIsSecure(r *http.Request, trustProxyHeaders bool) bool {
	if r.TLS != nil {
		return true
	}
	if !trustProxyHeaders {
		return false
	}
	if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return true
	}
	return strings.Contains(strings.ToLower(r.Header.Get("Cf-Visitor")), "\"https\"")
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		s.logger.Printf("%s %s ip=%s duration=%s", r.Method, r.URL.Path, clientIP(r), time.Since(start).Round(time.Millisecond))
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func visibleStores(stores []models.Store) []models.Store {
	result := make([]models.Store, 0, len(stores))
	for _, store := range stores {
		if !store.Archived {
			result = append(result, store)
		}
	}
	return result
}

func groupItems(items []models.GroceryItem) []storeGroup {
	groupMap := make(map[int64]int)
	groups := make([]storeGroup, 0)
	for _, item := range items {
		if idx, ok := groupMap[item.StoreID]; ok {
			groups[idx].Items = append(groups[idx].Items, item)
			continue
		}
		groupMap[item.StoreID] = len(groups)
		groups = append(groups, storeGroup{
			StoreID:   item.StoreID,
			StoreName: item.StoreName,
			Items:     []models.GroceryItem{item},
		})
	}
	return groups
}

func isAsyncRequest(r *http.Request) bool {
	return r.Header.Get("X-FridgeFlow-Async") == "1"
}

func (s *Server) writeMutationSuccess(w http.ResponseWriter, r *http.Request, redirectPath string) {
	if isAsyncRequest(r) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, redirectPath, http.StatusSeeOther)
}

func (s *Server) writeMutationError(w http.ResponseWriter, r *http.Request, redirectPrefix string, status int, message string) {
	if isAsyncRequest(r) {
		http.Error(w, message, status)
		return
	}
	http.Redirect(w, r, withErrorQuery(redirectPrefix, message), http.StatusSeeOther)
}

func (s *Server) redirectTarget(r *http.Request, fallback string) string {
	target := strings.TrimSpace(r.FormValue("redirect_to"))
	if target == "" {
		return fallback
	}
	return target
}

func statusForError(err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case service.IsValidationError(err):
		return http.StatusBadRequest
	case errors.Is(err, service.ErrUnauthorized):
		return http.StatusForbidden
	case errors.Is(err, sql.ErrNoRows):
		return http.StatusNotFound
	default:
		return http.StatusBadRequest
	}
}

func friendlyError(err error) string {
	switch {
	case err == nil:
		return ""
	case service.IsValidationError(err):
		return err.Error()
	case errors.Is(err, sql.ErrNoRows):
		return "That item could not be found."
	}

	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "duplicate key"), strings.Contains(text, "unique"):
		return "That already exists."
	case strings.Contains(text, "store still has active items"):
		return "You can only archive a store after its active items are cleared."
	case strings.Contains(text, "member already belongs"):
		return "That person is already in your household."
	case strings.Contains(text, "only the household owner"):
		return "Only the household owner can do that."
	case strings.Contains(text, "cannot remove household owner"):
		return "The household owner cannot be removed."
	default:
		return "Something went wrong. Please try again."
	}
}

func withErrorQuery(target, message string) string {
	if strings.Contains(target, "?error=") || strings.Contains(target, "&error=") {
		return target + url.QueryEscape(message)
	}
	separator := "?error="
	if strings.Contains(target, "?") {
		separator = "&error="
	}
	return target + separator + url.QueryEscape(message)
}
