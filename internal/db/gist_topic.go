package db

import "strings"

type GistTopic struct {
	GistID uint   `gorm:"primaryKey"`
	Topic  string `gorm:"primaryKey;size:50"`
}

// ReplaceGistTopics overwrites the topics for a gist with the given list,
// deduplicating + trimming whitespace and skipping empty values. The
// validator on the GistDTO form path enforces the 50-char limit and the
// max-10-topics rule; here we mirror only the bare-minimum hygiene because
// the API caller is already authenticated and may legitimately want to
// clear all topics by passing an empty array.
func ReplaceGistTopics(gistID uint, topics []string) error {
	tx := db.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	if err := tx.Where("gist_id = ?", gistID).Delete(&GistTopic{}).Error; err != nil {
		tx.Rollback()
		return err
	}
	seen := make(map[string]struct{}, len(topics))
	for _, raw := range topics {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		if err := tx.Create(&GistTopic{GistID: gistID, Topic: t}).Error; err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit().Error
}

// GetTopicsByUserID returns every distinct topic used across gists owned by
// the given user, ordered alphabetically.
func GetTopicsByUserID(userID uint) ([]string, error) {
	var topics []string
	err := db.Model(&GistTopic{}).
		Joins("JOIN gists ON gists.id = gist_topics.gist_id").
		Where("gists.user_id = ?", userID).
		Distinct("topic").
		Order("topic ASC").
		Pluck("topic", &topics).Error
	return topics, err
}
