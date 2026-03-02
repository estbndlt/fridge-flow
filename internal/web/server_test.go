package web

import (
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/estbndlt/fridge-flow/internal/config"
	"github.com/estbndlt/fridge-flow/internal/models"
)

type authStub struct{}

func (authStub) GoogleAuthURL(string) string { return "https://accounts.google.com/o/oauth2/v2/auth" }
func (authStub) CompleteGoogleLogin(context.Context, string) (models.CurrentUser, string, error) {
	return models.CurrentUser{}, "", nil
}
func (authStub) CurrentUser(context.Context, string) (models.CurrentUser, error) {
	return models.CurrentUser{
		UserID:        1,
		Email:         "owner@example.com",
		DisplayName:   "Owner",
		HouseholdID:   1,
		HouseholdName: "Owner's Household",
		Role:          models.RoleOwner,
	}, nil
}
func (authStub) Logout(context.Context, string) error { return nil }

type shoppingStub struct{}

func (shoppingStub) ListStores(context.Context, int64) ([]models.Store, error) {
	return []models.Store{{ID: 1, Name: "Costco"}, {ID: 2, Name: "Trader Joe's"}}, nil
}
func (shoppingStub) CreateStore(context.Context, int64, int64, string) (models.Store, error) {
	return models.Store{}, nil
}
func (shoppingStub) ArchiveStore(context.Context, int64, int64) error { return nil }
func (shoppingStub) ListActiveItems(context.Context, int64) ([]models.GroceryItem, error) {
	return []models.GroceryItem{{ID: 1, StoreID: 1, StoreName: "Costco", Name: "Eggs", QuantityText: "2 dozen"}}, nil
}
func (shoppingStub) ListFocusItems(context.Context, int64, int64) (models.Store, []models.GroceryItem, error) {
	return models.Store{ID: 1, Name: "Costco"}, []models.GroceryItem{{ID: 1, StoreID: 1, StoreName: "Costco", Name: "Eggs"}}, nil
}
func (shoppingStub) ListHistory(context.Context, int64) ([]models.GroceryItem, error) {
	return []models.GroceryItem{{ID: 9, StoreName: "Trader Joe's", Name: "Avocados", PurchasedAt: timePtr(time.Now())}}, nil
}
func (shoppingStub) CreateItem(context.Context, int64, int64, models.CreateItemInput) (models.GroceryItem, error) {
	return models.GroceryItem{}, nil
}
func (shoppingStub) UpdateItem(context.Context, int64, int64, models.UpdateItemInput) (models.GroceryItem, error) {
	return models.GroceryItem{}, nil
}
func (shoppingStub) PurchaseItem(context.Context, int64, int64, int64) error { return nil }
func (shoppingStub) RestoreItem(context.Context, int64, int64, int64) (models.GroceryItem, error) {
	return models.GroceryItem{}, nil
}
func (shoppingStub) DeleteItem(context.Context, int64, int64) error { return nil }
func (shoppingStub) ListMembers(context.Context, int64) ([]models.Member, []models.HouseholdInvite, error) {
	return []models.Member{{UserID: 1, DisplayName: "Owner", Email: "owner@example.com", Role: models.RoleOwner}}, nil, nil
}
func (shoppingStub) InviteMember(context.Context, int64, int64, models.InviteMemberInput) (models.HouseholdInvite, error) {
	return models.HouseholdInvite{}, nil
}
func (shoppingStub) RemoveMember(context.Context, models.CurrentUser, int64) error { return nil }

func TestLoginPageRenders(t *testing.T) {
	server := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Continue with Google") {
		t.Fatalf("expected login CTA, body=%s", rr.Body.String())
	}
}

func TestHomePageRendersForAuthenticatedUser(t *testing.T) {
	server := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf"})
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Quick add") || !strings.Contains(body, "Costco") {
		t.Fatalf("expected home page content, body=%s", body)
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()

	cfg := config.Config{
		AppName:            "FridgeFlow",
		AppBaseURL:         "http://localhost:3000",
		ListenAddr:         ":8080",
		DatabaseURL:        "postgres://unused",
		MigrationsDir:      filepath.Join("..", "..", "internal", "db", "migrations"),
		TemplateDir:        filepath.Join("..", "..", "web", "templates"),
		StaticDir:          filepath.Join("..", "..", "web", "static"),
		SessionTTL:         24 * time.Hour,
		PollInterval:       10 * time.Second,
		GoogleClientID:     "test-client",
		GoogleClientSecret: "test-secret",
		GoogleRedirectURL:  "http://localhost:3000/auth/google/callback",
	}

	server, err := NewServer(cfg, authStub{}, shoppingStub{}, log.New(testWriter{t}, "", 0))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return server
}

type testWriter struct {
	t *testing.T
}

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimSpace(string(p)))
	return len(p), nil
}

func timePtr(value time.Time) *time.Time {
	return &value
}
