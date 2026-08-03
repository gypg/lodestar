package model

import "time"

// GroupTestResult stores a completed group model test run for persistence and analytics.
type GroupTestResult struct {
	ID           int64                  `json:"id" gorm:"primaryKey;autoIncrement:false"`
	GroupName    string                 `json:"group_name" gorm:"size:255;not null;index"`
	TotalModels  int                    `json:"total_models" gorm:"not null;default:0"`
	PassedModels int                    `json:"passed_models" gorm:"not null;default:0"`
	FailedModels int                    `json:"failed_models" gorm:"not null;default:0"`
	Status       GroupTestResultStatus  `json:"status" gorm:"size:16;not null;default:'passed';index"`
	Results      []GroupModelTestResult `json:"results" gorm:"serializer:json"`
	Error        string                 `json:"error" gorm:"type:text"`
	StartedAt    time.Time              `json:"started_at" gorm:"not null;index"`
	FinishedAt   time.Time              `json:"finished_at" gorm:"not null"`
}

// GroupModelTestResult mirrors helper.GroupModelTestResult for JSON persistence.
type GroupModelTestResult struct {
	ClientID     string `json:"client_id,omitempty"`
	ItemID       int    `json:"item_id"`
	ChannelID    int    `json:"channel_id"`
	ChannelName  string `json:"channel_name"`
	ModelName    string `json:"model_name"`
	Passed       bool   `json:"passed"`
	Attempts     int    `json:"attempts"`
	StatusCode   int    `json:"status_code"`
	ResponseText string `json:"response_text,omitempty"`
	Message      string `json:"message,omitempty"`
}

type GroupTestResultStatus string

const (
	GroupTestResultPassed  GroupTestResultStatus = "passed"
	GroupTestResultFailed  GroupTestResultStatus = "failed"
	GroupTestResultRunning GroupTestResultStatus = "running"
)

func (GroupTestResult) TableName() string { return "group_test_results" }
