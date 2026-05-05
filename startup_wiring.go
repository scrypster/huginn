package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/config"
	"github.com/scrypster/huginn/internal/connections"
	modelslib "github.com/scrypster/huginn/internal/models"
	"github.com/scrypster/huginn/internal/notification"
	"github.com/scrypster/huginn/internal/runtime"
	"github.com/scrypster/huginn/internal/server"
	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

func newServerWithRuntime(
	cfg config.Config,
	orch *agent.Orchestrator,
	sessStore session.StoreInterface,
	token, huginnHome string,
	connMgr *connections.Manager,
	connStore connections.StoreInterface,
	connProviders []connections.IntegrationProvider,
) *server.Server {
	srv := server.New(cfg, orch, sessStore, token, huginnHome, connMgr, connStore, connProviders)
	runtimeMgr, runtimeMgrErr := runtime.NewManager(huginnHome)
	if runtimeMgrErr != nil {
		return srv
	}
	modelStore, modelStoreErr := modelslib.NewStore(huginnHome)
	if modelStoreErr != nil {
		return srv
	}
	srv.SetRuntimeManager(runtimeMgr)
	srv.SetModelStore(modelStore)
	return srv
}

func wireNotifications(huginnHome string, sqlDB *sqlitedb.DB, srv *server.Server) (notification.StoreInterface, []func(), error) {
	notifDir := filepath.Join(huginnHome, "store", "notifications")
	if err := os.MkdirAll(notifDir, 0755); err != nil {
		return nil, nil, fmt.Errorf("huginn: create notification store dir: %w", err)
	}
	notifDB, err := pebble.Open(notifDir, &pebble.Options{})
	if err != nil {
		return nil, nil, fmt.Errorf("huginn: open notification store: %w", err)
	}
	cleanup := []func(){func() { _ = notifDB.Close() }}

	notifStore, notifBootstrap, notifBootErr := notification.Bootstrap(
		notifDB,
		sqlDB,
		notification.BootstrapOptions{},
	)
	if notifBootErr != nil {
		return nil, cleanup, fmt.Errorf("huginn: notification bootstrap: %w", notifBootErr)
	}
	if notifBootstrap.MigrationError != "" {
		fmt.Fprintf(os.Stderr, "huginn: warning: notification migration fallback to pebble: %s\n", notifBootstrap.MigrationError)
	} else if notifBootstrap.Migration.Name != "" {
		fmt.Fprintf(
			os.Stderr,
			"huginn: notification migration: scanned=%d migrated=%d skipped_malformed=%d already_complete=%t deleted_source=%t\n",
			notifBootstrap.Migration.ScannedRecords,
			notifBootstrap.Migration.MigratedRecords,
			notifBootstrap.Migration.SkippedMalformed,
			notifBootstrap.Migration.AlreadyComplete,
			notifBootstrap.Migration.DeletedSource,
		)
	}

	prunerCtx, prunerCancel := context.WithCancel(context.Background())
	cleanup = append(cleanup, prunerCancel)
	if sqlNotifStore, ok := notifStore.(*notification.SQLiteNotificationStore); ok {
		if _, pruneErr := sqlNotifStore.PruneExpired(context.Background()); pruneErr != nil {
			fmt.Fprintf(os.Stderr, "huginn: warning: startup notification prune: %v\n", pruneErr)
		}
		sqlNotifStore.StartPruner(prunerCtx, 15*time.Minute)
	} else if pebbleNotifStore, ok := notifStore.(*notification.Store); ok {
		pebbleNotifStore.StartPruner(prunerCtx, 15*time.Minute)
	} else {
		return nil, cleanup, fmt.Errorf("huginn: unsupported notification store type %T", notifStore)
	}
	srv.SetNotificationStore(notifStore)
	return notifStore, cleanup, nil
}
