package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/community-outpost/gatehouse/internal/callback"
)

type Store struct {
	db                 *sqlx.DB
	lockTimeoutSeconds int
	usersTable         string
	pendingLoginsTable string
}

func New(db *sqlx.DB, usersTable, pendingLoginsTable string, lockTimeoutSeconds int) *Store {
	return &Store{
		db:                 db,
		lockTimeoutSeconds: lockTimeoutSeconds,
		usersTable:         quotedTable(usersTable),
		pendingLoginsTable: quotedTable(pendingLoginsTable),
	}
}

func (s *Store) EnsureUser(ctx context.Context, userID int64) error {
	if userID <= 0 {
		return nil
	}

	query := `
INSERT INTO ` + s.usersTable + ` (user_id, account_type, displayname)
VALUES (?, -1, ?)
ON DUPLICATE KEY UPDATE user_id = VALUES(user_id)`
	if _, err := s.db.ExecContext(ctx, query, userID, randomDisplayName()); err != nil {
		return fmt.Errorf("ensure user: %w", err)
	}
	return nil
}

var displayNameAdjectives = [...]string{
	"Agile", "Amber", "Brave", "Bright", "Calm", "Clever", "Cosmic", "Daring",
	"Electric", "Gentle", "Golden", "Happy", "Jolly", "Lucky", "Mighty", "Nimble",
	"Rapid", "Silver", "Solar", "Steady", "Swift", "Turbo", "Valiant", "Wild",
}

var displayNameNouns = [...]string{
	"Badger", "Bear", "Bison", "Cobra", "Eagle", "Falcon", "Fox", "Gazelle",
	"Gecko", "Hawk", "Heron", "Jaguar", "Koala", "Lynx", "Mantis", "Otter",
	"Owl", "Panda", "Raven", "Shark", "Tiger", "Toucan", "Wolf", "Yak",
}

func randomDisplayName() string {
	adjective := displayNameAdjectives[rand.IntN(len(displayNameAdjectives))]
	noun := displayNameNouns[rand.IntN(len(displayNameNouns))]
	return fmt.Sprintf("%s%s%d", adjective, noun, rand.IntN(10_000))
}

func (s *Store) SavePendingLogin(ctx context.Context, authCallback callback.AuthCallback) error {
	connection, err := s.db.Connx(ctx)
	if err != nil {
		return fmt.Errorf("reserve MySQL connection: %w", err)
	}
	defer connection.Close()

	lockName := "gatehouse:login:" + authCallback.Code
	var acquired sql.NullInt64
	if err := connection.GetContext(ctx, &acquired, "SELECT GET_LOCK(?, ?)", lockName, s.lockTimeoutSeconds); err != nil {
		return fmt.Errorf("acquire pending-login lock: %w", err)
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		return errors.New("timed out acquiring pending-login lock")
	}
	defer func() {
		releaseContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = connection.ExecContext(releaseContext, "SELECT RELEASE_LOCK(?)", lockName)
	}()

	transaction, err := connection.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin pending-login transaction: %w", err)
	}
	defer transaction.Rollback()

	if _, err := transaction.ExecContext(ctx, "DELETE FROM "+s.pendingLoginsTable+" WHERE code = ?", authCallback.Code); err != nil {
		return fmt.Errorf("replace pending login: %w", err)
	}
	state := 2
	if authCallback.Success {
		state = 1
	}
	userID := authCallback.UserID
	if userID <= 0 {
		userID = -1
	}
	if _, err := transaction.ExecContext(ctx,
		"INSERT INTO "+s.pendingLoginsTable+" (code, state, created, user_id) VALUES (?, ?, UTC_TIMESTAMP(), ?)",
		authCallback.Code, state, userID); err != nil {
		return fmt.Errorf("insert pending login: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit pending login: %w", err)
	}
	return nil
}

func quotedTable(table string) string {
	return "`" + table + "`"
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}
