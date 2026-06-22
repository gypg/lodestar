package relay

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	dbmodel "github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/utils/log"
)

const imageBedUploadTimeout = 10 * time.Second

// imageBedConfig holds the resolved image bed settings.
type imageBedConfig struct {
	Enabled  bool
	Endpoint string
	Token    string
}

// readImageBedConfig reads image bed settings from the cache.
func readImageBedConfig() imageBedConfig {
	enabled, err := setting.GetBool(dbmodel.SettingKeyImageBedEnabled)
	if err != nil || !enabled {
		return imageBedConfig{}
	}
	endpoint, _ := setting.GetString(dbmodel.SettingKeyImageBedEndpoint)
	token, _ := setting.GetString(dbmodel.SettingKeyImageBedToken)
	return imageBedConfig{Enabled: true, Endpoint: endpoint, Token: token}
}

// imageBedResponse represents a typical image bed upload response.
type imageBedResponse struct {
	Data struct {
		URL string `json:"url"`
	} `json:"data"`
	// Fallback: some APIs return url at the top level.
	URL string `json:"url"`
}

// uploadToImageBed uploads base64-encoded image data to the configured image
// bed endpoint and returns the hosted URL. On any error it returns the
// original b64 data unchanged so the relay is never broken.
func uploadToImageBed(b64Data string, cfg imageBedConfig) (string, error) {
	if cfg.Endpoint == "" {
		return "", fmt.Errorf("image bed endpoint is empty")
	}

	imageBytes, err := base64.StdEncoding.DecodeString(b64Data)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64 image: %w", err)
	}
	// Release b64 string for GC as soon as possible.
	b64Data = ""

	body, contentType, err := buildImageBedMultipartBody(imageBytes)
	if err != nil {
		return "", fmt.Errorf("failed to build multipart body: %w", err)
	}
	// Release raw bytes after building the multipart body.
	imageBytes = nil

	ctx, cancel := context.WithTimeout(context.Background(), imageBedUploadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.Endpoint, body)
	if err != nil {
		return "", fmt.Errorf("failed to create image bed request: %w", err)
	}

	req.Header.Set("Content-Type", contentType)
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("image bed upload failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
		return "", fmt.Errorf("image bed returned %d: %s", resp.StatusCode, string(errBody))
	}

	url, err := extractImageBedURL(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to parse image bed response: %w", err)
	}
	return url, nil
}

// buildImageBedMultipartBody builds a multipart/form-data body with the image
// bytes in a "file" field.
func buildImageBedMultipartBody(imageBytes []byte) (*bytes.Buffer, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("file", "image.png")
	if err != nil {
		return nil, "", fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := part.Write(imageBytes); err != nil {
		return nil, "", fmt.Errorf("failed to write image data: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	return &buf, writer.FormDataContentType(), nil
}

// extractImageBedURL parses the image bed response and extracts the hosted URL.
func extractImageBedURL(body io.Reader) (string, error) {
	limited := io.LimitReader(body, 64*1024)
	var resp imageBedResponse
	if err := json.NewDecoder(limited).Decode(&resp); err != nil {
		return "", fmt.Errorf("invalid JSON: %w", err)
	}

	if resp.Data.URL != "" {
		return resp.Data.URL, nil
	}
	if resp.URL != "" {
		return resp.URL, nil
	}
	return "", fmt.Errorf("no URL found in image bed response")
}

// tryImageBedUpload checks if image bed is enabled and attempts to upload the
// base64 image data. Returns the hosted URL and true on success, or empty
// string and false if image bed is disabled or the upload fails.
func tryImageBedUpload(b64Data string) (string, bool) {
	cfg := readImageBedConfig()
	if !cfg.Enabled {
		return "", false
	}

	url, err := uploadToImageBed(b64Data, cfg)
	if err != nil {
		log.Warnf("image bed upload failed, falling back to b64_json: %v", err)
		return "", false
	}
	return url, true
}
