package sftpgo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"sftpgo-manager/internal/domain"
)

// Client wraps the SFTPGo admin API with token caching.
type Client struct {
	baseURL   string
	adminUser string
	adminPass string

	mu       sync.Mutex
	token    string
	tokenExp time.Time
	client   *http.Client
}

// New constructs a Client.
func New(baseURL, adminUser, adminPass string) *Client {
	return &Client{
		baseURL:   baseURL,
		adminUser: adminUser,
		adminPass: adminPass,
		client:    http.DefaultClient,
	}
}

// CreateUser creates a tenant account in SFTPGo.
func (c *Client) CreateUser(ctx context.Context, username, password, homeDir string, publicKeys []string, fs *domain.S3FilesystemConfig) error {
	payload := map[string]any{
		"username":    username,
		"password":    password,
		"status":      1,
		"home_dir":    homeDir,
		"permissions": map[string][]string{"/": {"*"}},
	}
	if len(publicKeys) > 0 {
		payload["public_keys"] = publicKeys
	}
	if fs != nil {
		payload["filesystem"] = map[string]any{
			"provider": 1,
			"s3config": map[string]any{
				"bucket":           fs.Bucket,
				"region":           fs.Region,
				"endpoint":         fs.Endpoint,
				"access_key":       fs.AccessKey,
				"access_secret":    map[string]string{"status": "Plain", "payload": fs.SecretKey},
				"key_prefix":       fs.KeyPrefix,
				"force_path_style": true,
				"skip_tls_verify":  true,
			},
		}
	}
	return c.doJSON(ctx, http.MethodPost, c.baseURL+"/api/v2/users", payload, http.StatusCreated, nil)
}

// GetUser retrieves a user by username.
func (c *Client) GetUser(ctx context.Context, username string) (map[string]any, error) {
	var result map[string]any
	if err := c.doJSON(ctx, http.MethodGet, c.baseURL+"/api/v2/users/"+username, nil, http.StatusOK, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// UpdateUserPublicKeys updates the user's public keys.
func (c *Client) UpdateUserPublicKeys(ctx context.Context, username string, keys []string) error {
	user, err := c.GetUser(ctx, username)
	if err != nil {
		return err
	}

	payload := map[string]any{
		"username":    username,
		"status":      user["status"],
		"home_dir":    user["home_dir"],
		"permissions": user["permissions"],
		"public_keys": keys,
	}
	if filesystem, ok := user["filesystem"]; ok {
		payload["filesystem"] = filesystem
	}
	return c.doJSON(ctx, http.MethodPut, c.baseURL+"/api/v2/users/"+username, payload, http.StatusOK, nil)
}

// DeleteUser deletes a user by username.
func (c *Client) DeleteUser(ctx context.Context, username string) error {
	return c.doJSON(ctx, http.MethodDelete, c.baseURL+"/api/v2/users/"+username, nil, http.StatusOK, nil)
}

func (c *Client) getToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token != "" && time.Now().Before(c.tokenExp) {
		return c.token, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v2/token", nil)
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}
	req.SetBasicAuth(c.adminUser, c.adminPass)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token request failed (%d): %s", resp.StatusCode, body)
	}

	var out struct {
		AccessToken string    `json:"access_token"`
		ExpiresAt   time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	c.token = out.AccessToken
	c.tokenExp = out.ExpiresAt.Add(-30 * time.Second)
	return c.token, nil
}

func (c *Client) doJSON(ctx context.Context, method, url string, payload any, expectedStatus int, out any) error {
	var body io.Reader
	if payload != nil {
		buf, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal payload: %w", err)
		}
		body = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	token, err := c.getToken(ctx)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("perform request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != expectedStatus {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("sftpgo %s %s failed (%d): %s", method, url, resp.StatusCode, raw)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
