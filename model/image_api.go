package model

// ImageAPICredential never stores the bearer secret, only its SHA-256 hash.
type ImageAPICredential struct {
	ID         string `json:"id" gorm:"primaryKey"`
	UserID     string `json:"-" gorm:"index"`
	Name       string `json:"name"`
	Prefix     string `json:"prefix"`
	TokenHash  string `json:"-" gorm:"size:64;uniqueIndex"`
	ChannelID  string `json:"channelId"`
	Model      string `json:"model"`
	CreatedAt  string `json:"createdAt"`
	ExpiresAt  string `json:"expiresAt" gorm:"index"`
	LastUsedAt string `json:"lastUsedAt"`
	RevokedAt  string `json:"revokedAt"`
}

// ImageAPIJob reserves one user-scoped generation intent before any model call.
type ImageAPIJob struct {
	ID              string `json:"id" gorm:"primaryKey"`
	UserID          string `json:"-" gorm:"index:idx_image_api_jobs_user_created,priority:1"`
	Fingerprint     string `json:"-" gorm:"size:64"`
	SubmissionError string `json:"submissionError,omitempty"`
	CreatedAt       string `json:"createdAt" gorm:"index:idx_image_api_jobs_user_created,priority:2"`
}
