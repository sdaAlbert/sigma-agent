package agent

import (
	"net/http"
	"time"
)

func defaultModelHTTPClient() *http.Client {
	return &http.Client{Timeout: 2 * time.Minute}
}
