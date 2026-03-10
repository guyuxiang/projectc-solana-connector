package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/guyuxiang/projectc-solana-connector/pkg/config"
	"github.com/guyuxiang/projectc-solana-connector/pkg/models"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SubscriptionStore interface {
	Load(ctx context.Context) (*models.SubscriptionSnapshot, error)
	SaveTxSubscription(ctx context.Context, sub *models.TxSubscription) error
	UpdateTxSubscriptionStatus(ctx context.Context, txCode string, status string, completed bool) error
	SaveAddressSubscription(ctx context.Context, sub *models.AddressSubscription) error
	UpdateAddressSubscriptionStatus(ctx context.Context, address string, status string, completed bool) error
	SavePublishedState(ctx context.Context, txCode string, state models.PublishedTxState) error
	DeletePublishedState(ctx context.Context, txCode string) error
}

func NewSubscriptionStore(cfg *config.Config) (SubscriptionStore, error) {
	return newMySQLSubscriptionStore(cfg.Connector.SubscriptionStore.MySQL)
}

type mySQLSubscriptionStore struct {
	db *gorm.DB
}

type txSubscriptionModel struct {
	TxCode             string    `gorm:"column:tx_code;primaryKey;size:128"`
	NetworkCode        string    `gorm:"column:network_code;size:64;not null"`
	EndBlockNumber     uint64    `gorm:"column:end_block_number;not null"`
	SubscriptionStatus string    `gorm:"column:subscription_status;size:32;not null"`
	Completed          bool      `gorm:"column:completed;not null"`
	CreatedAt          time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt          time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (txSubscriptionModel) TableName() string { return "connector_tx_subscriptions" }

type addressSubscriptionModel struct {
	Address            string    `gorm:"column:address;primaryKey;size:128"`
	NetworkCode        string    `gorm:"column:network_code;size:64;not null"`
	LastObservedSlot   uint64    `gorm:"column:last_observed_slot;not null"`
	LastObservedTxCode string    `gorm:"column:last_observed_tx_code;size:128;not null"`
	SubscriptionStatus string    `gorm:"column:subscription_status;size:32;not null"`
	Completed          bool      `gorm:"column:completed;not null"`
	SeenTxsJSON        string    `gorm:"column:seen_txs_json;type:mediumtext;not null"`
	CreatedAt          time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt          time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (addressSubscriptionModel) TableName() string { return "connector_address_subscriptions" }

type publishedStateModel struct {
	TxCode      string    `gorm:"column:tx_code;primaryKey;size:128"`
	NetworkCode string    `gorm:"column:network_code;size:64;not null"`
	BlockNumber uint64    `gorm:"column:block_number;not null"`
	State       string    `gorm:"column:state;size:32;not null"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt   time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (publishedStateModel) TableName() string { return "connector_published_states" }

func newMySQLSubscriptionStore(cfg *config.MySQLConfig) (SubscriptionStore, error) {
	if cfg == nil || cfg.DSN == "" {
		return nil, errors.New("subscriptionStore.mysql.dsn is required")
	}

	db, err := gorm.Open(mysql.Open(cfg.DSN), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifeSec) * time.Second)

	store := &mySQLSubscriptionStore{db: db}
	if err := store.migrate(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *mySQLSubscriptionStore) migrate() error {
	return s.db.AutoMigrate(
		&txSubscriptionModel{},
		&addressSubscriptionModel{},
		&publishedStateModel{},
	)
}

func (s *mySQLSubscriptionStore) Load(ctx context.Context) (*models.SubscriptionSnapshot, error) {
	snapshot := &models.SubscriptionSnapshot{
		TxSubs:         make(map[string]*models.TxSubscription),
		AddressSubs:    make(map[string]*models.AddressSubscription),
		PublishedState: make(map[string]models.PublishedTxState),
	}

	var txRows []txSubscriptionModel
	if err := s.db.WithContext(ctx).Where("subscription_status = ?", models.TxSubscriptionStatusActive).Find(&txRows).Error; err != nil {
		return nil, err
	}
	for _, row := range txRows {
		sub := &models.TxSubscription{
			CreatedAt:          row.CreatedAt,
			TxCode:             row.TxCode,
			NetworkCode:        row.NetworkCode,
			EndBlockNumber:     row.EndBlockNumber,
			SubscriptionStatus: row.SubscriptionStatus,
			Completed:          row.Completed,
		}
		snapshot.TxSubs[sub.TxCode] = sub
	}

	var addressRows []addressSubscriptionModel
	if err := s.db.WithContext(ctx).
		Where("subscription_status = ? OR subscription_status = ''", models.TxSubscriptionStatusActive).
		Find(&addressRows).Error; err != nil {
		return nil, err
	}
	for _, row := range addressRows {
		status := row.SubscriptionStatus
		if status == "" {
			status = models.TxSubscriptionStatusActive
		}
		sub := &models.AddressSubscription{
			CreatedAt:          row.CreatedAt,
			Address:            row.Address,
			NetworkCode:        row.NetworkCode,
			LastObservedSlot:   row.LastObservedSlot,
			LastObservedTxCode: row.LastObservedTxCode,
			SubscriptionStatus: status,
			Completed:          row.Completed,
			SeenTxs:            decodeSeenTxs(row.SeenTxsJSON),
		}
		snapshot.AddressSubs[sub.Address] = sub
	}

	var publishedRows []publishedStateModel
	if err := s.db.WithContext(ctx).Find(&publishedRows).Error; err != nil {
		return nil, err
	}
	for _, row := range publishedRows {
		snapshot.PublishedState[row.TxCode] = models.PublishedTxState{
			CreatedAt:   row.CreatedAt,
			NetworkCode: row.NetworkCode,
			BlockNumber: row.BlockNumber,
			State:       row.State,
		}
	}
	return snapshot, nil
}

func (s *mySQLSubscriptionStore) SaveTxSubscription(ctx context.Context, sub *models.TxSubscription) error {
	model := txSubscriptionModel{
		TxCode:             sub.TxCode,
		NetworkCode:        sub.NetworkCode,
		EndBlockNumber:     sub.EndBlockNumber,
		SubscriptionStatus: sub.SubscriptionStatus,
		Completed:          sub.Completed,
		CreatedAt:          sub.CreatedAt,
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "tx_code"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"network_code", "end_block_number", "subscription_status", "completed", "updated_at",
		}),
	}).Create(&model).Error
}

func (s *mySQLSubscriptionStore) UpdateTxSubscriptionStatus(ctx context.Context, txCode string, status string, completed bool) error {
	return s.db.WithContext(ctx).Model(&txSubscriptionModel{}).
		Where("tx_code = ?", txCode).
		Updates(map[string]interface{}{
			"subscription_status": status,
			"completed":           completed,
			"updated_at":          time.Now(),
		}).Error
}

func (s *mySQLSubscriptionStore) SaveAddressSubscription(ctx context.Context, sub *models.AddressSubscription) error {
	seenTxsJSON, err := encodeSeenTxs(sub.SeenTxs)
	if err != nil {
		return err
	}
	model := addressSubscriptionModel{
		Address:            sub.Address,
		NetworkCode:        sub.NetworkCode,
		LastObservedSlot:   sub.LastObservedSlot,
		LastObservedTxCode: sub.LastObservedTxCode,
		SubscriptionStatus: sub.SubscriptionStatus,
		Completed:          sub.Completed,
		SeenTxsJSON:        seenTxsJSON,
		CreatedAt:          sub.CreatedAt,
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "address"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"network_code", "last_observed_slot", "last_observed_tx_code", "subscription_status", "completed", "seen_txs_json", "updated_at",
		}),
	}).Create(&model).Error
}

func (s *mySQLSubscriptionStore) UpdateAddressSubscriptionStatus(ctx context.Context, address string, status string, completed bool) error {
	return s.db.WithContext(ctx).Model(&addressSubscriptionModel{}).
		Where("address = ?", address).
		Updates(map[string]interface{}{
			"subscription_status": status,
			"completed":           completed,
			"updated_at":          time.Now(),
		}).Error
}

func (s *mySQLSubscriptionStore) SavePublishedState(ctx context.Context, txCode string, state models.PublishedTxState) error {
	model := publishedStateModel{
		TxCode:      txCode,
		NetworkCode: state.NetworkCode,
		BlockNumber: state.BlockNumber,
		State:       state.State,
		CreatedAt:   state.CreatedAt,
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "tx_code"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"network_code", "block_number", "state", "updated_at",
		}),
	}).Create(&model).Error
}

func (s *mySQLSubscriptionStore) DeletePublishedState(ctx context.Context, txCode string) error {
	return s.db.WithContext(ctx).Delete(&publishedStateModel{}, "tx_code = ?", txCode).Error
}

func encodeSeenTxs(seen map[string]struct{}) (string, error) {
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	payload, err := json.Marshal(keys)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func decodeSeenTxs(raw string) map[string]struct{} {
	out := make(map[string]struct{})
	if strings.TrimSpace(raw) == "" {
		return out
	}
	var keys []string
	if err := json.Unmarshal([]byte(raw), &keys); err != nil {
		return out
	}
	for _, key := range keys {
		out[key] = struct{}{}
	}
	return out
}
