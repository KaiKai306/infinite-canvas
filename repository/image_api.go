package repository

import (
	"github.com/tigerowo/infinite-canvas/model"
	"gorm.io/gorm/clause"
)

func SaveImageAPICredential(item model.ImageAPICredential) error {
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Create(&item).Error
}

func ListImageAPICredentials(userID string) ([]model.ImageAPICredential, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	items := []model.ImageAPICredential{}
	err = db.Where("user_id = ?", userID).Order("created_at DESC").Find(&items).Error
	return items, err
}

func FindImageAPICredential(hash string) (model.ImageAPICredential, error) {
	db, err := DB()
	if err != nil {
		return model.ImageAPICredential{}, err
	}
	var item model.ImageAPICredential
	err = db.Where("token_hash = ?", hash).First(&item).Error
	return item, err
}

func RevokeImageAPICredential(userID, id, at string) error {
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Model(&model.ImageAPICredential{}).Where("user_id = ? AND id = ?", userID, id).Update("revoked_at", at).Error
}

func TouchImageAPICredential(id, at string) {
	db, err := DB()
	if err == nil {
		db.Model(&model.ImageAPICredential{}).Where("id = ? AND (last_used_at = '' OR last_used_at < ?)", id, at).Update("last_used_at", at)
	}
}

func ReserveImageAPIJob(item model.ImageAPIJob) (bool, error) {
	db, err := DB()
	if err != nil {
		return false, err
	}
	tx := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&item)
	return tx.RowsAffected == 1, tx.Error
}

func GetImageAPIJob(userID, id string) (model.ImageAPIJob, error) {
	db, err := DB()
	if err != nil {
		return model.ImageAPIJob{}, err
	}
	var item model.ImageAPIJob
	err = db.Where("user_id = ? AND id = ?", userID, id).First(&item).Error
	return item, err
}

func ListImageAPIJobs(userID string, page, pageSize int) ([]model.ImageAPIJob, int64, error) {
	db, err := DB()
	if err != nil {
		return nil, 0, err
	}
	query := db.Model(&model.ImageAPIJob{}).Where("user_id = ?", userID)
	var total int64
	if err = query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := []model.ImageAPIJob{}
	err = query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	return items, total, err
}

func FailImageAPIJob(userID, id, message string) error {
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Model(&model.ImageAPIJob{}).Where("user_id = ? AND id = ?", userID, id).Update("submission_error", message).Error
}

func ImageAPIJobCounts(userID, since string) (int64, int64, error) {
	db, err := DB()
	if err != nil {
		return 0, 0, err
	}
	var recent, active int64
	if err = db.Model(&model.ImageAPIJob{}).Where("user_id = ? AND created_at >= ?", userID, since).Count(&recent).Error; err != nil {
		return 0, 0, err
	}
	err = db.Table("image_api_jobs AS j").Joins("LEFT JOIN canvas_image_tasks AS t ON t.id = j.id AND t.user_id = j.user_id").Where("j.user_id = ? AND j.submission_error = '' AND (t.status IN ? OR (t.id IS NULL AND j.created_at >= ?))", userID, []string{"queued", "processing", "running", "in_progress"}, since).Count(&active).Error
	return recent, active, err
}

// Called before the HTTP listener starts: never replay a model request lost during restart.
func MarkInterruptedImageAPIRequests(at string) error {
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Model(&model.CanvasImageTask{}).Where("source = 'api' AND image_url = '' AND status IN ? AND EXISTS (SELECT 1 FROM image_api_jobs WHERE image_api_jobs.id = canvas_image_tasks.id AND image_api_jobs.user_id = canvas_image_tasks.user_id)", []string{"queued", "processing", "running", "in_progress"}).Updates(map[string]any{"status": "failed", "error": "api_interrupted_before_result", "error_detail": "服务重启前尚未保存上游结果，请核对后再决定是否重新提交", "updated_at": at, "completed_at": at}).Error
}
