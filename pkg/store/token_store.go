package store

import (
	"context"
	"errors"
	"time"

	"github.com/guyuxiang/projectc-solana-connector/pkg/config"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TokenStore interface {
	Load(ctx context.Context) (map[string]*config.Token, error)
	Get(ctx context.Context, code string) (*config.Token, error)
	List(ctx context.Context, networkCode string) (map[string]*config.Token, error)
	Save(ctx context.Context, code string, token *config.Token) error
	Delete(ctx context.Context, code string) error
	SaveAll(ctx context.Context, tokens map[string]*config.Token) error
}

type mySQLTokenStore struct {
	db *gorm.DB
}

type tokenModel struct {
	Code        string    `gorm:"column:code;primaryKey;size:64"`
	NetworkCode string    `gorm:"column:network_code;size:64;not null"`
	MintAddress string    `gorm:"column:mint_address;size:128;not null"`
	Decimals    uint8     `gorm:"column:decimals;not null"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt   time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (tokenModel) TableName() string { return "connector_tokens" }

func NewTokenStore(cfg *config.Config) (TokenStore, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}
	return newMySQLTokenStore(cfg.MySQL)
}

func newMySQLTokenStore(cfg *config.MySQLConfig) (TokenStore, error) {
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

	store := &mySQLTokenStore{db: db}
	if err := store.db.AutoMigrate(&tokenModel{}); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *mySQLTokenStore) Load(ctx context.Context) (map[string]*config.Token, error) {
	var rows []tokenModel
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, err
	}

	tokens := make(map[string]*config.Token, len(rows))
	for _, row := range rows {
		tokens[row.Code] = &config.Token{
			NetworkCode: row.NetworkCode,
			MintAddress: row.MintAddress,
			Decimals:    row.Decimals,
		}
	}
	return tokens, nil
}

func (s *mySQLTokenStore) Get(ctx context.Context, code string) (*config.Token, error) {
	var row tokenModel
	if err := s.db.WithContext(ctx).Where("code = ?", code).First(&row).Error; err != nil {
		return nil, err
	}
	return &config.Token{
		NetworkCode: row.NetworkCode,
		MintAddress: row.MintAddress,
		Decimals:    row.Decimals,
	}, nil
}

func (s *mySQLTokenStore) List(ctx context.Context, networkCode string) (map[string]*config.Token, error) {
	query := s.db.WithContext(ctx)
	if networkCode != "" {
		query = query.Where("network_code = ?", networkCode)
	}

	var rows []tokenModel
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}

	tokens := make(map[string]*config.Token, len(rows))
	for _, row := range rows {
		tokens[row.Code] = &config.Token{
			NetworkCode: row.NetworkCode,
			MintAddress: row.MintAddress,
			Decimals:    row.Decimals,
		}
	}
	return tokens, nil
}

func (s *mySQLTokenStore) Save(ctx context.Context, code string, token *config.Token) error {
	if token == nil {
		return nil
	}
	model := tokenModel{
		Code:        code,
		NetworkCode: token.NetworkCode,
		MintAddress: token.MintAddress,
		Decimals:    token.Decimals,
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "code"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"network_code", "mint_address", "decimals", "updated_at",
		}),
	}).Create(&model).Error
}

func (s *mySQLTokenStore) Delete(ctx context.Context, code string) error {
	return s.db.WithContext(ctx).Where("code = ?", code).Delete(&tokenModel{}).Error
}

func (s *mySQLTokenStore) SaveAll(ctx context.Context, tokens map[string]*config.Token) error {
	if len(tokens) == 0 {
		return nil
	}

	models := make([]tokenModel, 0, len(tokens))
	for code, token := range tokens {
		if token == nil {
			continue
		}
		models = append(models, tokenModel{
			Code:        code,
			NetworkCode: token.NetworkCode,
			MintAddress: token.MintAddress,
			Decimals:    token.Decimals,
		})
	}
	if len(models) == 0 {
		return nil
	}

	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "code"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"network_code", "mint_address", "decimals", "updated_at",
		}),
	}).Create(&models).Error
}
