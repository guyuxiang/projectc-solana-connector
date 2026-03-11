package service

import (
	"sync"

	"github.com/guyuxiang/projectc-solana-connector/pkg/config"
	"github.com/guyuxiang/projectc-solana-connector/pkg/mq"
	"github.com/guyuxiang/projectc-solana-connector/pkg/store"
)

var (
	appOnce sync.Once
	appInst *App
)

type App struct {
	Chain        ChainService
	Subscription SubscriptionService
}

func GetApp() *App {
	appOnce.Do(func() {
		cfg := config.GetConfig()
		publisher := mq.NewCallbackPublisher(cfg)
		chain := NewChainService(cfg)
		subscriptionStore, err := store.NewSubscriptionStore(cfg)
		if err != nil {
			panic(err)
		}
		subscription := NewSubscriptionService(cfg, chain, publisher, subscriptionStore)
		appInst = &App{
			Chain:        chain,
			Subscription: subscription,
		}
	})
	return appInst
}
