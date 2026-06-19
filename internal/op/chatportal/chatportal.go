package chatportal

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/apikey"
)

const maxMessagesPerSession = 200
const maxTitleRunes = 48

// Message is one chat turn stored in session JSON.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// SessionSummary is list item without full messages.
type SessionSummary struct {
	ID        int    `json:"id"`
	Title     string `json:"title"`
	Model     string `json:"model"`
	APIKeyID  int    `json:"api_key_id"`
	UpdatedAt int64  `json:"updated_at"`
	CreatedAt int64  `json:"created_at"`
}

// SessionDetail includes parsed messages.
type SessionDetail struct {
	SessionSummary
	Messages []Message `json:"messages"`
}

func ListForUser(uid uint, ctx context.Context) ([]SessionSummary, error) {
	var rows []model.ChatSession
	err := db.GetDB().WithContext(ctx).
		Where("user_id = ?", uid).
		Order("updated_at DESC").
		Limit(100).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]SessionSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, SessionSummary{
			ID: r.ID, Title: r.Title, Model: r.Model, APIKeyID: r.APIKeyID,
			UpdatedAt: r.UpdatedAt, CreatedAt: r.CreatedAt,
		})
	}
	return out, nil
}

func GetForUser(uid uint, id int, ctx context.Context) (*SessionDetail, error) {
	var row model.ChatSession
	err := db.GetDB().WithContext(ctx).
		Where("id = ? AND user_id = ?", id, uid).
		First(&row).Error
	if err != nil {
		return nil, err
	}
	msgs, err := parseMessages(row.Messages)
	if err != nil {
		return nil, err
	}
	return &SessionDetail{
		SessionSummary: SessionSummary{
			ID: row.ID, Title: row.Title, Model: row.Model, APIKeyID: row.APIKeyID,
			UpdatedAt: row.UpdatedAt, CreatedAt: row.CreatedAt,
		},
		Messages: msgs,
	}, nil
}

func Create(uid uint, title, modelName string, apiKeyID int, ctx context.Context) (*SessionDetail, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "新对话"
	}
	title = truncateTitle(title)
	if err := assertKeyOwned(uid, apiKeyID, ctx); err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	row := model.ChatSession{
		UserID: uid, Title: title, Model: strings.TrimSpace(modelName),
		APIKeyID: apiKeyID, Messages: "[]", CreatedAt: now, UpdatedAt: now,
	}
	if err := db.GetDB().WithContext(ctx).Create(&row).Error; err != nil {
		return nil, err
	}
	return &SessionDetail{
		SessionSummary: SessionSummary{
			ID: row.ID, Title: row.Title, Model: row.Model, APIKeyID: row.APIKeyID,
			UpdatedAt: row.UpdatedAt, CreatedAt: row.CreatedAt,
		},
		Messages: []Message{},
	}, nil
}

func SaveMessages(uid uint, id int, modelName string, apiKeyID int, messages []Message, ctx context.Context) error {
	if len(messages) > maxMessagesPerSession {
		return errors.New("too many messages in session")
	}
	if err := assertKeyOwned(uid, apiKeyID, ctx); err != nil {
		return err
	}
	for i := range messages {
		if messages[i].Role != "user" && messages[i].Role != "assistant" {
			return errors.New("invalid message role")
		}
	}
	raw, err := json.Marshal(messages)
	if err != nil {
		return err
	}
	title := deriveTitle(messages)
	now := time.Now().Unix()
	res := db.GetDB().WithContext(ctx).Model(&model.ChatSession{}).
		Where("id = ? AND user_id = ?", id, uid).
		Updates(map[string]interface{}{
			"messages":   string(raw),
			"model":      strings.TrimSpace(modelName),
			"api_key_id": apiKeyID,
			"title":      title,
			"updated_at": now,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("session not found")
	}
	return nil
}

func Delete(uid uint, id int, ctx context.Context) error {
	res := db.GetDB().WithContext(ctx).
		Where("id = ? AND user_id = ?", id, uid).
		Delete(&model.ChatSession{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("session not found")
	}
	return nil
}

func assertKeyOwned(uid uint, apiKeyID int, ctx context.Context) error {
	if apiKeyID <= 0 {
		return errors.New("api_key_id required")
	}
	keys, err := apikey.ListByUser(uid, ctx)
	if err != nil {
		return err
	}
	for _, k := range keys {
		if k.ID == apiKeyID {
			return nil
		}
	}
	return errors.New("api key not found")
}

func parseMessages(raw string) ([]Message, error) {
	if raw == "" || raw == "[]" {
		return []Message{}, nil
	}
	var msgs []Message
	if err := json.Unmarshal([]byte(raw), &msgs); err != nil {
		return nil, err
	}
	return msgs, nil
}

func deriveTitle(messages []Message) string {
	for _, m := range messages {
		if m.Role == "user" {
			t := strings.TrimSpace(m.Content)
			if t != "" {
				return truncateTitle(t)
			}
		}
	}
	return "新对话"
}

func truncateTitle(s string) string {
	r := []rune(s)
	if len(r) <= maxTitleRunes {
		return s
	}
	return string(r[:maxTitleRunes]) + "…"
}