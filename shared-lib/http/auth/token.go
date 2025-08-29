package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OAuthTokenResponse represents the structure of the token response from the OAuth 2.0 server.
type OAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"` // Optional, depending on grant type
}

// GetOAuthToken retrieves an OAuth 2.0 access token using the client credentials grant type.
func GetOAuthToken(ctx context.Context, clientID, clientSecret, tokenURL string) (*OAuthTokenResponse, error) {
	// 1. Prepare the request body.
	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)

	// 2. Create the HTTP request.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// 3. Make the HTTP request.
	client := &http.Client{
		Timeout: 10 * time.Second, // Set a reasonable timeout
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	// 4. Check the response status code.
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// 5. Decode the response body.
	var tokenResponse OAuthTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResponse); err != nil {
		return nil, fmt.Errorf("failed to decode response body: %w", err)
	}

	return &tokenResponse, nil
}

// AuthConfig holds authentication configuration for fetching compose files
type AuthConfig struct {
	Type     AuthType          `json:"type"`
	Username string            `json:"username,omitempty"`
	Password string            `json:"password,omitempty"`
	Token    string            `json:"token,omitempty"`
	APIKey   string            `json:"apiKey,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
}

type AuthType string

const (
	AuthTypeNone   AuthType = "none"
	AuthTypeBasic  AuthType = "basic"
	AuthTypeBearer AuthType = "bearer"
	AuthTypeAPIKey AuthType = "apikey"
	AuthTypeCustom AuthType = "custom"
)

// applyAuthentication applies the specified authentication method to the HTTP request
func ApplyAuthentication(req *http.Request, auth *AuthConfig) error {
	if auth == nil {
		return nil // No authentication required
	}

	switch auth.Type {
	case AuthTypeNone:
		// No authentication
		return nil

	case AuthTypeBasic:
		if auth.Username == "" || auth.Password == "" {
			return fmt.Errorf("username and password required for basic authentication")
		}
		req.SetBasicAuth(auth.Username, auth.Password)

	case AuthTypeBearer:
		if auth.Token == "" {
			return fmt.Errorf("token required for bearer authentication")
		}
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", auth.Token))

	case AuthTypeAPIKey:
		if auth.APIKey == "" {
			return fmt.Errorf("API key required for API key authentication")
		}
		// Common API key header patterns
		req.Header.Set("X-API-Key", auth.APIKey)
		// Alternative patterns:
		// req.Header.Set("Authorization", fmt.Sprintf("ApiKey %s", auth.APIKey))
		// req.Header.Set("X-Auth-Token", auth.APIKey)

	case AuthTypeCustom:
		if len(auth.Headers) == 0 {
			return fmt.Errorf("custom headers required for custom authentication")
		}
		for key, value := range auth.Headers {
			if key == "" || value == "" {
				continue
			}
			req.Header.Set(key, value)
		}

	default:
		return fmt.Errorf("unsupported authentication type: %s", auth.Type)
	}

	return nil
}

// Helper function to create AuthConfig for different scenarios
func NewBasicAuth(username, password string) *AuthConfig {
	return &AuthConfig{
		Type:     AuthTypeBasic,
		Username: username,
		Password: password,
	}
}

func NewBearerAuth(token string) *AuthConfig {
	return &AuthConfig{
		Type:  AuthTypeBearer,
		Token: token,
	}
}

func NewAPIKeyAuth(apiKey string) *AuthConfig {
	return &AuthConfig{
		Type:   AuthTypeAPIKey,
		APIKey: apiKey,
	}
}

func NewCustomAuth(headers map[string]string) *AuthConfig {
	return &AuthConfig{
		Type:    AuthTypeCustom,
		Headers: headers,
	}
}
