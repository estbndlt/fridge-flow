package service

import (
	"context"
	"testing"

	"github.com/estbndlt/fridge-flow/internal/models"
)

type shoppingRepoStub struct{}

func (shoppingRepoStub) ListStores(context.Context, int64) ([]models.Store, error) {
	return nil, nil
}

func (shoppingRepoStub) CreateStore(_ context.Context, householdID, createdByUserID int64, name string) (models.Store, error) {
	return models.Store{ID: 1, HouseholdID: householdID, Name: name, CreatedByUserID: createdByUserID}, nil
}

func (shoppingRepoStub) ArchiveStore(context.Context, int64, int64) error {
	return nil
}

func (shoppingRepoStub) ListActiveItems(context.Context, int64) ([]models.GroceryItem, error) {
	return nil, nil
}

func (shoppingRepoStub) ListFocusItems(context.Context, int64, int64) (models.Store, []models.GroceryItem, error) {
	return models.Store{ID: 1, Name: "Costco"}, nil, nil
}

func (shoppingRepoStub) ListHistory(context.Context, int64) ([]models.GroceryItem, error) {
	return nil, nil
}

func (shoppingRepoStub) CreateItem(_ context.Context, householdID, addedByUserID int64, input models.CreateItemInput) (models.GroceryItem, error) {
	return models.GroceryItem{ID: 1, HouseholdID: householdID, Name: input.Name, StoreID: input.StoreID, AddedByUserID: addedByUserID}, nil
}

func (shoppingRepoStub) UpdateItem(_ context.Context, householdID, itemID int64, input models.UpdateItemInput) (models.GroceryItem, error) {
	return models.GroceryItem{ID: itemID, HouseholdID: householdID, Name: input.Name, StoreID: input.StoreID}, nil
}

func (shoppingRepoStub) PurchaseItem(context.Context, int64, int64, int64) error {
	return nil
}

func (shoppingRepoStub) RestoreItem(_ context.Context, householdID, itemID, addedByUserID int64) (models.GroceryItem, error) {
	return models.GroceryItem{ID: itemID + 1, HouseholdID: householdID, AddedByUserID: addedByUserID}, nil
}

func (shoppingRepoStub) DeleteItem(context.Context, int64, int64) error {
	return nil
}

func (shoppingRepoStub) ListMembers(context.Context, int64) ([]models.Member, []models.HouseholdInvite, error) {
	return nil, nil, nil
}

func (shoppingRepoStub) CreateInvite(_ context.Context, householdID, invitedByUserID int64, email string) (models.HouseholdInvite, error) {
	return models.HouseholdInvite{ID: 1, HouseholdID: householdID, Email: email, InvitedByUserID: invitedByUserID}, nil
}

func (shoppingRepoStub) RemoveMember(context.Context, int64, int64) error {
	return nil
}

func TestCreateItemRequiresNameAndStore(t *testing.T) {
	svc := NewShoppingService(shoppingRepoStub{})

	if _, err := svc.CreateItem(context.Background(), 1, 2, models.CreateItemInput{Name: "", StoreID: 1}); !IsValidationError(err) {
		t.Fatalf("expected validation error for missing name, got %v", err)
	}

	if _, err := svc.CreateItem(context.Background(), 1, 2, models.CreateItemInput{Name: "Eggs"}); !IsValidationError(err) {
		t.Fatalf("expected validation error for missing store, got %v", err)
	}
}

func TestInviteMemberValidatesEmail(t *testing.T) {
	svc := NewShoppingService(shoppingRepoStub{})

	if _, err := svc.InviteMember(context.Background(), 1, 2, models.InviteMemberInput{Email: "invalid"}); !IsValidationError(err) {
		t.Fatalf("expected validation error for invalid email, got %v", err)
	}

	invite, err := svc.InviteMember(context.Background(), 1, 2, models.InviteMemberInput{Email: "TEST@EXAMPLE.COM"})
	if err != nil {
		t.Fatalf("InviteMember: %v", err)
	}
	if invite.Email != "test@example.com" {
		t.Fatalf("expected normalized email, got %q", invite.Email)
	}
}

func TestRemoveMemberRequiresOwner(t *testing.T) {
	svc := NewShoppingService(shoppingRepoStub{})

	err := svc.RemoveMember(context.Background(), models.CurrentUser{HouseholdID: 1, Role: models.RoleMember}, 10)
	if err == nil {
		t.Fatalf("expected authorization error")
	}
}
