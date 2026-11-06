package model

// MediaWork references existing private originals; the gallery never duplicates them.
type MediaWork struct {
	ID                 string `json:"id" gorm:"primaryKey"`
	UserID             string `json:"-" gorm:"index:idx_media_works_owner_created,priority:1"`
	ObjectID           string `json:"-" gorm:"uniqueIndex"`
	ThumbnailID        string `json:"-" gorm:"index"`
	ThumbnailState     string `json:"thumbnailState" gorm:"size:32"`
	ThumbnailCheckedAt string `json:"-"`
	Kind               string `json:"kind" gorm:"size:16;index"`
	Title              string `json:"title"`
	Prompt             string `json:"prompt" gorm:"type:text"`
	Model              string `json:"model"`
	Source             string `json:"source"`
	TaskID             string `json:"taskId" gorm:"index"`
	Width              int    `json:"width"`
	Height             int    `json:"height"`
	Bytes              int64  `json:"bytes"`
	MimeType           string `json:"mimeType"`
	CreatedAt          string `json:"createdAt" gorm:"index:idx_media_works_owner_created,priority:3"`
	DeletedAt          string `json:"-" gorm:"not null;default:'';index:idx_media_works_owner_created,priority:2"`
}
