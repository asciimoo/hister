// SPDX-FileContributor: Adam Tauber <asciimoo@gmail.com>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"unicode"

	"github.com/asciimoo/hister/config"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type accountPolicy struct {
	userHandling  bool
	passwordLogin bool
	proxyLogin    bool
	localRecovery bool
}

// The database owner installs this immutable policy with each connection.
// Inspection and offline account creation do not enforce startup prerequisites.
var activeAccountPolicy accountPolicy

func accountPolicyFor(c *config.Config) accountPolicy {
	oidc := c.Server.OAuth["oidc"]
	return accountPolicy{
		userHandling: c.App.UserHandling, passwordLogin: !c.Server.OAuthOnly,
		proxyLogin:    c.Server.ProxyAuthHeader != "",
		localRecovery: c.App.UserHandling && oidc != nil && len(oidc.AdminGroups) > 0,
	}
}

// CheckAdministratorAvailability is a runtime prerequisite, not a loading or
// migration prerequisite, so offline administrator recovery remains possible.
func CheckAdministratorAvailability(c *config.Config) error {
	if !c.App.UserHandling {
		return nil
	}
	if DB == nil {
		return errors.New("cannot verify administrator availability: database unavailable")
	}
	var count int64
	policy := accountPolicyFor(c)
	if err := usableAdministrators(DB, policy, false).Count(&count).Error; err != nil {
		return errors.New("cannot verify administrator availability: database query failed")
	}
	if count == 0 {
		return errors.New("user handling requires an administrator with a usable credential; stop the server and run hister user create --offline --admin USERNAME")
	}
	if policy.localRecovery {
		if err := usableAdministrators(DB, policy, true).Count(&count).Error; err != nil {
			return errors.New("cannot verify recovery administrator availability: database query failed")
		}
		if count == 0 {
			return errors.New("OIDC role synchronization requires an independent local administrator with a usable password or API token; stop the server and run hister user create --offline --admin USERNAME")
		}
	}
	return nil
}

func usableAdministrators(db *gorm.DB, policy accountPolicy, local bool) *gorm.DB {
	credentials := db.Where("token <> ?", "")
	if policy.passwordLogin {
		credentials = credentials.Or("password <> ?", "")
	}
	if policy.proxyLogin && !local {
		credentials = credentials.Or("username <> ?", "")
	}
	query := db.Model(&User{}).Where("is_admin = ?", true).Where(credentials)
	if local {
		query = query.Where("o_auth_id = ?", "")
	}
	return query
}

var (
	ErrUserNotFound      = errors.New("user not found")
	ErrInvalidPassword   = errors.New("invalid password")
	ErrUserAlreadyExists = errors.New("user already exists")
	ErrEmptyPassword     = errors.New("password must not be empty")
	ErrInvalidUserUpdate = errors.New("invalid user update")
	ErrStaleUserRevision = errors.New("user revision has changed")
	ErrLastAdmin         = errors.New("cannot remove the last administrator")
	ErrLastRecoveryAdmin = errors.New("cannot remove the last independent local recovery administrator")
	ErrAdminRequired     = errors.New("administrator privileges required")
)

type User struct {
	CommonFields
	Username  string `gorm:"uniqueIndex" json:"username"`
	Password  string `json:"-"`
	Token     string `json:"-"`
	IsAdmin   bool   `json:"is_admin"`
	RulesJSON string `gorm:"column:rules_json;default:'{}'" json:"-"`
	OAuthID   string `gorm:"index" json:"-"`
	// Zero preserves sessions from before session versioning was introduced.
	SessionVersion uint   `gorm:"not null;default:0" json:"-"`
	Revision       uint64 `gorm:"not null;default:1" json:"-"`
}

type UserChanges struct {
	Username        *string
	Password        *string
	IsAdmin         *bool
	RegenerateToken bool
}

// userTransaction serializes privilege removals across connections and processes.
// Acquire the write lock before reading so two administrators cannot both remove
// the other's last remaining administrator privilege based on an outdated count.
func userTransaction(fn func(*gorm.DB) error) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var err error
		switch tx.Name() {
		case "postgres":
			err = tx.Exec("LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE").Error
		case "sqlite":
			// A no-op write acquires SQLite's writer lock without changing an account.
			err = tx.Exec("UPDATE users SET is_admin = is_admin WHERE 1 = 0").Error
		default:
			return ErrDBType
		}
		if err != nil {
			return err
		}
		return fn(tx)
	})
}

func checkAdminRemoval(tx *gorm.DB, user *User) error {
	if !user.IsAdmin {
		return nil
	}
	var count int64
	if err := tx.Model(&User{}).Where("is_admin = ?", true).Count(&count).Error; err != nil {
		return err
	}
	if count <= 1 {
		return ErrLastAdmin
	}
	if activeAccountPolicy.userHandling {
		if err := usableAdministrators(tx, activeAccountPolicy, false).Where("id <> ?", user.ID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return ErrLastAdmin
		}
	}
	if activeAccountPolicy.localRecovery {
		if err := usableAdministrators(tx, activeAccountPolicy, true).Where("id <> ?", user.ID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return ErrLastRecoveryAdmin
		}
	}
	return nil
}

// CheckUserDeletion checks before a document purge. Deletion checks again under
// the account write lock because another administrator can change in the interim.
func CheckUserDeletion(id uint) error {
	user, err := GetUserByID(id)
	if err != nil {
		return err
	}
	return checkAdminRemoval(DB, user)
}

// UpdateUser applies administrative changes together and rejects stale revisions.
// The caller supplies the authenticated administrator's ID, never a request field.
func UpdateUser(actorID, userID uint, revision uint64, changes UserChanges) (*User, error) {
	return updateUser(actorID, userID, revision, changes, false)
}

// RotateUserToken permits owners and administrators to rotate a token. General
// account updates still require administrator privileges, including for oneself.
func RotateUserToken(actorID, userID uint, revision uint64) (*User, error) {
	return updateUser(actorID, userID, revision, UserChanges{RegenerateToken: true}, true)
}

func updateUser(actorID, userID uint, revision uint64, changes UserChanges, allowSelfRotation bool) (*User, error) {
	if revision == 0 || revision >= math.MaxInt64 || (changes.Username == nil && changes.Password == nil && changes.IsAdmin == nil && !changes.RegenerateToken) {
		return nil, ErrInvalidUserUpdate
	}
	updates := map[string]any{"revision": gorm.Expr("revision + 1")}
	if changes.Username != nil {
		if strings.TrimSpace(*changes.Username) == "" || strings.ContainsFunc(*changes.Username, unicode.IsControl) {
			return nil, ErrInvalidUserUpdate
		}
		updates["username"] = *changes.Username
	}
	if changes.Password != nil {
		if len(*changes.Password) < 8 || len(*changes.Password) > 72 {
			return nil, ErrInvalidUserUpdate
		}
		// Hash before acquiring the database write lock.
		hash, err := bcrypt.GenerateFromPassword([]byte(*changes.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		updates["password"] = string(hash)
		updates["session_version"] = gorm.Expr("session_version + 1")
	}
	if changes.IsAdmin != nil {
		updates["is_admin"] = *changes.IsAdmin
	}
	if changes.RegenerateToken {
		updates["token"] = rand.Text()
	}
	var user User
	err := userTransaction(func(tx *gorm.DB) error {
		var actor User
		if err := tx.First(&actor, actorID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrAdminRequired
			}
			return err
		}
		if !actor.IsAdmin && (!allowSelfRotation || actorID != userID) {
			return ErrAdminRequired
		}
		if err := tx.First(&user, userID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrUserNotFound
			}
			return err
		}
		if user.Revision != revision {
			return ErrStaleUserRevision
		}
		if changes.IsAdmin != nil && !*changes.IsAdmin {
			if err := checkAdminRemoval(tx, &user); err != nil {
				return err
			}
		}
		if err := tx.Model(&user).Updates(updates).Error; err != nil {
			if translator, ok := tx.Dialector.(gorm.ErrorTranslator); ok {
				if errors.Is(translator.Translate(err), gorm.ErrDuplicatedKey) {
					return ErrUserAlreadyExists
				}
			}
			return err
		}
		return tx.First(&user, userID).Error
	})
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (u *User) ParseRules() (*config.Rules, error) {
	r := &config.Rules{
		Allow:      &config.Rule{ReStrs: []string{}},
		Skip:       &config.Rule{ReStrs: []string{}},
		Priority:   &config.Rule{ReStrs: []string{}},
		Versioning: &config.Rule{ReStrs: []string{}},
		Aliases:    make(config.Aliases),
	}
	if u.RulesJSON != "" && u.RulesJSON != "{}" {
		if err := json.Unmarshal([]byte(u.RulesJSON), r); err != nil {
			return nil, err
		}
		if r.Skip == nil {
			r.Skip = &config.Rule{ReStrs: []string{}}
		}
		if r.Allow == nil {
			r.Allow = &config.Rule{ReStrs: []string{}}
		}
		if r.Priority == nil {
			r.Priority = &config.Rule{ReStrs: []string{}}
		}
		if r.Versioning == nil {
			r.Versioning = &config.Rule{ReStrs: []string{}}
		}
		if r.Aliases == nil {
			r.Aliases = make(config.Aliases)
		}
	}
	if err := r.Compile(); err != nil {
		return nil, err
	}
	return r, nil
}

func GetUserRules(userID uint) (*config.Rules, error) {
	var u User
	if err := DB.Select("rules_json").First(&u, userID).Error; err != nil {
		return nil, ErrUserNotFound
	}
	return u.ParseRules()
}

func SaveUserRules(userID uint, rules *config.Rules) error {
	b, err := json.Marshal(rules)
	if err != nil {
		return err
	}
	return DB.Model(&User{}).Where("id = ?", userID).Update("rules_json", string(b)).Error
}

func CreateUser(username, password string, isAdmin bool) (*User, error) {
	var existing User
	if err := DB.Where("username = ?", username).First(&existing).Error; err == nil {
		return nil, ErrUserAlreadyExists
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if password == "" {
		return nil, ErrEmptyPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	u := &User{Username: username, Password: string(hash), Token: rand.Text(), IsAdmin: isAdmin}
	// A competing request can create the username after the initial lookup.
	result := DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "username"}}, DoNothing: true,
	}).Create(u)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrUserAlreadyExists
	}
	return u, nil
}

func DeleteUser(username string) error {
	return deleteUser("username = ?", username)
}

func DeleteUserByID(id uint) error {
	return deleteUser("id = ?", id)
}

func deleteUser(query string, value any) error {
	return userTransaction(func(tx *gorm.DB) error {
		var user User
		if err := tx.Where(query, value).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrUserNotFound
			}
			return err
		}
		if err := checkAdminRemoval(tx, &user); err != nil {
			return err
		}
		return tx.Delete(&user).Error
	})
}

func AuthenticateUser(username, password string) (*User, error) {
	var u User
	if err := DB.Where("username = ?", username).First(&u).Error; err != nil {
		return nil, ErrUserNotFound
	}
	if password == "" || u.Password == "" {
		return nil, ErrInvalidPassword
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(password)); err != nil {
		return nil, ErrInvalidPassword
	}
	return &u, nil
}

func GetUserByToken(token string) (*User, error) {
	var u User
	if err := DB.Where("token = ?", token).First(&u).Error; err != nil {
		return nil, ErrUserNotFound
	}
	return &u, nil
}

func RegenerateToken(userID uint) (string, error) {
	token := rand.Text()
	result := DB.Model(&User{}).Where("id = ?", userID).Updates(map[string]any{"token": token, "revision": gorm.Expr("revision + 1")})
	if result.Error != nil {
		return "", result.Error
	}
	if result.RowsAffected == 0 {
		return "", ErrUserNotFound
	}
	return token, nil
}

func GetUser(username string) (*User, error) {
	var u User
	if err := DB.Where("username = ?", username).First(&u).Error; err != nil {
		return nil, ErrUserNotFound
	}
	return &u, nil
}

func GetUserByID(id uint) (*User, error) {
	var u User
	if err := DB.First(&u, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &u, nil
}

// ListUsers returns account metadata ordered by ID. Credentials and rules are not loaded.
func ListUsers(username string, afterID uint, limit int) ([]User, error) {
	users := make([]User, 0)
	query := DB.Select("id", "username", "is_admin", "created_at", "updated_at", "revision").
		Where("id > ?", afterID).Order("id ASC").Limit(limit)
	if username != "" {
		query = query.Where("username = ?", username)
	}
	err := query.Find(&users).Error
	return users, err
}

func RegenerateTokenByUsername(username string) (string, error) {
	var u User
	if err := DB.Where("username = ?", username).First(&u).Error; err != nil {
		return "", ErrUserNotFound
	}
	return RegenerateToken(u.ID)
}

func UpdateUsername(username, newUsername string) error {
	var existing User
	if err := DB.Where("username = ?", newUsername).First(&existing).Error; err == nil {
		return ErrUserAlreadyExists
	}
	result := DB.Model(&User{}).Where("username = ?", username).Updates(map[string]any{"username": newUsername, "revision": gorm.Expr("revision + 1")})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrUserNotFound
	}
	return nil
}

func UpdatePassword(username, password string) error {
	if password == "" {
		return ErrEmptyPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	result := DB.Model(&User{}).Where("username = ?", username).Updates(map[string]any{
		"password":        string(hash),
		"session_version": gorm.Expr("session_version + 1"),
		"revision":        gorm.Expr("revision + 1"),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrUserNotFound
	}
	return nil
}

func GetUserByOAuthID(oauthID string) (*User, error) {
	var u User
	if err := DB.Where("o_auth_id = ?", oauthID).First(&u).Error; err != nil {
		return nil, ErrUserNotFound
	}
	return &u, nil
}

func CreateOAuthUser(username, oauthID string) (*User, error) {
	var existing User
	if err := DB.Where("username = ?", username).First(&existing).Error; err == nil {
		return nil, ErrUserAlreadyExists
	}
	u := &User{Username: username, Token: rand.Text(), OAuthID: oauthID}
	return u, DB.Create(u).Error
}

// SyncOAuthAdmin applies the role from an authenticated provider identity.
// Provider revocations remain authoritative. Runtime startup and local account
// mutations require and protect an independent recovery administrator when OIDC
// manages roles, rather than preserving a revoked provider privilege.
func SyncOAuthAdmin(oauthID string, isAdmin bool) (*User, error) {
	if oauthID == "" {
		return nil, ErrUserNotFound
	}
	var user User
	err := userTransaction(func(tx *gorm.DB) error {
		if err := tx.Where("o_auth_id = ?", oauthID).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrUserNotFound
			}
			return err
		}
		if user.IsAdmin == isAdmin {
			return nil
		}
		if err := tx.Model(&user).Updates(map[string]any{
			"is_admin": isAdmin, "revision": gorm.Expr("revision + 1"),
		}).Error; err != nil {
			return err
		}
		return tx.First(&user, user.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// CreateProxyUser creates an account for reverse-proxy authentication.
// Like CreateOAuthUser it leaves Password empty so password authentication
// stays disabled: AuthenticateUser rejects users with no stored password,
// so these credentials can never be used if proxy auth is later disabled.
func CreateProxyUser(username string) (*User, error) {
	var existing User
	if err := DB.Where("username = ?", username).First(&existing).Error; err == nil {
		return nil, ErrUserAlreadyExists
	}
	u := &User{Username: username, Token: rand.Text()}
	return u, DB.Create(u).Error
}

func ToggleAdmin(username string) (bool, error) {
	var newAdmin bool
	err := userTransaction(func(tx *gorm.DB) error {
		var user User
		if err := tx.Where("username = ?", username).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrUserNotFound
			}
			return err
		}
		if err := checkAdminRemoval(tx, &user); err != nil {
			return err
		}
		newAdmin = !user.IsAdmin
		return tx.Model(&user).Updates(map[string]any{"is_admin": newAdmin, "revision": gorm.Expr("revision + 1")}).Error
	})
	return newAdmin, err
}
