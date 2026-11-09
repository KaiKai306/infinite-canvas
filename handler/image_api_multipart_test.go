package handler

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/http/httptest"
	"testing"

	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/service"
)

func TestNativeImageAPIReferenceFields(t *testing.T) {
	for _, count := range []int{1, 2, 4} {
		images := make([][]byte, count)
		types := make([]string, count)
		for i := range images {
			images[i] = append([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, byte(i))
			types[i] = "image/png"
		}
		body, contentType, err := nativeImageAPIRequest(model.ImageAPIJob{ID: "fixture"}, model.ImageAPICredential{}, service.ImageAPIJobInput{Model: "gpt-image-1", Prompt: "combine references", Count: 1, Size: "auto", Quality: "auto"}, images, types)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		r.Header.Set("Content-Type", contentType)
		forwarded, forwardedType, endpoint, source, _, _, taskID, _, _, err := readCanvasTaskAIRequest(r, "/images/generations")
		if err != nil || endpoint != "/images/edits" || source != "api" || taskID != "fixture" {
			t.Fatalf("task metadata changed: endpoint=%q source=%q id=%q err=%v", endpoint, source, taskID, err)
		}
		_, params, err := mime.ParseMediaType(forwardedType)
		if err != nil {
			t.Fatal(err)
		}
		form, err := multipart.NewReader(bytes.NewReader(forwarded), params["boundary"]).ReadForm(1 << 20)
		if err != nil {
			t.Fatal(err)
		}
		field := "image"
		if count > 1 {
			field = "image[]"
		}
		files := form.File[field]
		if len(form.File) != 1 || len(files) != count || form.Value["model"][0] != "gpt-image-1" || form.Value["prompt"][0] != "combine references" {
			t.Fatalf("reference fields changed: count=%d files=%#v", count, form.File)
		}
		for i, header := range files {
			file, err := header.Open()
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(file)
			_ = file.Close()
			if err != nil || header.Header.Get("Content-Type") != "image/png" || !bytes.Equal(data, images[i]) {
				t.Fatalf("reference %d changed for count=%d", i, count)
			}
		}
		_ = form.RemoveAll()
	}
}
