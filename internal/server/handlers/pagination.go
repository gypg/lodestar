package handlers

import "strconv"

const (
	defaultPageSize = 20
	maxPageSize     = 100
	// maxPage 钳制深分页（octopus #239）：page 无上界时大 page 直译成深
	// OFFSET，relay_logs 这类大表上 DB 成本随 page 线性放大，低权（logs:read）
	// 即可反复触发。1000 × maxPageSize = 最多扫 10 万行窗口，正常翻页远够。
	maxPage = 1000
)

func parsePagination(rawPage, rawPageSize string) (page, pageSize int) {
	page, _ = strconv.Atoi(rawPage)
	pageSize, _ = strconv.Atoi(rawPageSize)
	if page < 1 {
		page = 1
	}
	if page > maxPage {
		page = maxPage
	}
	if pageSize < 1 || pageSize > maxPageSize {
		pageSize = defaultPageSize
	}
	return
}
