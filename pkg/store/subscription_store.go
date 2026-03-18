package store

import (
	"context"
	"encoding/json"
	"errors"
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
	UpdateTxSubscriptionStatus(ctx context.Context, txCode string, status string) error
	SaveAddressSubscription(ctx context.Context, sub *models.AddressSubscription) error
	UpdateAddressSubscriptionStatus(ctx context.Context, address string, status string) error
	SavePublishedState(ctx context.Context, txCode string, state models.PublishedTxState) error
	SavePendingCallback(ctx context.Context, pending *models.PendingCallback) error
	DeletePendingCallback(ctx context.Context, taskID string) error
}

func NewSubscriptionStore(cfg *config.Config) (SubscriptionStore, error) {
	return newMySQLSubscriptionStore(cfg.MySQL)
}

type mySQLSubscriptionStore struct {
	db *gorm.DB
}

type txSubscriptionModel struct {
	TxCode             string    `gorm:"column:tx_code;primaryKey;size:128"`
	NetworkCode        string    `gorm:"column:network_code;size:64;not null"`
	EndBlockNumber     uint64    `gorm:"column:end_block_number;not null"`
	SubscriptionStatus string    `gorm:"column:subscription_status;size:32;not null"`
	CreatedAt          time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt          time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (txSubscriptionModel) TableName() string { return "connector_tx_subscriptions" }

type addressSubscriptionModel struct {
	Address                string    `gorm:"column:address;primaryKey;size:128"`
	NetworkCode            string    `gorm:"column:network_code;size:64;not null"`
	LastObservedSlot       uint64    `gorm:"column:last_observed_slot;not null"`
	LastObservedTxCode     string    `gorm:"column:last_observed_tx_code;size:128;not null"`
	TrackedAccountsJSON    string    `gorm:"column:tracked_accounts_json;type:longtext"`
	AccountCheckpointsJSON string    `gorm:"column:account_checkpoints_json;type:longtext"`
	SubscriptionStatus     string    `gorm:"column:subscription_status;size:32;not null"`
	CreatedAt              time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt              time.Time `gorm:"column:updated_at;autoUpdateTime"`
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

type pendingCallbackModel struct {
	TaskID      string    `gorm:"column:task_id;primaryKey;size:160"`
	Kind        string    `gorm:"column:kind;size:32;not null"`
	TxCode      string    `gorm:"column:tx_code;size:128;not null"`
	NetworkCode string    `gorm:"column:network_code;size:64;not null"`
	PayloadJSON string    `gorm:"column:payload_json;type:longtext"`
	RetryCount  uint64    `gorm:"column:retry_count;not null"`
	LastError   string    `gorm:"column:last_error;type:text"`
	NextRetryAt time.Time `gorm:"column:next_retry_at;not null"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt   time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (pendingCallbackModel) TableName() string { return "connector_pending_callbacks" }

func newMySQLSubscriptionStore(cfg *config.MySQLConfig) (SubscriptionStore, error) {
	if cfg == nil || cfg.DSN == "" {
		return nil, errors.New("mysql.dsn is required")
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
		&pendingCallbackModel{},
	)
}

func (s *mySQLSubscriptionStore) Load(ctx context.Context) (*models.SubscriptionSnapshot, error) {
	snapshot := &models.SubscriptionSnapshot{
		TxSubs:           make(map[string]*models.TxSubscription),
		AddressSubs:      make(map[string]*models.AddressSubscription),
		PublishedState:   make(map[string]models.PublishedTxState),
		PendingCallbacks: make(map[string]*models.PendingCallback),
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
		trackedAccounts := make([]string, 0)
		if row.TrackedAccountsJSON != "" {
			if err := json.Unmarshal([]byte(row.TrackedAccountsJSON), &trackedAccounts); err != nil {
				return nil, err
			}
		}
		accountCheckpoints := make(map[string]models.AddressCheckpoint)
		if row.AccountCheckpointsJSON != "" {
			if err := json.Unmarshal([]byte(row.AccountCheckpointsJSON), &accountCheckpoints); err != nil {
				return nil, err
			}
		}
		if len(accountCheckpoints) == 0 && (row.LastObservedSlot > 0 || row.LastObservedTxCode != "") {
			accountCheckpoints[row.Address] = models.AddressCheckpoint{
				LastObservedSlot:   row.LastObservedSlot,
				LastObservedTxCode: row.LastObservedTxCode,
			}
		}
		sub := &models.AddressSubscription{
			CreatedAt:          row.CreatedAt,
			Address:            row.Address,
			NetworkCode:        row.NetworkCode,
			TrackedAccounts:    trackedAccounts,
			AccountCheckpoints: accountCheckpoints,
			SubscriptionStatus: status,
		}
		snapshot.AddressSubs[sub.Address] = sub
	}

	var publishedRows []publishedStateModel
	if err := s.db.WithContext(ctx).
		Where("state NOT IN ?", []string{
			models.TxStateFinalized,
			models.TxStateDropped,
			models.TxStateReverted,
		}).
		Find(&publishedRows).Error; err != nil {
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

	var pendingRows []pendingCallbackModel
	if err := s.db.WithContext(ctx).Find(&pendingRows).Error; err != nil {
		return nil, err
	}
	for _, row := range pendingRows {
		snapshot.PendingCallbacks[row.TaskID] = &models.PendingCallback{
			TaskID:      row.TaskID,
			Kind:        row.Kind,
			TxCode:      row.TxCode,
			NetworkCode: row.NetworkCode,
			PayloadJSON: row.PayloadJSON,
			RetryCount:  row.RetryCount,
			LastError:   row.LastError,
			NextRetryAt: row.NextRetryAt,
			CreatedAt:   row.CreatedAt,
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
		CreatedAt:          sub.CreatedAt,
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "tx_code"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"network_code", "end_block_number", "subscription_status", "updated_at",
		}),
	}).Create(&model).Error
}

func (s *mySQLSubscriptionStore) UpdateTxSubscriptionStatus(ctx context.Context, txCode string, status string) error {
	return s.db.WithContext(ctx).Model(&txSubscriptionModel{}).
		Where("tx_code = ?", txCode).
		Updates(map[string]interface{}{
			"subscription_status": status,
			"updated_at":          time.Now(),
		}).Error
}

func (s *mySQLSubscriptionStore) SaveAddressSubscription(ctx context.Context, sub *models.AddressSubscription) error {
	trackedAccountsJSON, err := json.Marshal(sub.TrackedAccounts)
	if err != nil {
		return err
	}
	accountCheckpointsJSON, err := json.Marshal(sub.AccountCheckpoints)
	if err != nil {
		return err
	}
	legacySlot, legacyTxCode := addressSubscriptionLegacyCheckpoint(sub)
	model := addressSubscriptionModel{
		Address:                sub.Address,
		NetworkCode:            sub.NetworkCode,
		LastObservedSlot:       legacySlot,
		LastObservedTxCode:     legacyTxCode,
		TrackedAccountsJSON:    string(trackedAccountsJSON),
		AccountCheckpointsJSON: string(accountCheckpointsJSON),
		SubscriptionStatus:     sub.SubscriptionStatus,
		CreatedAt:              sub.CreatedAt,
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "address"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"network_code", "last_observed_slot", "last_observed_tx_code", "tracked_accounts_json", "account_checkpoints_json", "subscription_status", "updated_at",
		}),
	}).Create(&model).Error
}

func (s *mySQLSubscriptionStore) UpdateAddressSubscriptionStatus(ctx context.Context, address string, status string) error {
	return s.db.WithContext(ctx).Model(&addressSubscriptionModel{}).
		Where("address = ?", address).
		Updates(map[string]interface{}{
			"subscription_status": status,
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

func (s *mySQLSubscriptionStore) SavePendingCallback(ctx context.Context, pending *models.PendingCallback) error {
	model := pendingCallbackModel{
		TaskID:      pending.TaskID,
		Kind:        pending.Kind,
		TxCode:      pending.TxCode,
		NetworkCode: pending.NetworkCode,
		PayloadJSON: pending.PayloadJSON,
		RetryCount:  pending.RetryCount,
		LastError:   pending.LastError,
		NextRetryAt: pending.NextRetryAt,
		CreatedAt:   pending.CreatedAt,
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "task_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"kind", "tx_code", "network_code", "payload_json", "retry_count", "last_error", "next_retry_at", "updated_at",
		}),
	}).Create(&model).Error
}

func (s *mySQLSubscriptionStore) DeletePendingCallback(ctx context.Context, taskID string) error {
	return s.db.WithContext(ctx).Where("task_id = ?", taskID).Delete(&pendingCallbackModel{}).Error
}

func addressSubscriptionLegacyCheckpoint(sub *models.AddressSubscription) (uint64, string) {
	var latestSlot uint64
	latestTxCode := ""
	for _, account := range sub.TrackedAccounts {
		checkpoint, ok := sub.AccountCheckpoints[account]
		if !ok {
			continue
		}
		if checkpoint.LastObservedSlot > latestSlot {
			latestSlot = checkpoint.LastObservedSlot
			latestTxCode = checkpoint.LastObservedTxCode
		}
	}
	if latestSlot == 0 {
		for account, checkpoint := range sub.AccountCheckpoints {
			if checkpoint.LastObservedSlot > latestSlot {
				latestSlot = checkpoint.LastObservedSlot
				latestTxCode = checkpoint.LastObservedTxCode
			}
			if latestSlot == 0 && latestTxCode == "" {
				latestTxCode = checkpoint.LastObservedTxCode
			}
			if latestTxCode == "" && account != "" {
				latestTxCode = checkpoint.LastObservedTxCode
			}
		}
	}
	return latestSlot, latestTxCode
}
