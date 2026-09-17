// SPDX-FileContributor: Adam Tauber <asciimoo@gmail.com>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

var ErrWebSessionNotFound = errors.New("web session not found")

// WebSession stores browser session data under a hash of the opaque cookie
// value. The raw session identifier is never persisted.
type WebSession struct {
	ID        uint   `gorm:"primaryKey"`
	TokenHash string `gorm:"uniqueIndex;size:64;not null"`
	Data      []byte `gorm:"not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
	ExpiresAt time.Time `gorm:"index;not null"`
}

func CreateWebSession(session *WebSession) error {
	session.ExpiresAt = session.ExpiresAt.UTC()
	return DB.Create(session).Error
}

func GetWebSession(tokenHash string) (*WebSession, error) {
	var session WebSession
	if err := DB.Where("token_hash = ?", tokenHash).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrWebSessionNotFound
		}
		return nil, err
	}
	return &session, nil
}

func UpdateWebSession(tokenHash string, data []byte, expiresAt time.Time) error {
	result := DB.Model(&WebSession{}).Where("token_hash = ?", tokenHash).Updates(map[string]any{
		"data":       data,
		"expires_at": expiresAt.UTC(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrWebSessionNotFound
	}
	return nil
}

func DeleteWebSession(tokenHash string) error {
	return DB.Where("token_hash = ?", tokenHash).Delete(&WebSession{}).Error
}

func DeleteExpiredWebSessions(now time.Time) error {
	return DB.Where("expires_at <= ?", now.UTC()).Delete(&WebSession{}).Error
}
