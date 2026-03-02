package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/estbndlt/fridge-flow/internal/models"
)

type shoppingRepository interface {
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
	CreateInvite(ctx context.Context, householdID, invitedByUserID int64, email string) (models.HouseholdInvite, error)
	RemoveMember(ctx context.Context, householdID, memberUserID int64) error
}

type ShoppingService struct {
	repo shoppingRepository
}

func NewShoppingService(repo shoppingRepository) *ShoppingService {
	return &ShoppingService{repo: repo}
}

func (s *ShoppingService) ListStores(ctx context.Context, householdID int64) ([]models.Store, error) {
	return s.repo.ListStores(ctx, householdID)
}

func (s *ShoppingService) CreateStore(ctx context.Context, householdID, createdByUserID int64, name string) (models.Store, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return models.Store{}, ValidationError{Message: "store name is required"}
	}
	return s.repo.CreateStore(ctx, householdID, createdByUserID, name)
}

func (s *ShoppingService) ArchiveStore(ctx context.Context, householdID, storeID int64) error {
	if storeID <= 0 {
		return ValidationError{Message: "invalid store"}
	}
	return s.repo.ArchiveStore(ctx, householdID, storeID)
}

func (s *ShoppingService) ListActiveItems(ctx context.Context, householdID int64) ([]models.GroceryItem, error) {
	return s.repo.ListActiveItems(ctx, householdID)
}

func (s *ShoppingService) ListFocusItems(ctx context.Context, householdID, storeID int64) (models.Store, []models.GroceryItem, error) {
	if storeID <= 0 {
		return models.Store{}, nil, ValidationError{Message: "invalid store"}
	}
	return s.repo.ListFocusItems(ctx, householdID, storeID)
}

func (s *ShoppingService) ListHistory(ctx context.Context, householdID int64) ([]models.GroceryItem, error) {
	return s.repo.ListHistory(ctx, householdID)
}

func (s *ShoppingService) CreateItem(ctx context.Context, householdID, addedByUserID int64, input models.CreateItemInput) (models.GroceryItem, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.QuantityText = strings.TrimSpace(input.QuantityText)
	input.Notes = strings.TrimSpace(input.Notes)
	if input.Name == "" {
		return models.GroceryItem{}, ValidationError{Message: "item name is required"}
	}
	if input.StoreID <= 0 {
		return models.GroceryItem{}, ValidationError{Message: "store is required"}
	}
	return s.repo.CreateItem(ctx, householdID, addedByUserID, input)
}

func (s *ShoppingService) UpdateItem(ctx context.Context, householdID, itemID int64, input models.UpdateItemInput) (models.GroceryItem, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.QuantityText = strings.TrimSpace(input.QuantityText)
	input.Notes = strings.TrimSpace(input.Notes)
	if itemID <= 0 {
		return models.GroceryItem{}, ValidationError{Message: "invalid item"}
	}
	if input.Name == "" {
		return models.GroceryItem{}, ValidationError{Message: "item name is required"}
	}
	if input.StoreID <= 0 {
		return models.GroceryItem{}, ValidationError{Message: "store is required"}
	}
	return s.repo.UpdateItem(ctx, householdID, itemID, input)
}

func (s *ShoppingService) PurchaseItem(ctx context.Context, householdID, itemID, purchasedByUserID int64) error {
	if itemID <= 0 {
		return ValidationError{Message: "invalid item"}
	}
	return s.repo.PurchaseItem(ctx, householdID, itemID, purchasedByUserID)
}

func (s *ShoppingService) RestoreItem(ctx context.Context, householdID, itemID, addedByUserID int64) (models.GroceryItem, error) {
	if itemID <= 0 {
		return models.GroceryItem{}, ValidationError{Message: "invalid item"}
	}
	return s.repo.RestoreItem(ctx, householdID, itemID, addedByUserID)
}

func (s *ShoppingService) DeleteItem(ctx context.Context, householdID, itemID int64) error {
	if itemID <= 0 {
		return ValidationError{Message: "invalid item"}
	}
	return s.repo.DeleteItem(ctx, householdID, itemID)
}

func (s *ShoppingService) ListMembers(ctx context.Context, householdID int64) ([]models.Member, []models.HouseholdInvite, error) {
	return s.repo.ListMembers(ctx, householdID)
}

func (s *ShoppingService) InviteMember(ctx context.Context, householdID, invitedByUserID int64, input models.InviteMemberInput) (models.HouseholdInvite, error) {
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	if input.Email == "" || !strings.Contains(input.Email, "@") {
		return models.HouseholdInvite{}, ValidationError{Message: "valid email is required"}
	}
	return s.repo.CreateInvite(ctx, householdID, invitedByUserID, input.Email)
}

func (s *ShoppingService) RemoveMember(ctx context.Context, currentUser models.CurrentUser, memberUserID int64) error {
	if currentUser.Role != models.RoleOwner {
		return fmt.Errorf("%w: only the household owner can remove members", ErrUnauthorized)
	}
	if memberUserID <= 0 {
		return ValidationError{Message: "invalid member"}
	}
	return s.repo.RemoveMember(ctx, currentUser.HouseholdID, memberUserID)
}
