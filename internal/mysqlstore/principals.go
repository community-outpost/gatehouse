package mysqlstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"

	"github.com/community-outpost/gatehouse/internal/identity"
)

type PrincipalResolver struct {
	db                 *sqlx.DB
	usersTable         string
	principalsTable    string
	lockTimeoutSeconds int
}

func NewPrincipalResolver(db *sqlx.DB, usersTable, principalsTable string, lockTimeoutSeconds int) *PrincipalResolver {
	return &PrincipalResolver{
		db:                 db,
		usersTable:         quotedTable(usersTable),
		principalsTable:    quotedTable(principalsTable),
		lockTimeoutSeconds: lockTimeoutSeconds,
	}
}

func (r *PrincipalResolver) FindUser(ctx context.Context, issuer, subject string) (int64, bool, error) {
	if err := validatePrincipal(issuer, subject); err != nil {
		return 0, false, err
	}
	var userID int64
	err := r.db.GetContext(ctx, &userID,
		"SELECT user_id FROM "+r.principalsTable+" WHERE issuer = ? AND subject = ?",
		strings.TrimSpace(issuer), strings.TrimSpace(subject))
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("resolve principal: %w", err)
	}
	return userID, true, nil
}

func (r *PrincipalResolver) DisplayNameAvailable(ctx context.Context, displayName string) (bool, error) {
	if !identity.ValidStoredDisplayName(displayName) {
		return false, errors.New("display name is invalid")
	}
	var exists bool
	if err := r.db.GetContext(ctx, &exists,
		"SELECT EXISTS(SELECT 1 FROM "+r.usersTable+" WHERE displayname = ?)", displayName); err != nil {
		return false, fmt.Errorf("check display name availability: %w", err)
	}
	return !exists, nil
}

func (r *PrincipalResolver) ResolveOrCreateUser(ctx context.Context, issuer, subject, displayName string) (int64, error) {
	issuer = strings.TrimSpace(issuer)
	subject = strings.TrimSpace(subject)
	if err := validatePrincipal(issuer, subject); err != nil {
		return 0, err
	}
	if displayName == "" {
		var err error
		displayName, err = randomDisplayName()
		if err != nil {
			return 0, err
		}
	} else if !identity.ValidStoredDisplayName(displayName) {
		return 0, errors.New("display name is invalid")
	}

	connection, err := r.db.Connx(ctx)
	if err != nil {
		return 0, fmt.Errorf("reserve principal connection: %w", err)
	}
	defer connection.Close()

	lockDigest := sha256.Sum256([]byte(issuer + "\x00" + subject))
	lockName := "gatehouse:principal:" + hex.EncodeToString(lockDigest[:20])
	var acquired sql.NullInt64
	if err := connection.GetContext(ctx, &acquired, "SELECT GET_LOCK(?, ?)", lockName, r.lockTimeoutSeconds); err != nil {
		return 0, fmt.Errorf("acquire principal lock: %w", err)
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		return 0, errors.New("timed out acquiring principal lock")
	}
	defer func() {
		releaseContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = connection.ExecContext(releaseContext, "SELECT RELEASE_LOCK(?)", lockName)
	}()

	var existingUserID int64
	err = connection.GetContext(ctx, &existingUserID,
		"SELECT user_id FROM "+r.principalsTable+" WHERE issuer = ? AND subject = ?", issuer, subject)
	if err == nil {
		return existingUserID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("resolve principal: %w", err)
	}

	transaction, err := connection.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, fmt.Errorf("begin principal transaction: %w", err)
	}
	defer transaction.Rollback()

	result, err := transaction.ExecContext(ctx,
		"INSERT INTO "+r.usersTable+" (account_type, displayname) VALUES (-1, ?)", displayName)
	if err != nil {
		var mysqlError *mysql.MySQLError
		if errors.As(err, &mysqlError) && mysqlError.Number == 1062 {
			return 0, identity.ErrDisplayNameUnavailable
		}
		return 0, fmt.Errorf("create local user: %w", err)
	}
	userID, err := result.LastInsertId()
	if err != nil || userID <= 0 {
		return 0, errors.New("create local user did not return a positive ID")
	}
	if _, err := transaction.ExecContext(ctx,
		"INSERT INTO "+r.principalsTable+" (issuer, subject, user_id) VALUES (?, ?, ?)", issuer, subject, userID); err != nil {
		return 0, fmt.Errorf("create login principal: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return 0, fmt.Errorf("commit principal transaction: %w", err)
	}
	return userID, nil
}

func validatePrincipal(issuer, subject string) error {
	issuer = strings.TrimSpace(issuer)
	subject = strings.TrimSpace(subject)
	if issuer == "" || len(issuer) > 64 || subject == "" || len(subject) > 255 {
		return errors.New("principal issuer or subject is invalid")
	}
	return nil
}
