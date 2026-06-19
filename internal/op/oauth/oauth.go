/*
Package oauth implements GitHub OAuth login and account binding for Lodestar.

State tokens are stored in-memory with a 5-minute TTL (same pattern as WebAuthn
challenges).  All HTTP calls to GitHub are made directly — no external OAuth
library is required.
*/
package oauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	stg "github.com/gypg/lodestar/internal/op/setting"
	usr "github.com/gypg/lodestar/internal/op/user"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

var (
	ErrOAuthNotEnabled       = errors.New("GitHub OAuth is not enabled")
	ErrInvalidState          = errors.New("invalid or expired OAuth state")
	ErrAlreadyBound          = errors.New("GitHub account is already bound to another user")
	ErrNotBound              = errors.New("no GitHub account is bound to this user")
	ErrRegistrationDisabled  = errors.New("registration is disabled")
	ErrEmptyCode             = errors.New("authorization code is empty")
	ErrTokenExchangeFailed   = errors.New("failed to exchange code for token")
	ErrUserInfoFailed        = errors.New("failed to get user info from GitHub")
)

// ---------------------------------------------------------------------------
// State token store (in-memory, 5-min TTL)
// ---------------------------------------------------------------------------

const stateTTL = 5 * time.Minute

type stateEntry struct {
	expiry time.Time
}

var (
	statesMu sync.Mutex
	states   = make(map[string]*stateEntry)
)

func saveState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)

	now := time.Now()
	statesMu.Lock()
	defer statesMu.Unlock()
	// lazy cleanup
	for k, v := range states {
		if now.After(v.expiry) {
			delete(states, k)
		}
	}
	states[token] = &stateEntry{expiry: now.Add(stateTTL)}
	return token, nil
}

func takeState(token string) bool {
	statesMu.Lock()
	defer statesMu.Unlock()
	s, ok := states[token]
	if !ok {
		return false
	}
	delete(states, token)
	return time.Now().Before(s.expiry)
}

// ---------------------------------------------------------------------------
// GitHub response types
// ---------------------------------------------------------------------------

type gitHubTokenResponse struct {
	AccessToken string `json:"access_token"`
	Scope       string `json:"scope"`
	TokenType   string `json:"token_type"`
}

type gitHubUser struct {
	Id    int64  `json:"id"`
	Login string `json:"login"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// GitHubUser is the public view of a GitHub user after OAuth.
type GitHubUser struct {
	ProviderUserID string // stable numeric ID
	Username       string // GitHub login
	DisplayName    string // display name
	Email          string
}

// ---------------------------------------------------------------------------
// Configuration helpers
// ---------------------------------------------------------------------------

func IsEnabled() bool {
	v, _ := stg.GetBool(model.SettingKeyGitHubOAuthEnabled)
	return v
}

func clientCredentials() (clientID, clientSecret string, err error) {
	clientID, err = stg.GetString(model.SettingKeyGitHubOAuthClientID)
	if err != nil || strings.TrimSpace(clientID) == "" {
		return "", "", fmt.Errorf("github oauth client id is not configured")
	}
	clientSecret, err = stg.GetString(model.SettingKeyGitHubOAuthClientSecret)
	if err != nil || strings.TrimSpace(clientSecret) == "" {
		return "", "", fmt.Errorf("github oauth client secret is not configured")
	}
	return strings.TrimSpace(clientID), strings.TrimSpace(clientSecret), nil
}

// ---------------------------------------------------------------------------
// GenerateState creates a CSRF state token for the OAuth flow.
// ---------------------------------------------------------------------------

func GenerateState(_ context.Context) (string, error) {
	if !IsEnabled() {
		return "", ErrOAuthNotEnabled
	}
	return saveState()
}

// ---------------------------------------------------------------------------
// ExchangeCodeAndUser exchanges the authorization code for an access token
// and fetches user info from GitHub.
// ---------------------------------------------------------------------------

func ExchangeCodeAndUser(ctx context.Context, code string) (*GitHubUser, error) {
	if !IsEnabled() {
		return nil, ErrOAuthNotEnabled
	}
	if code == "" {
		return nil, ErrEmptyCode
	}

	// Exchange code for access token.
	accessToken, err := exchangeToken(ctx, code)
	if err != nil {
		return nil, err
	}

	// Get user info.
	return getUserInfo(ctx, accessToken)
}

// ValidateState checks and consumes a CSRF state token.
func ValidateState(state string) bool {
	return takeState(state)
}

// ---------------------------------------------------------------------------
// FindOrCreateUser looks up an existing user by GitHub ID or creates a new one.
// ---------------------------------------------------------------------------

func FindOrCreateUser(ctx context.Context, ghUser *GitHubUser) (model.User, error) {
	// Look up existing binding.
	binding, err := findByProvider("github", ghUser.ProviderUserID)
	if err == nil {
		// Found binding; load user.
		u, err := usr.GetByID(binding.UserID, ctx)
		if err != nil {
			return model.User{}, fmt.Errorf("bound user not found: %w", err)
		}
		return u, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.User{}, err
	}

	// No binding — create new user.
	commercialMode, _ := stg.GetBool(model.SettingKeyCommercialMode)
	if !commercialMode {
		return model.User{}, ErrRegistrationDisabled
	}

	username := ghUser.Username
	if username == "" {
		username = "github_" + strconv.FormatInt(time.Now().UnixMilli(), 36)
	}
	displayName := ghUser.DisplayName
	if displayName == "" {
		displayName = username
	}

	newUser := model.User{
		Username: username,
		Password: randomPassword(),
		Role:     model.UserRoleUser,
	}
	if err := newUser.HashPassword(); err != nil {
		return model.User{}, fmt.Errorf("hash password: %w", err)
	}
	if err := db.GetDB().WithContext(ctx).Create(&newUser).Error; err != nil {
		// If username collision, append a suffix.
		newUser.Username = username + "_" + strconv.FormatInt(time.Now().UnixMilli(), 36)
		if err2 := db.GetDB().WithContext(ctx).Create(&newUser).Error; err2 != nil {
			return model.User{}, fmt.Errorf("create user: %w", err2)
		}
	}

	// Create binding.
	binding = &model.OAuthBinding{
		UserID:           newUser.ID,
		Provider:         "github",
		ProviderUserID:   ghUser.ProviderUserID,
		ProviderUsername: ghUser.Username,
	}
	if err := db.GetDB().WithContext(ctx).Create(binding).Error; err != nil {
		return model.User{}, fmt.Errorf("create oauth binding: %w", err)
	}

	return newUser, nil
}

// ---------------------------------------------------------------------------
// BindUser links a GitHub account to an existing logged-in user.
// ---------------------------------------------------------------------------

func BindUser(ctx context.Context, userID uint, ghUser *GitHubUser) error {
	// Check if this GitHub account is already bound.
	_, err := findByProvider("github", ghUser.ProviderUserID)
	if err == nil {
		return ErrAlreadyBound
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	binding := &model.OAuthBinding{
		UserID:           userID,
		Provider:         "github",
		ProviderUserID:   ghUser.ProviderUserID,
		ProviderUsername: ghUser.Username,
	}
	return db.GetDB().WithContext(ctx).Create(binding).Error
}

// ---------------------------------------------------------------------------
// UnbindUser removes the GitHub binding for the given user.
// ---------------------------------------------------------------------------

func UnbindUser(ctx context.Context, userID uint) error {
	res := db.GetDB().WithContext(ctx).
		Where("user_id = ? AND provider = ?", userID, "github").
		Delete(&model.OAuthBinding{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotBound
	}
	return nil
}

// GetBinding returns the GitHub OAuth binding for a user, if any.
func GetBinding(userID uint) (*model.OAuthBinding, error) {
	var binding model.OAuthBinding
	err := db.GetDB().
		Where("user_id = ? AND provider = ?", userID, "github").
		First(&binding).Error
	if err != nil {
		return nil, err
	}
	return &binding, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func exchangeToken(ctx context.Context, code string) (string, error) {
	clientID, clientSecret, err := clientCredentials()
	if err != nil {
		return "", err
	}

	payload := map[string]string{
		"client_id":     clientID,
		"client_secret": clientSecret,
		"code":          code,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://github.com/login/oauth/access_token", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrTokenExchangeFailed, err)
	}
	defer resp.Body.Close()

	var tokenResp gitHubTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("%w: decode error: %v", ErrTokenExchangeFailed, err)
	}
	if tokenResp.AccessToken == "" {
		return "", ErrTokenExchangeFailed
	}
	return tokenResp.AccessToken, nil
}

func getUserInfo(ctx context.Context, accessToken string) (*GitHubUser, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUserInfoFailed, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: status %d, body: %s", ErrUserInfoFailed, resp.StatusCode, truncate(string(body), 500))
	}

	var u gitHubUser
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, fmt.Errorf("%w: decode error: %v", ErrUserInfoFailed, err)
	}
	if u.Id == 0 || u.Login == "" {
		return nil, fmt.Errorf("%w: empty id or login", ErrUserInfoFailed)
	}

	return &GitHubUser{
		ProviderUserID: strconv.FormatInt(u.Id, 10),
		Username:       u.Login,
		DisplayName:    u.Name,
		Email:          u.Email,
	}, nil
}

func findByProvider(provider, providerUserID string) (*model.OAuthBinding, error) {
	var binding model.OAuthBinding
	err := db.GetDB().
		Where("provider = ? AND provider_user_id = ?", provider, providerUserID).
		First(&binding).Error
	return &binding, err
}

func randomPassword() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
