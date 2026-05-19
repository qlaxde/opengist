package db

import (
	"time"
)

type GistComment struct {
	ID        uint   `gorm:"primaryKey"`
	GistID    uint   `gorm:"index:idx_gist_comments_gist_id,priority:1"`
	UserID    uint
	Content   string `gorm:"type:text"`
	CreatedAt int64  `gorm:"index:idx_gist_comments_gist_id,priority:2"`
	UpdatedAt int64

	Gist Gist `gorm:"constraint:OnDelete:CASCADE;" validate:"-"`
	User User `gorm:"constraint:OnDelete:CASCADE;" validate:"-"`
}

func (c *GistComment) Create() error {
	now := time.Now().Unix()
	c.CreatedAt = now
	c.UpdatedAt = now
	return db.Create(c).Error
}

func (c *GistComment) Update() error {
	c.UpdatedAt = time.Now().Unix()
	return db.Save(c).Error
}

func (c *GistComment) Delete() error {
	return db.Delete(c).Error
}

func GetCommentByID(id uint) (*GistComment, error) {
	c := new(GistComment)
	err := db.Preload("User").Where("id = ?", id).First(c).Error
	return c, err
}

func GetCommentsByGistID(gistID uint) ([]*GistComment, error) {
	var comments []*GistComment
	err := db.Preload("User").
		Where("gist_id = ?", gistID).
		Order("created_at asc").
		Find(&comments).Error
	return comments, err
}
