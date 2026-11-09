package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"

	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
	"github.com/tigerowo/infinite-canvas/service"
)

func ImageAPIConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	cfg, err := service.CurrentImageAPIConfig(r.Context())
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, cfg)
}

func ImageAPICredentials(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	user, _ := service.UserFromContext(r.Context())
	if r.Method == http.MethodGet {
		items, err := repository.ListImageAPICredentials(user.ID)
		if err != nil {
			FailError(w, err)
			return
		}
		OK(w, items)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var input service.ImageAPICredentialInput
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil {
		FailWithStatus(w, 400, "凭证参数无效")
		return
	}
	item, secret, err := service.CreateImageAPICredential(r.Context(), input)
	if err != nil {
		Fail(w, err.Error())
		return
	}
	OK(w, map[string]any{"credential": item, "secret": secret})
}

func RevokeImageAPICredential(w http.ResponseWriter, r *http.Request, id string) {
	user, _ := service.UserFromContext(r.Context())
	if err := repository.RevokeImageAPICredential(user.ID, id, taskTime()); err != nil {
		FailError(w, err)
		return
	}
	OK(w, true)
}

func ImageAPICapabilities(w http.ResponseWriter, r *http.Request) {
	credential, _ := service.ImageAPICredentialFromContext(r.Context())
	OK(w, map[string]any{"version": "v1", "model": credential.Model, "channelId": credential.ChannelID, "maxCount": 4, "outputFormats": []string{"png", "gif"}, "gif": map[string]any{"models": "GPT Image 2.6+", "minFrames": 2, "maxFrames": 16, "minFrameDurationMs": 20, "maxFrameDurationMs": 2000, "streaming": false}, "maxReferences": 4, "maxReferenceBytes": 10 << 20, "referenceTypes": []string{"image/png", "image/jpeg", "image/webp"}, "submission": "POST /api/image/v1/jobs", "poll": "GET /api/image/v1/jobs/{id}", "download": "GET /api/image/v1/jobs/{id}/images/{index}", "authorization": "Authorization: Bearer <dedicated-image-key>", "idempotency": "Required Idempotency-Key: 8-128 letters, digits, _ or -. Retry the same key and identical input; conflicting input returns 409.", "jsonFields": []string{"prompt", "model", "size", "quality", "count", "output_format", "frame_count", "frame_duration_ms"}, "references": "multipart/form-data: the same text fields and up to four repeated image file fields; no remote reference URLs", "storage": "Results are archived to the account's private cloud storage and appear in /works; no browser or canvas required.", "rateLimit": "10 new jobs per user per minute, 3 active API jobs", "retry": "Poll every 3 seconds. A new intent uses a new key. Never automatically replay submission_unknown or a failed job under a new key.", "errors": "Nonzero code means failure; inspect HTTP status and msg. No new model call is made for an existing idempotency key."})
}

func ImageAPIJobs(w http.ResponseWriter, r *http.Request) {
	user, _ := service.UserFromContext(r.Context())
	if r.Method == http.MethodGet {
		q := parseQuery(r)
		q.Normalize()
		if q.PageSize > 100 {
			q.PageSize = 100
		}
		items, total, err := repository.ListImageAPIJobs(user.ID, q.Page, q.PageSize)
		if err != nil {
			FailError(w, err)
			return
		}
		result := []map[string]any{}
		for _, item := range items {
			result = append(result, service.ImageAPIJobResponse(item))
		}
		OK(w, map[string]any{"items": result, "total": total})
		return
	}
	unlock := service.ImageAPIUserLock(user.ID)
	defer unlock()
	r.Body = http.MaxBytesReader(w, r.Body, 42<<20)
	input, images, types, err := readImageAPIInput(r)
	if err != nil {
		FailWithStatus(w, 400, err.Error())
		return
	}
	credential, _ := service.ImageAPICredentialFromContext(r.Context())
	input, err = service.NormalizeImageAPIJobInput(input, credential)
	if err != nil {
		imageAPIError(w, err)
		return
	}
	if err := service.RequireImageAPIArchive(r.Context()); err != nil {
		imageAPIError(w, err)
		return
	}
	job, created, err := service.ReserveImageAPIJob(user.ID, r.Header.Get("Idempotency-Key"), service.ImageAPIJobFingerprint(credential, input, images))
	if err != nil {
		imageAPIError(w, err)
		return
	}
	if !created {
		OK(w, service.ImageAPIJobResponse(job))
		return
	}
	channel, err := service.SelectUserLocalModelChannelForModel(user.ID, input.Model, credential.ChannelID)
	if err != nil {
		_ = repository.FailImageAPIJob(user.ID, job.ID, "个人渠道或模型不可用，请检查账号配置")
		FailWithStatus(w, 400, "个人渠道或模型不可用，请检查账号配置")
		return
	}
	if input.OutputFormat == "gif" && strings.TrimSpace(channel.ParameterTranslation) != "" {
		_ = repository.FailImageAPIJob(user.ID, job.ID, "GIF 暂不支持参数转换模型")
		FailWithStatus(w, 400, "GIF 请使用原生 OpenAI Images 渠道，暂不支持参数转换模型")
		return
	}
	body, contentType, err := nativeImageAPIRequest(job, credential, input, images, types)
	if err != nil {
		_ = repository.FailImageAPIJob(user.ID, job.ID, "任务参数组装失败")
		FailWithStatus(w, 400, "任务参数组装失败")
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, "/api/v1/canvas/image-tasks", bytes.NewReader(body))
	if err != nil {
		FailWithStatus(w, 500, "任务请求失败")
		return
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set(userModelChannelHeader, credential.ChannelID)
	result := httptest.NewRecorder()
	CreateCanvasImageTask(result, req)
	var payload struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if json.Unmarshal(result.Body.Bytes(), &payload) != nil || payload.Code != 0 {
		message := "图片任务未接受，请检查个人渠道配置"
		_ = repository.FailImageAPIJob(user.ID, job.ID, message)
		FailWithStatus(w, 400, message)
		return
	}
	w.Header().Set("Location", "/api/image/v1/jobs/"+job.ID)
	writeJSONWithStatus(w, http.StatusAccepted, response{Code: 0, Data: service.ImageAPIJobResponse(job), Msg: "ok"})
}

func ImageAPIJob(w http.ResponseWriter, r *http.Request, id string) {
	user, _ := service.UserFromContext(r.Context())
	job, err := repository.GetImageAPIJob(user.ID, id)
	if err != nil {
		FailWithStatus(w, 404, "任务不存在")
		return
	}
	OK(w, service.ImageAPIJobResponse(job))
}

func ImageAPIJobImage(w http.ResponseWriter, r *http.Request, id, indexText string) {
	user, _ := service.UserFromContext(r.Context())
	if _, err := repository.GetImageAPIJob(user.ID, id); err != nil {
		FailWithStatus(w, 404, "任务不存在")
		return
	}
	task, found, err := repository.GetUserCanvasImageTask(user.ID, id)
	if err != nil || !found {
		FailWithStatus(w, 404, "任务不存在")
		return
	}
	if task.Status != "completed" {
		FailWithStatus(w, 409, "图片尚未完成归档")
		return
	}
	index, err := strconv.Atoi(indexText)
	if err != nil || index < 0 || index >= len(task.ImageStorage) || task.ImageStorage[index] == nil {
		FailWithStatus(w, 404, "图片不存在")
		return
	}
	ref := task.ImageStorage[index]
	objectID := strings.TrimPrefix(ref.StorageKey, "server:")
	object, err := service.StorageObjectInfo(r.Context(), objectID)
	if err != nil || object.CreatedBy != user.ID {
		FailWithStatus(w, 404, "图片不存在或已移除")
		return
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": "Kai-image-" + id + "-" + strconv.Itoa(index) + imageAPIExtension(ref.MimeType)}))
	FileContent(w, r, objectID)
}

func imageAPIError(w http.ResponseWriter, err error) {
	var apiErr service.ImageAPIHTTPError
	if errors.As(err, &apiErr) {
		if apiErr.Status == 429 {
			w.Header().Set("Retry-After", "15")
		}
		FailWithStatus(w, apiErr.Status, apiErr.Message)
		return
	}
	FailWithStatus(w, 500, "任务服务暂不可用，请使用原任务号重试")
}

func readImageAPIInput(r *http.Request) (service.ImageAPIJobInput, [][]byte, []string, error) {
	var input service.ImageAPIJobInput
	images := [][]byte{}
	types := []string{}
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return input, nil, nil, errors.New("请使用 JSON 或 multipart/form-data")
	}
	if contentType == "application/json" {
		body, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
		if err != nil || len(body) > 1<<20 {
			return input, nil, nil, errors.New("JSON 请求体不得超过1 MiB")
		}
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			return input, nil, nil, errors.New("生图参数无效或包含未知字段")
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			return input, nil, nil, errors.New("只接受一个 JSON 对象")
		}
		return input, images, types, nil
	}
	if contentType != "multipart/form-data" {
		return input, nil, nil, errors.New("请使用 JSON 或 multipart/form-data")
	}
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		return input, nil, nil, errors.New("参考图上传无效或超过总大小限制")
	}
	defer r.MultipartForm.RemoveAll()
	for field, values := range r.MultipartForm.Value {
		if len(values) != 1 {
			return input, nil, nil, errors.New("文本字段不能重复")
		}
		switch field {
		case "prompt":
			input.Prompt = values[0]
		case "model":
			input.Model = values[0]
		case "size":
			input.Size = values[0]
		case "quality":
			input.Quality = values[0]
		case "count":
			value, err := strconv.Atoi(values[0])
			if err != nil {
				return input, nil, nil, errors.New("count 必须为整数")
			}
			input.Count = value
		case "output_format":
			input.OutputFormat = values[0]
		case "frame_count", "frame_duration_ms":
			value, err := strconv.Atoi(values[0])
			if err != nil {
				return input, nil, nil, errors.New("帧数和每帧时长必须为整数")
			}
			if field == "frame_count" {
				input.FrameCount = value
			} else {
				input.FrameDurationMs = value
			}
		default:
			return input, nil, nil, errors.New("存在未知生图参数")
		}
	}
	for field, files := range r.MultipartForm.File {
		if field != "image" {
			return input, nil, nil, errors.New("参考图文件字段必须为 image")
		}
		if len(files) > 4 {
			return input, nil, nil, errors.New("最多使用4张参考图")
		}
		for _, header := range files {
			file, err := header.Open()
			if err != nil {
				return input, nil, nil, errors.New("参考图读取失败")
			}
			data, readErr := io.ReadAll(io.LimitReader(file, (10<<20)+1))
			file.Close()
			if readErr != nil || len(data) > 10<<20 {
				return input, nil, nil, errors.New("每张参考图不得超过10 MiB")
			}
			typeName := http.DetectContentType(data)
			if typeName != "image/png" && typeName != "image/jpeg" && typeName != "image/webp" {
				return input, nil, nil, errors.New("参考图只支持 PNG、JPEG 或 WebP")
			}
			dimensions, _, err := image.DecodeConfig(bytes.NewReader(data))
			if err != nil || dimensions.Width < 1 || dimensions.Height < 1 || int64(dimensions.Width)*int64(dimensions.Height) > 32000000 {
				return input, nil, nil, errors.New("参考图无效或超过3200万像素")
			}
			images = append(images, data)
			types = append(types, typeName)
		}
	}
	return input, images, types, nil
}

func nativeImageAPIRequest(job model.ImageAPIJob, credential model.ImageAPICredential, input service.ImageAPIJobInput, images [][]byte, types []string) ([]byte, string, error) {
	request := map[string]any{"model": input.Model, "prompt": input.Prompt, "n": input.Count}
	if input.OutputFormat == "gif" {
		request["output_format"], request["frame_count"], request["frame_duration_ms"] = "gif", input.FrameCount, input.FrameDurationMs
	}
	if input.Size != "auto" {
		request["size"] = input.Size
	}
	if input.Quality != "auto" {
		request["quality"] = input.Quality
	}
	if len(images) == 0 {
		body, err := json.Marshal(map[string]any{"source": "api", "sourceId": job.ID, "clientTaskId": job.ID, "prompt": input.Prompt, "request": request})
		return body, "application/json", err
	}
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	for key, value := range request {
		_ = writer.WriteField(key, strings.TrimSpace(toImageAPIString(value)))
	}
	for key, value := range map[string]string{"_canvas_endpoint": "/images/edits", "_canvas_source": "api", "_canvas_source_id": job.ID, "_canvas_task_id": job.ID, "_canvas_prompt": input.Prompt} {
		_ = writer.WriteField(key, value)
	}
	imageField := "image"
	if len(images) > 1 {
		imageField = "image[]"
	}
	for index, data := range images {
		part, err := writer.CreateFormFile(imageField, "reference-"+strconv.Itoa(index)+imageAPIExtension(types[index]))
		if err != nil {
			return nil, "", err
		}
		if _, err = part.Write(data); err != nil {
			return nil, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return buffer.Bytes(), writer.FormDataContentType(), nil
}

func toImageAPIString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	data, _ := json.Marshal(value)
	return string(data)
}
func imageAPIExtension(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
}
