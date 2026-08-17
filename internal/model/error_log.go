package model

// ErrorLog 记录前后端崩溃/错误日志（排障用），落主库（随主库备份）。
// 与 relay log 体系完全不同：relay log 是中继请求日志，可能存独立日志库且
// 视为可丢弃；ErrorLog 是崩溃日志，重启不丢。
//
// 来源（Source）：
//   - backend：后端 Go panic（gin CustomRecovery 捕获）
//   - frontend：管理 UI 捕获的 JS 错误（window.onerror / unhandledrejection /
//     React ErrorBoundary）
//
// Level 区分严重程度：panic（崩溃）、error、unhandledrejection、uncaught。
// Message 为错误简述，Stack 为完整堆栈；请求/页面信息辅助定位触发场景。
type ErrorLog struct {
	ID     int64  `json:"id" gorm:"primaryKey;autoIncrement:false"` // Snowflake ID
	Time   int64  `json:"time" gorm:"column:time;index"`            // 时间戳（秒）
	Source string `json:"source" gorm:"column:source;size:16"`      // backend | frontend
	Level  string `json:"level" gorm:"column:level;size:32"`        // panic | error | unhandledrejection | uncaught
	// Message 错误信息（panic 值 / JS 错误信息）
	Message string `json:"message" gorm:"column:message;type:text"`
	// Stack 完整堆栈（后端 debug.Stack() / 前端 error.stack）
	Stack string `json:"stack" gorm:"column:stack;type:text"`
	// 后端请求信息（panic 发生时）
	RequestMethod string `json:"request_method" gorm:"column:request_method;size:16"`
	RequestPath   string `json:"request_path" gorm:"column:request_path;size:512"`
	ClientIP      string `json:"client_ip" gorm:"column:client_ip;size:64"`
	UserAgent     string `json:"user_agent" gorm:"column:user_agent;size:512"`
	// 前端页面信息（错误发生时）
	PageURL string `json:"page_url" gorm:"column:page_url;size:1024"`
	RouteID string `json:"route_id" gorm:"column:route_id;size:128"`
	// 上报方版本（后端 conf.Version / 前端 APP_VERSION）
	Version string `json:"version" gorm:"column:version;size:64"`
}

// TableName 显式指定表名（与 GORM 默认一致，便于阅读）。
func (ErrorLog) TableName() string { return "error_logs" }
