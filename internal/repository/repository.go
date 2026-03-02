package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/estbndlt/fridge-flow/internal/models"
)

type Repository struct {
	db *sql.DB
}

func New(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func displayNameFromEmail(email string) string {
	name := strings.TrimSpace(email)
	if idx := strings.Index(name, "@"); idx > 0 {
		name = name[:idx]
	}
	name = strings.ReplaceAll(name, ".", " ")
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.TrimSpace(name)
	if name == "" {
		return email
	}
	parts := strings.Fields(name)
	for i, part := range parts {
		if len(part) == 0 {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
	}
	return strings.Join(parts, " ")
}

func defaultHouseholdName(displayName string) string {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return "FridgeFlow Household"
	}
	first := strings.Fields(displayName)
	if len(first) == 0 {
		return "FridgeFlow Household"
	}
	return first[0] + "'s Household"
}

func (r *Repository) CompleteGoogleLogin(ctx context.Context, email, displayName string) (models.CurrentUser, bool, error) {
	email = normalizeEmail(email)
	displayName = strings.TrimSpace(displayName)
	if email == "" {
		return models.CurrentUser{}, false, nil
	}
	if displayName == "" {
		displayName = displayNameFromEmail(email)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return models.CurrentUser{}, false, fmt.Errorf("begin login tx: %w", err)
	}
	defer tx.Rollback()

	existingCurrent, err := currentUserByEmailTx(ctx, tx, email)
	if err == nil {
		if err := updateDisplayNameTx(ctx, tx, existingCurrent.UserID, displayName); err != nil {
			return models.CurrentUser{}, false, err
		}
		existingCurrent.DisplayName = displayName
		if err := tx.Commit(); err != nil {
			return models.CurrentUser{}, false, fmt.Errorf("commit existing login: %w", err)
		}
		return existingCurrent, true, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return models.CurrentUser{}, false, err
	}

	var householdCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM households`).Scan(&householdCount); err != nil {
		return models.CurrentUser{}, false, fmt.Errorf("count households: %w", err)
	}

	if householdCount == 0 {
		user, err := upsertUserTx(ctx, tx, email, displayName)
		if err != nil {
			return models.CurrentUser{}, false, err
		}
		var householdID int64
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO households (name, owner_user_id)
			VALUES ($1, $2)
			RETURNING id
		`, defaultHouseholdName(displayName), user.ID).Scan(&householdID); err != nil {
			return models.CurrentUser{}, false, fmt.Errorf("create household: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO household_memberships (household_id, user_id, role)
			VALUES ($1, $2, $3)
		`, householdID, user.ID, models.RoleOwner); err != nil {
			return models.CurrentUser{}, false, fmt.Errorf("create owner membership: %w", err)
		}
		current, err := currentUserByUserIDTx(ctx, tx, user.ID)
		if err != nil {
			return models.CurrentUser{}, false, err
		}
		if err := tx.Commit(); err != nil {
			return models.CurrentUser{}, false, fmt.Errorf("commit bootstrap login: %w", err)
		}
		return current, true, nil
	}

	var inviteID, householdID int64
	err = tx.QueryRowContext(ctx, `
		SELECT id, household_id
		FROM household_invites
		WHERE lower(email) = lower($1)
		  AND consumed_at IS NULL
		ORDER BY id DESC
		LIMIT 1
		FOR UPDATE
	`, email).Scan(&inviteID, &householdID)
	if errors.Is(err, sql.ErrNoRows) {
		return models.CurrentUser{}, false, nil
	}
	if err != nil {
		return models.CurrentUser{}, false, fmt.Errorf("lookup invite: %w", err)
	}

	user, err := upsertUserTx(ctx, tx, email, displayName)
	if err != nil {
		return models.CurrentUser{}, false, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO household_memberships (household_id, user_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (household_id, user_id) DO NOTHING
	`, householdID, user.ID, models.RoleMember); err != nil {
		return models.CurrentUser{}, false, fmt.Errorf("create membership: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE household_invites
		SET consumed_at = NOW()
		WHERE id = $1
	`, inviteID); err != nil {
		return models.CurrentUser{}, false, fmt.Errorf("consume invite: %w", err)
	}

	current, err := currentUserByUserIDTx(ctx, tx, user.ID)
	if err != nil {
		return models.CurrentUser{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return models.CurrentUser{}, false, fmt.Errorf("commit invited login: %w", err)
	}
	return current, true, nil
}

func (r *Repository) CreateSession(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO sessions (user_id, session_token_hash, expires_at)
		VALUES ($1, $2, $3)
	`, userID, tokenHash, expiresAt)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (r *Repository) GetCurrentUserBySessionHash(ctx context.Context, sessionHash string) (models.CurrentUser, error) {
	var current models.CurrentUser
	err := r.db.QueryRowContext(ctx, `
		SELECT u.id, u.email, u.display_name, h.id, h.name, hm.role
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		JOIN household_memberships hm ON hm.user_id = u.id
		JOIN households h ON h.id = hm.household_id
		WHERE s.session_token_hash = $1
		  AND s.expires_at > NOW()
		LIMIT 1
	`, sessionHash).Scan(
		&current.UserID,
		&current.Email,
		&current.DisplayName,
		&current.HouseholdID,
		&current.HouseholdName,
		&current.Role,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.CurrentUser{}, sql.ErrNoRows
		}
		return models.CurrentUser{}, fmt.Errorf("get current user by session: %w", err)
	}
	return current, nil
}

func (r *Repository) DeleteSessionByHash(ctx context.Context, sessionHash string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE session_token_hash = $1`, sessionHash)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (r *Repository) ListStores(ctx context.Context, householdID int64) ([]models.Store, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT s.id, s.household_id, s.name, s.archived, s.created_by_user_id, s.created_at,
		       COALESCE(COUNT(gi.id) FILTER (WHERE gi.status = 'active'), 0) AS active_item_count
		FROM stores s
		LEFT JOIN grocery_items gi ON gi.store_id = s.id
		WHERE s.household_id = $1
		GROUP BY s.id
		ORDER BY s.archived ASC, lower(s.name) ASC
	`, householdID)
	if err != nil {
		return nil, fmt.Errorf("list stores: %w", err)
	}
	defer rows.Close()

	var stores []models.Store
	for rows.Next() {
		var store models.Store
		if err := rows.Scan(
			&store.ID,
			&store.HouseholdID,
			&store.Name,
			&store.Archived,
			&store.CreatedByUserID,
			&store.CreatedAt,
			&store.ActiveItemCount,
		); err != nil {
			return nil, fmt.Errorf("scan store: %w", err)
		}
		stores = append(stores, store)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stores: %w", err)
	}
	return stores, nil
}

func (r *Repository) CreateStore(ctx context.Context, householdID, createdByUserID int64, name string) (models.Store, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO stores (household_id, name, created_by_user_id)
		VALUES ($1, $2, $3)
		RETURNING id
	`, householdID, strings.TrimSpace(name), createdByUserID).Scan(&id)
	if err != nil {
		return models.Store{}, fmt.Errorf("create store: %w", err)
	}
	return r.storeByID(ctx, householdID, id)
}

func (r *Repository) ArchiveStore(ctx context.Context, householdID, storeID int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin archive store tx: %w", err)
	}
	defer tx.Rollback()

	var activeCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM grocery_items
		WHERE household_id = $1 AND store_id = $2 AND status = 'active'
	`, householdID, storeID).Scan(&activeCount); err != nil {
		return fmt.Errorf("count active store items: %w", err)
	}
	if activeCount > 0 {
		return fmt.Errorf("store still has active items")
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE stores
		SET archived = TRUE, updated_at = NOW()
		WHERE household_id = $1 AND id = $2
	`, householdID, storeID)
	if err != nil {
		return fmt.Errorf("archive store: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("archive store rows affected: %w", err)
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit archive store: %w", err)
	}
	return nil
}

func (r *Repository) ListActiveItems(ctx context.Context, householdID int64) ([]models.GroceryItem, error) {
	return r.listItems(ctx, `
		SELECT gi.id, gi.household_id, gi.store_id, s.name, gi.name, gi.quantity_text, gi.notes, gi.status,
		       gi.added_by_user_id, COALESCE(u.display_name, u.email), gi.purchased_by_user_id,
		       COALESCE(pu.display_name, pu.email, ''), gi.purchased_at, gi.created_at, gi.updated_at
		FROM grocery_items gi
		JOIN stores s ON s.id = gi.store_id
		JOIN users u ON u.id = gi.added_by_user_id
		LEFT JOIN users pu ON pu.id = gi.purchased_by_user_id
		WHERE gi.household_id = $1
		  AND gi.status = 'active'
		  AND s.archived = FALSE
		ORDER BY lower(s.name) ASC, gi.created_at DESC
	`, householdID)
}

func (r *Repository) ListFocusItems(ctx context.Context, householdID, storeID int64) (models.Store, []models.GroceryItem, error) {
	store, err := r.storeByID(ctx, householdID, storeID)
	if err != nil {
		return models.Store{}, nil, err
	}
	items, err := r.listItems(ctx, `
		SELECT gi.id, gi.household_id, gi.store_id, s.name, gi.name, gi.quantity_text, gi.notes, gi.status,
		       gi.added_by_user_id, COALESCE(u.display_name, u.email), gi.purchased_by_user_id,
		       COALESCE(pu.display_name, pu.email, ''), gi.purchased_at, gi.created_at, gi.updated_at
		FROM grocery_items gi
		JOIN stores s ON s.id = gi.store_id
		JOIN users u ON u.id = gi.added_by_user_id
		LEFT JOIN users pu ON pu.id = gi.purchased_by_user_id
		WHERE gi.household_id = $1
		  AND gi.store_id = $2
		  AND gi.status = 'active'
		ORDER BY gi.created_at DESC
	`, householdID, storeID)
	if err != nil {
		return models.Store{}, nil, err
	}
	return store, items, nil
}

func (r *Repository) ListHistory(ctx context.Context, householdID int64) ([]models.GroceryItem, error) {
	return r.listItems(ctx, `
		SELECT gi.id, gi.household_id, gi.store_id, s.name, gi.name, gi.quantity_text, gi.notes, gi.status,
		       gi.added_by_user_id, COALESCE(u.display_name, u.email), gi.purchased_by_user_id,
		       COALESCE(pu.display_name, pu.email, ''), gi.purchased_at, gi.created_at, gi.updated_at
		FROM grocery_items gi
		JOIN stores s ON s.id = gi.store_id
		JOIN users u ON u.id = gi.added_by_user_id
		LEFT JOIN users pu ON pu.id = gi.purchased_by_user_id
		WHERE gi.household_id = $1
		  AND gi.status = 'purchased'
		ORDER BY gi.purchased_at DESC NULLS LAST, gi.created_at DESC
	`, householdID)
}

func (r *Repository) CreateItem(ctx context.Context, householdID, addedByUserID int64, input models.CreateItemInput) (models.GroceryItem, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO grocery_items (household_id, store_id, name, quantity_text, notes, status, added_by_user_id)
		SELECT $1, s.id, $3, $4, $5, 'active', $6
		FROM stores s
		WHERE s.id = $2
		  AND s.household_id = $1
		  AND s.archived = FALSE
		RETURNING id
	`, householdID, input.StoreID, input.Name, input.QuantityText, input.Notes, addedByUserID).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.GroceryItem{}, sql.ErrNoRows
		}
		return models.GroceryItem{}, fmt.Errorf("create item: %w", err)
	}
	return r.itemByID(ctx, householdID, id)
}

func (r *Repository) UpdateItem(ctx context.Context, householdID, itemID int64, input models.UpdateItemInput) (models.GroceryItem, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE grocery_items gi
		SET store_id = s.id,
		    name = $4,
		    quantity_text = $5,
		    notes = $6,
		    updated_at = NOW()
		FROM stores s
		WHERE gi.id = $2
		  AND gi.household_id = $1
		  AND gi.status = 'active'
		  AND s.id = $3
		  AND s.household_id = $1
		  AND s.archived = FALSE
	`, householdID, itemID, input.StoreID, input.Name, input.QuantityText, input.Notes)
	if err != nil {
		return models.GroceryItem{}, fmt.Errorf("update item: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return models.GroceryItem{}, fmt.Errorf("update item rows affected: %w", err)
	}
	if rows == 0 {
		return models.GroceryItem{}, sql.ErrNoRows
	}
	return r.itemByID(ctx, householdID, itemID)
}

func (r *Repository) PurchaseItem(ctx context.Context, householdID, itemID, purchasedByUserID int64) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE grocery_items
		SET status = 'purchased',
		    purchased_by_user_id = $3,
		    purchased_at = NOW(),
		    updated_at = NOW()
		WHERE household_id = $1
		  AND id = $2
		  AND status = 'active'
	`, householdID, itemID, purchasedByUserID)
	if err != nil {
		return fmt.Errorf("purchase item: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("purchase item rows affected: %w", err)
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *Repository) RestoreItem(ctx context.Context, householdID, itemID, addedByUserID int64) (models.GroceryItem, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return models.GroceryItem{}, fmt.Errorf("begin restore item tx: %w", err)
	}
	defer tx.Rollback()

	var source models.GroceryItem
	var purchasedByUserID sql.NullInt64
	var purchasedByName sql.NullString
	var purchasedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `
		SELECT gi.id, gi.household_id, gi.store_id, s.name, gi.name, gi.quantity_text, gi.notes, gi.status,
		       gi.added_by_user_id, COALESCE(u.display_name, u.email), gi.purchased_by_user_id,
		       COALESCE(pu.display_name, pu.email, ''), gi.purchased_at, gi.created_at, gi.updated_at
		FROM grocery_items gi
		JOIN stores s ON s.id = gi.store_id
		JOIN users u ON u.id = gi.added_by_user_id
		LEFT JOIN users pu ON pu.id = gi.purchased_by_user_id
		WHERE gi.household_id = $1
		  AND gi.id = $2
		  AND gi.status = 'purchased'
		  AND s.archived = FALSE
	`, householdID, itemID).Scan(
		&source.ID,
		&source.HouseholdID,
		&source.StoreID,
		&source.StoreName,
		&source.Name,
		&source.QuantityText,
		&source.Notes,
		&source.Status,
		&source.AddedByUserID,
		&source.AddedByName,
		&purchasedByUserID,
		&purchasedByName,
		&purchasedAt,
		&source.CreatedAt,
		&source.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.GroceryItem{}, sql.ErrNoRows
		}
		return models.GroceryItem{}, fmt.Errorf("load restore item source: %w", err)
	}
	if purchasedByUserID.Valid {
		source.PurchasedByUserID = &purchasedByUserID.Int64
	}
	source.PurchasedByName = purchasedByName.String
	if purchasedAt.Valid {
		source.PurchasedAt = &purchasedAt.Time
	}

	var newID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO grocery_items (household_id, store_id, name, quantity_text, notes, status, added_by_user_id)
		VALUES ($1, $2, $3, $4, $5, 'active', $6)
		RETURNING id
	`, householdID, source.StoreID, source.Name, source.QuantityText, source.Notes, addedByUserID).Scan(&newID); err != nil {
		return models.GroceryItem{}, fmt.Errorf("insert restored item: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return models.GroceryItem{}, fmt.Errorf("commit restore item: %w", err)
	}
	return r.itemByID(ctx, householdID, newID)
}

func (r *Repository) DeleteItem(ctx context.Context, householdID, itemID int64) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE grocery_items
		SET status = 'deleted',
		    updated_at = NOW()
		WHERE household_id = $1
		  AND id = $2
		  AND status = 'active'
	`, householdID, itemID)
	if err != nil {
		return fmt.Errorf("delete item: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete item rows affected: %w", err)
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *Repository) ListMembers(ctx context.Context, householdID int64) ([]models.Member, []models.HouseholdInvite, error) {
	memberRows, err := r.db.QueryContext(ctx, `
		SELECT u.id, u.email, u.display_name, hm.role
		FROM household_memberships hm
		JOIN users u ON u.id = hm.user_id
		WHERE hm.household_id = $1
		ORDER BY CASE WHEN hm.role = 'owner' THEN 0 ELSE 1 END, lower(u.display_name), lower(u.email)
	`, householdID)
	if err != nil {
		return nil, nil, fmt.Errorf("list members: %w", err)
	}
	defer memberRows.Close()

	var members []models.Member
	for memberRows.Next() {
		var member models.Member
		if err := memberRows.Scan(&member.UserID, &member.Email, &member.DisplayName, &member.Role); err != nil {
			return nil, nil, fmt.Errorf("scan member: %w", err)
		}
		members = append(members, member)
	}
	if err := memberRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate members: %w", err)
	}

	inviteRows, err := r.db.QueryContext(ctx, `
		SELECT id, household_id, email, invited_by_user_id, created_at, consumed_at
		FROM household_invites
		WHERE household_id = $1
		  AND consumed_at IS NULL
		ORDER BY created_at DESC
	`, householdID)
	if err != nil {
		return nil, nil, fmt.Errorf("list invites: %w", err)
	}
	defer inviteRows.Close()

	var invites []models.HouseholdInvite
	for inviteRows.Next() {
		var invite models.HouseholdInvite
		if err := inviteRows.Scan(
			&invite.ID,
			&invite.HouseholdID,
			&invite.Email,
			&invite.InvitedByUserID,
			&invite.CreatedAt,
			&invite.ConsumedAt,
		); err != nil {
			return nil, nil, fmt.Errorf("scan invite: %w", err)
		}
		invites = append(invites, invite)
	}
	if err := inviteRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate invites: %w", err)
	}

	return members, invites, nil
}

func (r *Repository) CreateInvite(ctx context.Context, householdID, invitedByUserID int64, email string) (models.HouseholdInvite, error) {
	email = normalizeEmail(email)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return models.HouseholdInvite{}, fmt.Errorf("begin create invite tx: %w", err)
	}
	defer tx.Rollback()

	var memberExists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM household_memberships hm
			JOIN users u ON u.id = hm.user_id
			WHERE hm.household_id = $1
			  AND lower(u.email) = lower($2)
		)
	`, householdID, email).Scan(&memberExists); err != nil {
		return models.HouseholdInvite{}, fmt.Errorf("check existing member: %w", err)
	}
	if memberExists {
		return models.HouseholdInvite{}, fmt.Errorf("member already belongs to this household")
	}

	var invite models.HouseholdInvite
	err = tx.QueryRowContext(ctx, `
		SELECT id, household_id, email, invited_by_user_id, created_at, consumed_at
		FROM household_invites
		WHERE household_id = $1
		  AND lower(email) = lower($2)
		  AND consumed_at IS NULL
		LIMIT 1
		FOR UPDATE
	`, householdID, email).Scan(
		&invite.ID,
		&invite.HouseholdID,
		&invite.Email,
		&invite.InvitedByUserID,
		&invite.CreatedAt,
		&invite.ConsumedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `
			INSERT INTO household_invites (household_id, email, invited_by_user_id)
			VALUES ($1, $2, $3)
			RETURNING id, household_id, email, invited_by_user_id, created_at, consumed_at
		`, householdID, email, invitedByUserID).Scan(
			&invite.ID,
			&invite.HouseholdID,
			&invite.Email,
			&invite.InvitedByUserID,
			&invite.CreatedAt,
			&invite.ConsumedAt,
		)
		if err != nil {
			return models.HouseholdInvite{}, fmt.Errorf("insert invite: %w", err)
		}
	} else if err != nil {
		return models.HouseholdInvite{}, fmt.Errorf("lookup invite: %w", err)
	} else {
		err = tx.QueryRowContext(ctx, `
			UPDATE household_invites
			SET invited_by_user_id = $2,
			    created_at = NOW()
			WHERE id = $1
			RETURNING id, household_id, email, invited_by_user_id, created_at, consumed_at
		`, invite.ID, invitedByUserID).Scan(
			&invite.ID,
			&invite.HouseholdID,
			&invite.Email,
			&invite.InvitedByUserID,
			&invite.CreatedAt,
			&invite.ConsumedAt,
		)
		if err != nil {
			return models.HouseholdInvite{}, fmt.Errorf("refresh invite: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return models.HouseholdInvite{}, fmt.Errorf("commit invite: %w", err)
	}
	return invite, nil
}

func (r *Repository) RemoveMember(ctx context.Context, householdID, memberUserID int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin remove member tx: %w", err)
	}
	defer tx.Rollback()

	var role string
	err = tx.QueryRowContext(ctx, `
		SELECT role
		FROM household_memberships
		WHERE household_id = $1 AND user_id = $2
	`, householdID, memberUserID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return sql.ErrNoRows
	}
	if err != nil {
		return fmt.Errorf("load member role: %w", err)
	}
	if role == models.RoleOwner {
		return fmt.Errorf("cannot remove household owner")
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM household_memberships
		WHERE household_id = $1 AND user_id = $2
	`, householdID, memberUserID); err != nil {
		return fmt.Errorf("delete member: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = $1`, memberUserID); err != nil {
		return fmt.Errorf("delete member sessions: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit remove member: %w", err)
	}
	return nil
}

func currentUserByEmailTx(ctx context.Context, tx *sql.Tx, email string) (models.CurrentUser, error) {
	var current models.CurrentUser
	err := tx.QueryRowContext(ctx, `
		SELECT u.id, u.email, u.display_name, h.id, h.name, hm.role
		FROM users u
		JOIN household_memberships hm ON hm.user_id = u.id
		JOIN households h ON h.id = hm.household_id
		WHERE lower(u.email) = lower($1)
		LIMIT 1
	`, email).Scan(
		&current.UserID,
		&current.Email,
		&current.DisplayName,
		&current.HouseholdID,
		&current.HouseholdName,
		&current.Role,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.CurrentUser{}, sql.ErrNoRows
		}
		return models.CurrentUser{}, fmt.Errorf("current user by email: %w", err)
	}
	return current, nil
}

func currentUserByUserIDTx(ctx context.Context, tx *sql.Tx, userID int64) (models.CurrentUser, error) {
	var current models.CurrentUser
	err := tx.QueryRowContext(ctx, `
		SELECT u.id, u.email, u.display_name, h.id, h.name, hm.role
		FROM users u
		JOIN household_memberships hm ON hm.user_id = u.id
		JOIN households h ON h.id = hm.household_id
		WHERE u.id = $1
		LIMIT 1
	`, userID).Scan(
		&current.UserID,
		&current.Email,
		&current.DisplayName,
		&current.HouseholdID,
		&current.HouseholdName,
		&current.Role,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.CurrentUser{}, sql.ErrNoRows
		}
		return models.CurrentUser{}, fmt.Errorf("current user by user id: %w", err)
	}
	return current, nil
}

func upsertUserTx(ctx context.Context, tx *sql.Tx, email, displayName string) (models.User, error) {
	var user models.User
	err := tx.QueryRowContext(ctx, `
		INSERT INTO users (email, display_name)
		VALUES ($1, $2)
		ON CONFLICT (email) DO UPDATE
		SET display_name = CASE
			WHEN EXCLUDED.display_name <> '' THEN EXCLUDED.display_name
			ELSE users.display_name
		END
		RETURNING id, email, display_name, created_at
	`, email, displayName).Scan(&user.ID, &user.Email, &user.DisplayName, &user.CreatedAt)
	if err != nil {
		return models.User{}, fmt.Errorf("upsert user: %w", err)
	}
	return user, nil
}

func updateDisplayNameTx(ctx context.Context, tx *sql.Tx, userID int64, displayName string) error {
	if strings.TrimSpace(displayName) == "" {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE users
		SET display_name = $2
		WHERE id = $1
	`, userID, displayName); err != nil {
		return fmt.Errorf("update display name: %w", err)
	}
	return nil
}

func (r *Repository) storeByID(ctx context.Context, householdID, storeID int64) (models.Store, error) {
	var store models.Store
	err := r.db.QueryRowContext(ctx, `
		SELECT s.id, s.household_id, s.name, s.archived, s.created_by_user_id, s.created_at,
		       COALESCE(COUNT(gi.id) FILTER (WHERE gi.status = 'active'), 0) AS active_item_count
		FROM stores s
		LEFT JOIN grocery_items gi ON gi.store_id = s.id
		WHERE s.household_id = $1 AND s.id = $2
		GROUP BY s.id
	`, householdID, storeID).Scan(
		&store.ID,
		&store.HouseholdID,
		&store.Name,
		&store.Archived,
		&store.CreatedByUserID,
		&store.CreatedAt,
		&store.ActiveItemCount,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Store{}, sql.ErrNoRows
		}
		return models.Store{}, fmt.Errorf("store by id: %w", err)
	}
	return store, nil
}

func (r *Repository) itemByID(ctx context.Context, householdID, itemID int64) (models.GroceryItem, error) {
	items, err := r.listItems(ctx, `
		SELECT gi.id, gi.household_id, gi.store_id, s.name, gi.name, gi.quantity_text, gi.notes, gi.status,
		       gi.added_by_user_id, COALESCE(u.display_name, u.email), gi.purchased_by_user_id,
		       COALESCE(pu.display_name, pu.email, ''), gi.purchased_at, gi.created_at, gi.updated_at
		FROM grocery_items gi
		JOIN stores s ON s.id = gi.store_id
		JOIN users u ON u.id = gi.added_by_user_id
		LEFT JOIN users pu ON pu.id = gi.purchased_by_user_id
		WHERE gi.household_id = $1
		  AND gi.id = $2
		LIMIT 1
	`, householdID, itemID)
	if err != nil {
		return models.GroceryItem{}, err
	}
	if len(items) == 0 {
		return models.GroceryItem{}, sql.ErrNoRows
	}
	return items[0], nil
}

func (r *Repository) listItems(ctx context.Context, query string, args ...any) ([]models.GroceryItem, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query items: %w", err)
	}
	defer rows.Close()

	var items []models.GroceryItem
	for rows.Next() {
		var item models.GroceryItem
		var purchasedByName sql.NullString
		var purchasedByUserID sql.NullInt64
		var purchasedAt sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.HouseholdID,
			&item.StoreID,
			&item.StoreName,
			&item.Name,
			&item.QuantityText,
			&item.Notes,
			&item.Status,
			&item.AddedByUserID,
			&item.AddedByName,
			&purchasedByUserID,
			&purchasedByName,
			&purchasedAt,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan grocery item: %w", err)
		}
		if purchasedByUserID.Valid {
			item.PurchasedByUserID = &purchasedByUserID.Int64
		}
		item.PurchasedByName = purchasedByName.String
		if purchasedAt.Valid {
			item.PurchasedAt = &purchasedAt.Time
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate grocery items: %w", err)
	}
	return items, nil
}
