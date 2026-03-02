package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/estbndlt/fridge-flow/internal/auth"
	"github.com/estbndlt/fridge-flow/internal/models"
	"github.com/estbndlt/fridge-flow/internal/security"
)

type authRepository interface {
	CompleteGoogleLogin(ctx context.Context, email, displayName string) (models.CurrentUser, bool, error)
	CreateSession(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error
	GetCurrentUserBySessionHash(ctx context.Context, sessionHash string) (models.CurrentUser, error)
	DeleteSessionByHash(ctx context.Context, sessionHash string) error
}

type AuthService struct {
	repo       authRepository
	google     *auth.GoogleClient
	sessionTTL time.Duration
}

func NewAuthService(repo authRepository, google *auth.GoogleClient, sessionTTL time.Duration) *AuthService {
	return &AuthService{repo: repo, google: google, sessionTTL: sessionTTL}
}

func (s *AuthService) GoogleAuthURL(state string) string {
	return s.google.AuthURL(state)
}

func (s *AuthService) CompleteGoogleLogin(ctx context.Context, code string) (models.CurrentUser, string, error) {
	token, err := s.google.ExchangeCode(ctx, code)
	if err != nil {
		return models.CurrentUser{}, "", err
	}

	profile, err := s.google.FetchProfile(ctx, token.AccessToken)
	if err != nil {
		return models.CurrentUser{}, "", err
	}
	if !profile.EmailVerified {
		return models.CurrentUser{}, "", ErrUnauthorized
	}

	currentUser, allowed, err := s.repo.CompleteGoogleLogin(ctx, normalizeEmail(profile.Email), strings.TrimSpace(profile.Name))
	if err != nil {
		return models.CurrentUser{}, "", err
	}
	if !allowed {
		return models.CurrentUser{}, "", ErrUnauthorized
	}

	rawSession, err := security.RandomToken(32)
	if err != nil {
		return models.CurrentUser{}, "", fmt.Errorf("generate session token: %w", err)
	}
	if err := s.repo.CreateSession(ctx, currentUser.UserID, security.HashToken(rawSession), time.Now().Add(s.sessionTTL)); err != nil {
		return models.CurrentUser{}, "", err
	}
	return currentUser, rawSession, nil
}

func (s *AuthService) CurrentUser(ctx context.Context, rawSession string) (models.CurrentUser, error) {
	return s.repo.GetCurrentUserBySessionHash(ctx, security.HashToken(strings.TrimSpace(rawSession)))
}

func (s *AuthService) Logout(ctx context.Context, rawSession string) error {
	if strings.TrimSpace(rawSession) == "" {
		return nil
	}
	return s.repo.DeleteSessionByHash(ctx, security.HashToken(rawSession))
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
