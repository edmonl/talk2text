// Package whisper transcribes audio through a Whisper-compatible HTTP endpoint.
package whisper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"time"

	"github.com/edmonl/talk2text/internal/runtimedir"
	"github.com/edmonl/talk2text/internal/util"
)

const maxErrorBodyBytes = 256

// Config controls the Whisper HTTP client.
type Config struct {
	// Endpoint is the Whisper-compatible HTTP inference endpoint.
	Endpoint string
	// ConnectTimeout limits the endpoint connection phase; zero disables the connect timeout.
	ConnectTimeout time.Duration
	// RequestTimeout limits the HTTP request after recording stops; zero disables the request timeout.
	RequestTimeout time.Duration
}

// Client transcribes clips through a Whisper-compatible HTTP endpoint.
type Client struct {
	endpoint string
	client   *http.Client
}

// NewClient returns a Whisper HTTP client using cfg.
func NewClient(cfg Config) *Client {
	dialer := &net.Dialer{Timeout: cfg.ConnectTimeout}
	transport := &http.Transport{
		DialContext: dialer.DialContext,
	}
	return &Client{
		endpoint: cfg.Endpoint,
		client: &http.Client{
			Transport: transport,
			Timeout:   cfg.RequestTimeout,
		},
	}
}

// Transcribe sends clip to Whisper and returns normalized response text.
func (c *Client) Transcribe(ctx context.Context, clipID int, pcm []byte, runtimeDir string) (string, error) {
	prompt, err := runtimedir.ReadPrompt(runtimeDir)
	if err != nil {
		return "", fmt.Errorf("failed to read transcription prompt: %w", err)
	}
	body, contentType, err := multipartBodyReader(clipID, prompt, pcm)
	if err != nil {
		return "", fmt.Errorf("failed to prepare body reader: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, body)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request or receive response: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return "", fmt.Errorf("whisper returned %s: %s", resp.Status, string(respBody))
	}
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}
	var parsed struct {
		Text *string `json:"text"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}
	if parsed.Text == nil {
		return "", errors.New("whisper response missing text")
	}
	return util.CollapseSpace(*parsed.Text), nil
}

func multipartBodyReader(clipID int, prompt string, pcm []byte) (io.Reader, string, error) {
	var prefix bytes.Buffer
	mp := multipart.NewWriter(&prefix)
	if err := writeMultipartTextFields(mp, prompt); err != nil {
		return nil, "", err
	}
	fileWriter, err := mp.CreateFormFile("file", fmt.Sprintf("%d.wav", clipID))
	if err != nil {
		return nil, "", err
	}
	if err = writeWavHeader(fileWriter, len(pcm)); err != nil {
		return nil, "", err
	}
	contentType := mp.FormDataContentType()

	var suffix bytes.Buffer
	suffixMP := multipart.NewWriter(&suffix)
	if err = suffixMP.SetBoundary(mp.Boundary()); err != nil {
		return nil, "", err
	}
	err = suffixMP.Close()
	if err != nil {
		return nil, "", err
	}

	body := io.MultiReader(
		bytes.NewReader(prefix.Bytes()),
		bytes.NewReader(pcm),
		bytes.NewReader(suffix.Bytes()),
	)
	return body, contentType, nil
}

func writeMultipartTextFields(mp *multipart.Writer, prompt string) error {
	if err := mp.WriteField("temperature", "0"); err != nil {
		return err
	}
	if err := mp.WriteField("temperature_inc", "0.9"); err != nil {
		return err
	}
	if err := mp.WriteField("response_format", "json"); err != nil {
		return err
	}
	if prompt != "" {
		if err := mp.WriteField("prompt", prompt); err != nil {
			return err
		}
	}
	return nil
}
