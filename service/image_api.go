package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

type ImageAPIChannel struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Protocol string   `json:"protocol"`
	Models   []string `json:"models"`
}

type ImageAPIConfig struct {
	Channels         []ImageAPIChannel `json:"channels"`
	DefaultChannelID string            `json:"defaultChannelId"`
	DefaultModel     string            `json:"defaultModel"`
	ArchiveEnabled   bool              `json:"archiveEnabled"`
}

type ImageAPICredentialInput struct {
	Name      string `json:"name"`
	ChannelID string `json:"channelId"`
	Model     string `json:"model"`
	Days      int    `json:"days"`
}

type ImageAPIJobInput struct {
	Prompt  string `json:"prompt"`
	Model   string `json:"model,omitempty"`
	Size    string `json:"size,omitempty"`
	Quality string `json:"quality,omitempty"`
	Count   int    `json:"count,omitempty"`
}

type ImageAPIHTTPError struct {
	Status  int
	Message string
}

func (e ImageAPIHTTPError) Error() string { return e.Message }

var imageAPIUserLocks sync.Map
var imageAPIKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

func imageAPIHash(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func ImageAPIUserLock(userID string) func() {
	value, _ := imageAPIUserLocks.LoadOrStore(userID, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func CurrentImageAPIConfig(ctx context.Context) (ImageAPIConfig, error) {
	user, ok := UserFromContext(ctx)
	if !ok || user.ID == "" {
		return ImageAPIConfig{}, errors.New("请先登录")
	}
	result := ImageAPIConfig{Channels: []ImageAPIChannel{}, ArchiveEnabled: config.Cfg.ServerImageArchive}
	stored, found, err := repository.GetUserConfig(user.ID)
	if err != nil || !found {
		return result, err
	}
	var cfg struct {
		ImageModel     string                       `json:"imageModel"`
		Model          string                       `json:"model"`
		ImageChannelID string                       `json:"imageChannelId"`
		LocalChannels  []userLocalModelChannelInput `json:"localChannels"`
	}
	if err = json.Unmarshal([]byte(stored.ModelConfig), &cfg); err != nil {
		return result, errors.New("请先保存个人模型配置")
	}
	result.DefaultModel = firstNonEmpty(cfg.ImageModel, cfg.Model)
	for _, channel := range cfg.LocalChannels {
		if strings.TrimSpace(channel.ID) == "" || strings.TrimSpace(channel.APIKey) == "" || strings.TrimSpace(channel.BaseURL) == "" {
			continue
		}
		protocol := strings.ToLower(strings.TrimSpace(channel.Protocol))
		if protocol == "comfyui" || protocol == "autodl" || protocol == "runninghub" {
			continue
		}
		result.Channels = append(result.Channels, ImageAPIChannel{ID: channel.ID, Name: channel.Name, Protocol: protocol, Models: append([]string{}, channel.Models...)})
		if result.DefaultChannelID == "" || channel.ID == cfg.ImageChannelID {
			result.DefaultChannelID = channel.ID
		}
	}
	return result, nil
}

func CreateImageAPICredential(ctx context.Context, input ImageAPICredentialInput) (model.ImageAPICredential, string, error) {
	user, ok := UserFromContext(ctx)
	if !ok || user.ID == "" {
		return model.ImageAPICredential{}, "", errors.New("请先登录")
	}
	unlock := ImageAPIUserLock(user.ID)
	defer unlock()
	if !config.Cfg.ServerImageArchive {
		return model.ImageAPICredential{}, "", errors.New("站点尚未启用服务端图片归档")
	}
	cloud, err := HasActiveCloudStorage(ctx)
	if err != nil || !cloud {
		return model.ImageAPICredential{}, "", errors.New("站点云存储不可用，请联系管理员")
	}
	cfg, err := CurrentImageAPIConfig(ctx)
	if err != nil {
		return model.ImageAPICredential{}, "", err
	}
	input.ChannelID = firstNonEmpty(strings.TrimSpace(input.ChannelID), cfg.DefaultChannelID)
	input.Model = firstNonEmpty(strings.TrimSpace(input.Model), cfg.DefaultModel)
	allowed := false
	for _, channel := range cfg.Channels {
		if channel.ID == input.ChannelID {
			allowed = true
		}
	}
	if !allowed {
		return model.ImageAPICredential{}, "", errors.New("请先保存可用的个人图片渠道")
	}
	if _, err := SelectUserLocalModelChannelForModel(user.ID, input.Model, input.ChannelID); err != nil {
		return model.ImageAPICredential{}, "", errors.New("所选个人渠道或模型不可用，请检查配置")
	}
	items, err := repository.ListImageAPICredentials(user.ID)
	if err != nil {
		return model.ImageAPICredential{}, "", err
	}
	active := 0
	at := time.Now().UTC()
	for _, item := range items {
		expires, _ := time.Parse(time.RFC3339, item.ExpiresAt)
		if item.RevokedAt == "" && expires.After(at) {
			active++
		}
	}
	if active >= 10 {
		return model.ImageAPICredential{}, "", errors.New("最多保留10个有效生图凭证，请先撤销不用的凭证")
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		input.Name = "我的 AI"
	}
	if len([]rune(input.Name)) > 64 {
		return model.ImageAPICredential{}, "", errors.New("名称最多64个字符")
	}
	if input.Days == 0 {
		input.Days = 7
	}
	if input.Days < 1 || input.Days > 30 {
		return model.ImageAPICredential{}, "", errors.New("有效期为1至30天")
	}
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return model.ImageAPICredential{}, "", err
	}
	secret := "kai_img_" + base64.RawURLEncoding.EncodeToString(secretBytes)
	item := model.ImageAPICredential{ID: uuid.NewString(), UserID: user.ID, Name: input.Name, Prefix: secret[:12] + "…" + secret[len(secret)-4:], TokenHash: imageAPIHash([]byte(secret)), ChannelID: input.ChannelID, Model: input.Model, CreatedAt: at.Format(time.RFC3339), ExpiresAt: at.Add(time.Duration(input.Days) * 24 * time.Hour).Format(time.RFC3339)}
	err = repository.SaveImageAPICredential(item)
	return item, secret, err
}

func AuthenticateImageAPI(secret string) (model.AuthUser, model.ImageAPICredential, bool) {
	if len(secret) > 100 || !strings.HasPrefix(secret, "kai_img_") {
		return model.AuthUser{}, model.ImageAPICredential{}, false
	}
	item, err := repository.FindImageAPICredential(imageAPIHash([]byte(secret)))
	if err != nil || item.RevokedAt != "" {
		return model.AuthUser{}, item, false
	}
	expires, err := time.Parse(time.RFC3339, item.ExpiresAt)
	if err != nil || !expires.After(time.Now()) {
		return model.AuthUser{}, item, false
	}
	user, found, err := repository.GetUserByID(item.UserID)
	if err != nil || !found || user.Status == model.UserStatusBan || user.Role == model.UserRoleGuest {
		return model.AuthUser{}, item, false
	}
	auth := model.PublicUser(user)
	auth.Role = model.UserRoleUser // A dedicated key never inherits administrative file access.
	repository.TouchImageAPICredential(item.ID, time.Now().UTC().Truncate(time.Minute).Format(time.RFC3339))
	return auth, item, true
}

type imageAPICredentialContextKey struct{}

func WithImageAPICredential(ctx context.Context, item model.ImageAPICredential) context.Context {
	return context.WithValue(ctx, imageAPICredentialContextKey{}, item)
}
func ImageAPICredentialFromContext(ctx context.Context) (model.ImageAPICredential, bool) {
	item, ok := ctx.Value(imageAPICredentialContextKey{}).(model.ImageAPICredential)
	return item, ok
}

func RequireImageAPIArchive(ctx context.Context) error {
	if !config.Cfg.ServerImageArchive {
		return ImageAPIHTTPError{503, "站点暂未启用图片归档，请稍后使用原任务号重试"}
	}
	available, err := HasActiveCloudStorage(ctx)
	if err != nil || !available {
		return ImageAPIHTTPError{503, "站点云存储当前不可用，请稍后使用原任务号重试"}
	}
	return nil
}

func NormalizeImageAPIJobInput(input ImageAPIJobInput, credential model.ImageAPICredential) (ImageAPIJobInput, error) {
	input.Prompt = strings.TrimSpace(input.Prompt)
	if len([]rune(input.Prompt)) < 1 || len([]rune(input.Prompt)) > 12000 {
		return input, ImageAPIHTTPError{400, "提示词为1至12000个字符"}
	}
	input.Model = firstNonEmpty(strings.TrimSpace(input.Model), credential.Model)
	input.Size = firstNonEmpty(strings.TrimSpace(input.Size), "auto")
	input.Quality = firstNonEmpty(strings.TrimSpace(input.Quality), "auto")
	if len(input.Model) > 200 || len(input.Size) > 32 || len(input.Quality) > 32 {
		return input, ImageAPIHTTPError{400, "模型或图片参数过长"}
	}
	if input.Count == 0 {
		input.Count = 1
	}
	if input.Count < 1 || input.Count > 4 {
		return input, ImageAPIHTTPError{400, "单次生成张数为1至4张"}
	}
	return input, nil
}

// Caller holds the per-user submission lock through native task acceptance.
func ReserveImageAPIJob(userID, key, fingerprint string) (model.ImageAPIJob, bool, error) {
	if !imageAPIKeyPattern.MatchString(key) {
		return model.ImageAPIJob{}, false, ImageAPIHTTPError{400, "请提供8至128位的 Idempotency-Key（字母、数字、下划线或短横线）"}
	}
	id := "api-image-" + imageAPIHash([]byte(userID+"\x00"+key))
	if saved, err := repository.GetImageAPIJob(userID, id); err == nil {
		if saved.Fingerprint != fingerprint {
			return saved, false, ImageAPIHTTPError{409, "相同任务号的参数或参考图不同，请使用新的任务号"}
		}
		return saved, false, nil
	}
	recent, active, err := repository.ImageAPIJobCounts(userID, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339))
	if err != nil {
		return model.ImageAPIJob{}, false, err
	}
	if recent >= 10 || active >= 3 {
		return model.ImageAPIJob{}, false, ImageAPIHTTPError{429, "提交过于频繁或已有3个进行中任务，请稍后重试"}
	}
	job := model.ImageAPIJob{ID: id, UserID: userID, Fingerprint: fingerprint, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	created, err := repository.ReserveImageAPIJob(job)
	if err != nil {
		return job, false, err
	}
	if !created {
		return job, false, ImageAPIHTTPError{409, "任务号正在处理，请使用原参数重试"}
	}
	return job, true, nil
}

func ImageAPIJobFingerprint(credential model.ImageAPICredential, input ImageAPIJobInput, images [][]byte) string {
	base, _ := json.Marshal(struct {
		ChannelID string
		Input     ImageAPIJobInput
	}{credential.ChannelID, input})
	sum := sha256.New()
	sum.Write(base)
	for _, data := range images {
		hash := sha256.Sum256(data)
		sum.Write(hash[:])
	}
	return hex.EncodeToString(sum.Sum(nil))
}

func ImageAPIJobResponse(job model.ImageAPIJob) map[string]any {
	result := map[string]any{"id": job.ID, "createdAt": job.CreatedAt, "status": "submission_unknown", "images": []any{}, "statusUrl": "/api/image/v1/jobs/" + job.ID, "error": "提交状态待核对；不要用新任务号自动重复生成"}
	task, found, err := repository.GetUserCanvasImageTask(job.UserID, job.ID)
	if err != nil || !found {
		if job.SubmissionError != "" {
			result["status"], result["error"] = "failed", job.SubmissionError
		}
		return result
	}
	result["status"], result["progress"], result["model"], result["prompt"], result["error"] = task.Status, task.Progress, task.Model, task.Prompt, nil
	if task.Status == "processing" && task.ImageURL != "" {
		result["phase"] = "archiving"
	}
	if task.Error == "api_interrupted_before_result" {
		result["status"], result["error"] = "submission_unknown", "服务在收到模型结果前重启；请核对结果或扣费，不要自动重新生成"
	} else if task.Status == "failed" || task.Status == "cancelled" {
		result["error"] = "任务未完成，请在本站检查生成记录后再决定是否重新提交"
	}
	if task.Status == "completed" {
		refs := task.ImageStorage
		if len(refs) == 0 && task.StorageKey != "" {
			refs = []*model.CanvasTaskImage{{StorageKey: task.StorageKey, MimeType: task.MimeType, Width: task.Width, Height: task.Height, Bytes: task.Bytes}}
		}
		images := []map[string]any{}
		for index, ref := range refs {
			if ref != nil {
				images = append(images, map[string]any{"index": index, "mimeType": ref.MimeType, "width": ref.Width, "height": ref.Height, "bytes": ref.Bytes, "downloadUrl": fmt.Sprintf("/api/image/v1/jobs/%s/images/%d", job.ID, index)})
			}
		}
		result["images"] = images
	}
	return result
}
