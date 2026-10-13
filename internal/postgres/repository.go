package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fairwix/MinecraftSkins-Go/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) Ping(ctx context.Context) error { return r.pool.Ping(ctx) }

const skinColumns = `id::text, name, base_price_usd::text, is_available, created_at_utc, updated_at_utc`

func scanSkin(row pgx.Row) (domain.Skin, error) {
	var skin domain.Skin
	var price string
	err := row.Scan(&skin.ID, &skin.Name, &price, &skin.IsAvailable, &skin.CreatedAtUTC, &skin.UpdatedAtUTC)
	if err != nil {
		return domain.Skin{}, err
	}
	skin.BasePriceUSD, err = decimal.NewFromString(price)
	if err != nil {
		return domain.Skin{}, fmt.Errorf("parse skin price: %w", err)
	}
	return skin, nil
}

func (r *Repository) ListSkins(ctx context.Context, filter domain.SkinFilter) ([]domain.Skin, int, error) {
	conditions := []string{"is_deleted = false"}
	args := make([]any, 0, 4)
	if filter.AvailableOnly {
		conditions = append(conditions, "is_available = true")
	}
	if search := strings.TrimSpace(filter.Search); search != "" {
		search = strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(search)
		args = append(args, "%"+search+"%")
		conditions = append(conditions, fmt.Sprintf(`name ILIKE $%d ESCAPE '\'`, len(args)))
	}
	where := strings.Join(conditions, " AND ")
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM skins WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count skins: %w", err)
	}
	args = append(args, filter.Take, filter.Skip)
	query := `SELECT ` + skinColumns + ` FROM skins WHERE ` + where +
		fmt.Sprintf(" ORDER BY name ASC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list skins: %w", err)
	}
	defer rows.Close()
	skins := make([]domain.Skin, 0)
	for rows.Next() {
		skin, err := scanSkin(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan skin: %w", err)
		}
		skins = append(skins, skin)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate skins: %w", err)
	}
	return skins, total, nil
}

func (r *Repository) GetSkin(ctx context.Context, id string) (domain.Skin, error) {
	skin, err := scanSkin(r.pool.QueryRow(ctx, `SELECT `+skinColumns+` FROM skins WHERE id = $1 AND is_deleted = false`, id))
	return skin, mapNotFound(err)
}

func (r *Repository) CreateSkin(ctx context.Context, skin domain.Skin) (domain.Skin, error) {
	created, err := scanSkin(r.pool.QueryRow(ctx,
		`INSERT INTO skins (id, name, base_price_usd, is_available, created_at_utc)
		 VALUES ($1, $2, $3::numeric, $4, $5) RETURNING `+skinColumns,
		skin.ID, skin.Name, skin.BasePriceUSD.StringFixed(2), skin.IsAvailable, skin.CreatedAtUTC))
	if err != nil {
		return domain.Skin{}, fmt.Errorf("create skin: %w", err)
	}
	return created, nil
}

func (r *Repository) UpdateSkin(ctx context.Context, id string, name *string, price *decimal.Decimal, available *bool) (domain.Skin, error) {
	var priceValue any
	if price != nil {
		priceValue = price.StringFixed(2)
	}
	skin, err := scanSkin(r.pool.QueryRow(ctx,
		`UPDATE skins SET name = COALESCE($2::text, name),
		 base_price_usd = COALESCE($3::numeric, base_price_usd),
		 is_available = COALESCE($4::boolean, is_available), updated_at_utc = now()
		 WHERE id = $1 AND is_deleted = false RETURNING `+skinColumns,
		id, name, priceValue, available))
	return skin, mapNotFound(err)
}

func (r *Repository) SoftDeleteSkin(ctx context.Context, id string) (bool, error) {
	result, err := r.pool.Exec(ctx,
		`UPDATE skins SET is_deleted = true, deleted_at_utc = now(), updated_at_utc = now()
		 WHERE id = $1 AND is_deleted = false`, id)
	if err != nil {
		return false, fmt.Errorf("soft delete skin: %w", err)
	}
	return result.RowsAffected() != 0, nil
}

const purchaseColumns = `p.id::text, p.skin_id::text, s.name, p.price_usd_final::text,
	p.btc_usd_rate::text, p.rate_source, p.purchased_at_utc, p.buyer_id, p.idempotency_key`

func scanPurchase(row pgx.Row) (domain.Purchase, error) {
	var purchase domain.Purchase
	var price, rate string
	var key *string
	err := row.Scan(&purchase.ID, &purchase.SkinID, &purchase.SkinName, &price,
		&rate, &purchase.RateSource, &purchase.PurchasedAt, &purchase.BuyerID, &key)
	if err != nil {
		return domain.Purchase{}, err
	}
	purchase.PriceUSDFinal, err = decimal.NewFromString(price)
	if err != nil {
		return domain.Purchase{}, fmt.Errorf("parse purchase price: %w", err)
	}
	purchase.BTCUSDRate, err = decimal.NewFromString(rate)
	if err != nil {
		return domain.Purchase{}, fmt.Errorf("parse purchase BTC rate: %w", err)
	}
	purchase.IdempotencyKey = key
	return purchase, nil
}

func (r *Repository) GetPurchaseByIdempotencyKey(ctx context.Context, buyerID, key string) (*domain.Purchase, error) {
	purchase, err := scanPurchase(r.pool.QueryRow(ctx,
		`SELECT `+purchaseColumns+` FROM purchases p JOIN skins s ON s.id = p.skin_id
		 WHERE p.buyer_id = $1 AND p.idempotency_key = $2`, buyerID, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find purchase by idempotency key: %w", err)
	}
	return &purchase, nil
}

func (r *Repository) CreatePurchase(ctx context.Context, skinID, buyerID string, rate domain.Rate, key *string) (domain.Purchase, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.Purchase{}, fmt.Errorf("begin purchase transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	if key != nil {
		existing, err := scanPurchase(tx.QueryRow(ctx,
			`SELECT `+purchaseColumns+` FROM purchases p JOIN skins s ON s.id = p.skin_id
			 WHERE p.buyer_id = $1 AND p.idempotency_key = $2`, buyerID, *key))
		if err == nil {
			if err := tx.Commit(ctx); err != nil {
				return domain.Purchase{}, fmt.Errorf("commit idempotent purchase read: %w", err)
			}
			return existing, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return domain.Purchase{}, fmt.Errorf("check idempotency key: %w", err)
		}
	}

	skin, err := scanSkin(tx.QueryRow(ctx,
		`SELECT `+skinColumns+` FROM skins WHERE id = $1 AND is_deleted = false FOR UPDATE`, skinID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Purchase{}, &domain.SkinUnavailableError{SkinID: skinID, Reason: "Skin not found"}
	}
	if err != nil {
		return domain.Purchase{}, fmt.Errorf("lock purchase skin: %w", err)
	}
	if !skin.IsAvailable {
		return domain.Purchase{}, &domain.SkinUnavailableError{SkinID: skinID, Reason: "Skin is not available"}
	}

	// Recheck after acquiring the skin lock to serialize retries that use the same key.
	if key != nil {
		existing, err := scanPurchase(tx.QueryRow(ctx,
			`SELECT `+purchaseColumns+` FROM purchases p JOIN skins s ON s.id = p.skin_id
			 WHERE p.buyer_id = $1 AND p.idempotency_key = $2`, buyerID, *key))
		if err == nil {
			if err := tx.Commit(ctx); err != nil {
				return domain.Purchase{}, fmt.Errorf("commit idempotent purchase read: %w", err)
			}
			return existing, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return domain.Purchase{}, fmt.Errorf("recheck idempotency key: %w", err)
		}
	}

	id, err := domain.NewID()
	if err != nil {
		return domain.Purchase{}, fmt.Errorf("create purchase id: %w", err)
	}
	purchasedAt := time.Now().UTC()
	finalPrice := domain.FinalPrice(skin.BasePriceUSD, rate.Rate)
	_, err = tx.Exec(ctx,
		`INSERT INTO purchases (id, skin_id, price_usd_final, btc_usd_rate, rate_source, purchased_at_utc, buyer_id, idempotency_key)
		 VALUES ($1, $2, $3::numeric, $4::numeric, $5, $6, $7, $8)`,
		id, skinID, finalPrice.StringFixed(2), rate.Rate.StringFixed(2), rate.Source, purchasedAt, buyerID, key)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && key != nil {
			_ = tx.Rollback(ctx)
			existing, lookupErr := r.GetPurchaseByIdempotencyKey(ctx, buyerID, *key)
			if lookupErr != nil {
				return domain.Purchase{}, lookupErr
			}
			if existing != nil {
				return *existing, nil
			}
		}
		return domain.Purchase{}, fmt.Errorf("insert purchase: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Purchase{}, fmt.Errorf("commit purchase: %w", err)
	}
	return domain.Purchase{
		ID: id, SkinID: skin.ID, SkinName: skin.Name, PriceUSDFinal: finalPrice,
		BTCUSDRate: rate.Rate, RateSource: rate.Source, PurchasedAt: purchasedAt,
		BuyerID: buyerID, IdempotencyKey: key,
	}, nil
}

func (r *Repository) GetPurchase(ctx context.Context, id string) (domain.Purchase, error) {
	purchase, err := scanPurchase(r.pool.QueryRow(ctx,
		`SELECT `+purchaseColumns+` FROM purchases p JOIN skins s ON s.id = p.skin_id WHERE p.id = $1`, id))
	return purchase, mapNotFound(err)
}

func (r *Repository) ListPurchases(ctx context.Context, filter domain.PurchaseFilter) ([]domain.Purchase, error) {
	conditions := make([]string, 0, 4)
	args := make([]any, 0, 6)
	if filter.BuyerID != "" {
		args = append(args, filter.BuyerID)
		conditions = append(conditions, fmt.Sprintf("p.buyer_id = $%d", len(args)))
	}
	if filter.SkinID != "" {
		args = append(args, filter.SkinID)
		conditions = append(conditions, fmt.Sprintf("p.skin_id = $%d", len(args)))
	}
	if filter.From != nil {
		args = append(args, *filter.From)
		conditions = append(conditions, fmt.Sprintf("p.purchased_at_utc >= $%d", len(args)))
	}
	if filter.To != nil {
		args = append(args, *filter.To)
		conditions = append(conditions, fmt.Sprintf("p.purchased_at_utc <= $%d", len(args)))
	}
	query := `SELECT ` + purchaseColumns + ` FROM purchases p JOIN skins s ON s.id = p.skin_id`
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	args = append(args, filter.Take, filter.Skip)
	query += fmt.Sprintf(" ORDER BY p.purchased_at_utc DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list purchases: %w", err)
	}
	defer rows.Close()
	purchases := make([]domain.Purchase, 0)
	for rows.Next() {
		purchase, err := scanPurchase(rows)
		if err != nil {
			return nil, fmt.Errorf("scan purchase: %w", err)
		}
		purchases = append(purchases, purchase)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate purchases: %w", err)
	}
	return purchases, nil
}

func mapNotFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("database query: %w", err)
	}
	return nil
}
