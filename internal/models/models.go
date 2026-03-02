package models

import "time"

const (
	RoleOwner  = "owner"
	RoleMember = "member"
)

const (
	ItemStatusActive    = "active"
	ItemStatusPurchased = "purchased"
	ItemStatusDeleted   = "deleted"
)

type User struct {
	ID          int64
	Email       string
	DisplayName string
	CreatedAt   time.Time
}

type CurrentUser struct {
	UserID        int64
	Email         string
	DisplayName   string
	HouseholdID   int64
	HouseholdName string
	Role          string
}

type Household struct {
	ID          int64
	Name        string
	OwnerUserID int64
	CreatedAt   time.Time
}

type HouseholdMembership struct {
	HouseholdID int64
	UserID      int64
	Role        string
	CreatedAt   time.Time
}

type HouseholdInvite struct {
	ID              int64
	HouseholdID     int64
	Email           string
	InvitedByUserID int64
	CreatedAt       time.Time
	ConsumedAt      *time.Time
}

type Store struct {
	ID              int64
	HouseholdID     int64
	Name            string
	Archived        bool
	CreatedByUserID int64
	ActiveItemCount int
	CreatedAt       time.Time
}

type GroceryItem struct {
	ID                int64
	HouseholdID       int64
	StoreID           int64
	StoreName         string
	Name              string
	QuantityText      string
	Notes             string
	Status            string
	AddedByUserID     int64
	AddedByName       string
	PurchasedByUserID *int64
	PurchasedByName   string
	PurchasedAt       *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type Session struct {
	ID               int64
	UserID           int64
	SessionTokenHash string
	ExpiresAt        time.Time
	CreatedAt        time.Time
}

type CreateItemInput struct {
	Name         string
	QuantityText string
	Notes        string
	StoreID      int64
}

type UpdateItemInput struct {
	Name         string
	QuantityText string
	Notes        string
	StoreID      int64
}

type InviteMemberInput struct {
	Email string
}

type Member struct {
	UserID      int64
	Email       string
	DisplayName string
	Role        string
}
