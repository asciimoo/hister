// SPDX-FileContributor: Adam Tauber <asciimoo@gmail.com>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"crypto/rand"
	"encoding/json"
	"errors"

	"github.com/asciimoo/hister/config"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrUserNotFound      = errors.New("user not found")
	ErrInvalidPassword   = errors.New("invalid password")
	ErrUserAlreadyExists = errors.New("user already exists")
)

type User struct {
	CommonFields
	Username  string `gorm:"uniqueIndex" json:"username"`
	Password  string `json:"-"`
	Token     string `json:"-"`
	IsAdmin   bool   `json:"is_admin"`
	RulesJSON string `gorm:"column:rules_json;default:'{}'" json:"-"`
	OAuthID   string `gorm:"index" json:"-"`
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
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	u := &User{Username: username, Password: string(hash), Token: rand.Text(), IsAdmin: isAdmin}
	return u, DB.Create(u).Error
}

func DeleteUser(username string) error {
	result := DB.Where("username = ?", username).Delete(&User{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrUserNotFound
	}
	return nil
}

func AuthenticateUser(username, password string) (*User, error) {
	var u User
	if err := DB.Where("username = ?", username).First(&u).Error; err != nil {
		return nil, ErrUserNotFound
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
	if err := DB.Model(&User{}).Where("id = ?", userID).Update("token", token).Error; err != nil {
		return "", err
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
		return nil, ErrUserNotFound
	}
	return &u, nil
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
	result := DB.Model(&User{}).Where("username = ?", username).Update("username", newUsername)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrUserNotFound
	}
	return nil
}

func UpdatePassword(username, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	result := DB.Model(&User{}).Where("username = ?", username).Update("password", string(hash))
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

func ToggleAdmin(username string) (bool, error) {
	var u User
	if err := DB.Where("username = ?", username).First(&u).Error; err != nil {
		return false, ErrUserNotFound
	}
	newAdmin := !u.IsAdmin
	if err := DB.Model(&u).Update("is_admin", newAdmin).Error; err != nil {
		return false, err
	}
	return newAdmin, nil
}
