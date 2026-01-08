// SPDX-FileContributor: Adam Tauber <asciimoo@gmail.com>
//
// SPDX-License-Identifier: AGPLv3+

package model

import (
	"errors"
)

type History struct {
	CommonFields
	Query string  `gorm:"unique" json:"query"`
	Links []*Link `gorm:"many2many:history_links;" json:"urls"`
}

type Link struct {
	CommonFields
	URL   string `gorm:"unique" json:"url"`
	Title string `json:"title"`
}

type HistoryLink struct {
	CommonFields
	HistoryID uint     `gorm:"uniqueIndex:historylinkuidx"`
	History   *History `json:"history"`
	LinkID    uint     `gorm:"uniqueIndex:historylinkuidx"`
	Link      *Link    `json:"link"`
	Count     uint     `gorm:"type:uint" json:"count"`
}

type URLCount struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Count uint   `json:"count"`
}

func GetOrCreateLink(u, title string) *Link {
	var ret *Link
	if err := DB.Model(&Link{}).Where("url = ?", u).First(&ret).Error; err != nil {
		ret = &Link{
			URL:   u,
			Title: title,
		}
		if err := DB.Create(ret).Error; err != nil {
			return nil
		}
	}
	return ret
}

func GetOrCreateHistory(q string) *History {
	var ret *History
	if err := DB.Model(&History{}).Where("query = ?", q).First(&ret).Error; err != nil {
		ret = &History{
			Query: q,
		}
		if err := DB.Create(ret).Error; err != nil {
			return nil
		}
	}
	return ret
}

func UpdateHistory(query, url, title string) error {
	l := GetOrCreateLink(url, title)
	h := GetOrCreateHistory(query)
	if l == nil || h == nil {
		return errors.New("failed to get link or query")
	}
	var hu *HistoryLink
	if err := DB.Model(&HistoryLink{}).Where("history_id = ? AND link_id = ?", h.ID, l.ID).First(&hu).Error; err != nil {
		hu = &HistoryLink{
			HistoryID: h.ID,
			LinkID:    l.ID,
			Count:     1,
		}
		return DB.Create(hu).Error
	}
	hu.Count += 1
	return DB.Save(hu).Error
}

func GetURLsByQuery(q string) ([]*URLCount, error) {
	var us []*URLCount
	err := DB.Select("links.url as url, links.title as title, history_links.count as count").
		Table("history_links").
		Joins("JOIN links ON history_links.link_id = links.id").
		Joins("JOIN histories ON history_links.history_id = histories.id").
		Where("histories.query = ?", q).
		Order("history_links.count desc").
		Limit(20).Find(&us).Error
	return us, err
}
