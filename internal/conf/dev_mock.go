package conf

import (
	"os"
	"strings"
)

func IsDevMockSuccess() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(strings.ToUpper(APP_NAME)+"_DEV_MOCK_SUCCESS")), "true")
}
