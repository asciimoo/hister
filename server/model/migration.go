// SPDX-FileContributor: Adam Tauber <asciimoo@gmail.com>
//
// SPDX-License-Identifier: AGPLv3+

package model

import (
	"github.com/rs/zerolog/log"
)

var migrationFunctions = []func() error{}

func migrate() error {
	var dbVer int64
	err := DB.Model(&Database{}).
		Select("version").
		First(&dbVer).Error
	if err != nil {
		// cannot query the version -> uninitialized database -> no need to migrate
		DB.Save(&Database{Version: 0})
		//lint:ignore nilerr // no need to check error
		return nil
	}
	migCount := 0
	for i, m := range migrationFunctions {
		if int64(i) >= dbVer {
			log.Info().Msgf("Migrating DB to version %d", i+1)
			err := m()
			if err != nil {
				return err
			}
			dbVer = int64(i) + 1
			DB.Model(&Database{}).Where("id = 1").Update("version", dbVer)
			migCount++
		}
	}
	if migCount > 0 {
		log.Debug().Int("Migrations performed", migCount).Msg("DB migrations completed")
	}
	return nil
}
